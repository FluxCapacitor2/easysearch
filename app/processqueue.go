package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/embedding"
	"github.com/go-co-op/gocron/v2"
	slogctx "github.com/veqryn/slog-context"
)

func scheduleJobs(db database.Database, config *config.Config) {

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	for _, src := range config.Sources {
		interval := 60.0 / float64(src.Speed)

		if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(interval*float64(time.Second))), gocron.NewTask(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			processCrawlQueue(ctx, db, src)
		})); err != nil {
			slog.Error("Failed to create crawl job", "error", err)
		}
	}

	for _, src := range config.Sources {
		if !src.Embeddings.Enabled {
			continue
		}
		interval := 60.0 / float64(src.Embeddings.Speed)

		if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(interval*float64(time.Second))), gocron.NewTask(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ctx = slogctx.Append(ctx, "sourceId", src.ID)

			if src.Embeddings.Enabled {
				err = db.StartEmbeddings(ctx, src.ID, src.Embeddings.ChunkSize, src.Embeddings.ChunkOverlap)
				if err != nil {
					slog.Error("Failed to queue pages that need embeddings", "error", err)
				}
			}

			processEmbedQueue(ctx, db, src)
		})); err != nil {
			slog.Error("Failed to create embedding job", "error", err)
		}
	}

	if err := db.Cleanup(context.Background()); err != nil {
		slog.Error("Failed to run Cleanup", "error", err)
	}

	if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := db.Cleanup(ctx)
		if err != nil {
			slogctx.Error(ctx, "Failed to run Cleanup", "error", err)
		}
	})); err != nil {
		slog.Error("Failed to create cleanup job", "error", err)
	}

	if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := db.CreateSpellfixIndex(ctx)
		if err != nil {
			slogctx.Error(ctx, "Failed to build spellfix index", "error", err)
		}
	})); err != nil {
		slog.Error("Failed to create spellfix index build job", "error", err)
	}

	scheduler.Start()
}

func processEmbedQueue(ctx context.Context, db database.Database, src config.Source) {
	items, err := db.PopEmbedQueue(ctx, src.Embeddings.BatchSize, src.ID)
	if err != nil {
		slogctx.Error(ctx, "Failed to get next item in embed queue", "error", err)
	}
	if len(items) == 0 {
		// The queue is empty
		return
	}

	markFailure := func(id int64) {
		err := db.UpdateEmbedQueueEntry(ctx, id, database.Error)
		if err != nil {
			slogctx.Error(ctx, "Failed to mark embedding queue item as Error", "error", err)
		}
	}

	content := make([]string, 0, len(items))

	for _, item := range items {
		content = append(content, item.Content)
	}

	vectors, err := embedding.GetEmbeddings(ctx, src.Embeddings.OpenAIBaseURL, src.Embeddings.Model, src.Embeddings.APIKey, content)
	if err != nil {
		slogctx.Error(ctx, "Failed to generate embeddings", "error", err)
		for _, item := range items {
			markFailure(item.ID)
		}
		return
	}

	for i, item := range items {
		err = db.AddEmbedding(ctx, item.PageID, src.ID, item.ChunkIndex, item.Content, vectors[i])
		if err != nil {
			slogctx.Error(ctx, "Failed to save embedding", "error", err)
			markFailure(item.ID)
			return
		}

		err = db.UpdateEmbedQueueEntry(ctx, item.ID, database.Finished)
		if err != nil {
			slogctx.Error(ctx, "Failed to mark embedding queue item as Finished", "error", err)
		}
	}
}

func processCrawlQueue(ctx context.Context, db database.Database, src config.Source) {
	// Pop the oldest item off the queue and crawl it.
	item, err := db.PopQueue(ctx, src.ID)
	if err != nil {
		slogctx.Error(ctx, "Failed to get next item in crawl queue", "error", err)
	}
	if item == nil {
		// The queue is empty
		return
	} else {
		ctx = slogctx.With(ctx, "sourceId", src.ID)
	}

	// The `item` is nil when there are no items in the queue
	result, err := crawler.Crawl(ctx, src, item.Depth, item.Referrers, db, item.URL)

	if result != nil {
		ctx = slogctx.With(ctx, "original", item.URL, "canonical", result.Canonical, "pageId", result.PageID)
	}

	if err != nil {
		// Mark the queue entry as errored
		slogctx.Error(ctx, "Failed to crawl URL", "error", err)
		{
			err := db.UpdateQueueEntry(ctx, item.ID, database.Error)
			if err != nil {
				slogctx.Error(ctx, "Failed to update queue item status to Error", "error", err)
			}
		}

		// Add an entry to the pages table to prevent immediately recrawling the same URL when referred from other sources.
		// Additionally, if refresh is enabled, another crawl attempt will be made after the refresh interval passes.
		if result != nil {
			_, err := db.AddDocument(ctx, src.ID, item.Depth, item.Referrers, result.Canonical, database.Error, "", "", "", err.Error())
			if err != nil {
				slogctx.Error(ctx, "Failed to add placeholder page in Error state", "error", err)
			}
		}

	} else {
		// Chunk the page into sections and add it to the embedding queue
		if result.PageID > 0 {
			chunks, err := embedding.ChunkText(result.Content.Content, src.Embeddings.ChunkSize, src.Embeddings.ChunkOverlap)

			if err != nil {
				slogctx.Error(ctx, "Failed to split page into chunks for embedding", "error", err)
			}

			// Filter out empty chunks
			filtered := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				if len(strings.TrimSpace(chunk)) != 0 {
					filtered = append(filtered, chunk)
				}
			}

			err = db.AddToEmbedQueue(ctx, result.PageID, filtered)

			if err != nil {
				slogctx.Error(ctx, "Failed to add page chunks to embed queue", "error", err)
			}
		}

		// If the crawl completed successfully, mark the item as finished
		err = db.UpdateQueueEntry(ctx, item.ID, database.Finished)
		if err != nil {
			slogctx.Error(ctx, "Failed to update queue item status to Finished", "error", err)
		}
	}

	// Remove existing references and then populate new ones
	err = db.RemoveAllReferences(ctx, result.PageID)
	if err != nil {
		slogctx.Error(ctx, "Failed to remove old references", "error", err)
	}

	// Record existing pages that this page refers to
	filtered := filterURLs(db, src, result.URLs, false)
	for _, url := range filtered {
		doc, err := db.GetDocument(ctx, src.ID, url)
		if err != nil || doc == nil {
			continue
		}
		err = db.AddReferrer(ctx, result.PageID, doc.ID)
		if err != nil {
			slogctx.Error(ctx, "Failed to record referrer", "error", err)
		}
	}

	if item.Depth+1 >= src.MaxDepth {
		return // No need to add any new URLs
	}

	// Add URLs found in the crawl to the queue
	filtered = filterURLs(db, src, result.URLs, true)
	err = db.AddToQueue(ctx, src.ID, result.Canonical, filtered, item.Depth+1, false)
	if err != nil {
		slogctx.Error(ctx, "Failed to add URLs to queue", "error", err)
	}
}

func filterURLs(db database.Database, src config.Source, urls []string, newOnly bool) []string {
	filtered := []string{}

	for _, fullURL := range urls {
		res, err := url.Parse(fullURL)
		if err != nil {
			continue
		} else {

			if newOnly {
				crawled, err := db.HasDocument(context.Background(), src.ID, fullURL)

				if err == nil && *crawled {
					continue
				}
			}

			if slices.Contains(src.AllowedDomains, res.Hostname()) {
				filtered = append(filtered, fullURL)
			}
		}
	}
	return filtered
}
