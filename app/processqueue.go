package main

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/embedding"
	"github.com/go-co-op/gocron/v2"
)

func startQueueJob(db database.Database, config *config.Config) {

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
			fmt.Printf("Error creating crawl job: %v", err)
		}
	}

	for _, src := range config.Sources {
		if !src.Embeddings.Enabled {
			continue
		}
		interval := 60.0 / float64(src.Embeddings.Speed)

		if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(interval*float64(time.Second))), gocron.NewTask(func() {

			if src.Embeddings.Enabled {
				err = db.StartEmbeddings(context.Background(), src.ID, src.Embeddings.ChunkSize, src.Embeddings.ChunkOverlap)
				if err != nil {
					fmt.Printf("Error queueing pages that need embeddings: %v\n", err)
				}
			}

			processEmbedQueue(db, src)
		})); err != nil {
			fmt.Printf("Error creating embedding job: %v", err)
		}
	}

	if err := db.Cleanup(context.Background()); err != nil {
		fmt.Printf("error running Cleanup on startup: %v\n", err)
	}

	if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
		err := db.Cleanup(context.Background())
		if err != nil {
			fmt.Printf("Error cleaning queue: %v\n", err)
		}
	})); err != nil {
		fmt.Printf("Failed to create cleanup job: %v\n", err)
	}

	scheduler.Start()
}

func processEmbedQueue(db database.Database, src config.Source) {
	items, err := db.PopEmbedQueue(context.Background(), src.Embeddings.BatchSize, src.ID)
	if err != nil {
		fmt.Printf("Failed to get next item in embed queue: %v\n", err)
	}
	if len(items) == 0 {
		// The queue is empty
		return
	}

	markFailure := func(id int64) {
		err := db.UpdateEmbedQueueEntry(context.Background(), id, database.Error)
		if err != nil {
			fmt.Printf("failed to mark embedding queue item as Error: %v\n", err)
		}
	}

	content := make([]string, 0, len(items))

	for _, item := range items {
		content = append(content, item.Content)
	}

	vectors, err := embedding.GetEmbeddings(src.Embeddings.OpenAIBaseURL, src.Embeddings.Model, src.Embeddings.APIKey, content)
	if err != nil {
		fmt.Printf("error getting embeddings: %v\n", err)
		for _, item := range items {
			markFailure(item.ID)
		}
		return
	}

	for i, item := range items {
		err = db.AddEmbedding(context.Background(), item.PageID, src.ID, item.ChunkIndex, item.Content, vectors[i])
		if err != nil {
			fmt.Printf("error saving embedding: %v\n", err)
			markFailure(item.ID)
			return
		}

		err = db.UpdateEmbedQueueEntry(context.Background(), item.ID, database.Finished)
		if err != nil {
			fmt.Printf("failed to mark embedding queue item as Finished: %v\n", err)
		}
	}
}

func processCrawlQueue(ctx context.Context, db database.Database, src config.Source) {
	// Pop the oldest item off the queue and crawl it.
	item, err := db.PopQueue(context.Background(), src.ID)
	if err != nil {
		fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
	}
	if item == nil {
		// The queue is empty
		return
	}

	// The `item` is nil when there are no items in the queue
	result, err := crawler.Crawl(ctx, src, item.Depth, item.Referrers, db, item.URL)

	if err != nil {
		// Mark the queue entry as errored
		fmt.Printf("Error crawling URL %v from source %v: %v\n", item.URL, src.ID, err)
		{
			err := db.UpdateQueueEntry(context.Background(), item.ID, database.Error)
			if err != nil {
				fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.URL, database.Error, err)
			}
		}

		// Add an entry to the pages table to prevent immediately recrawling the same URL when referred from other sources.
		// Additionally, if refresh is enabled, another crawl attempt will be made after the refresh interval passes.
		if result != nil {
			_, err := db.AddDocument(context.Background(), src.ID, item.Depth, item.Referrers, result.Canonical, database.Error, "", "", "", err.Error())
			if err != nil {
				fmt.Printf("Failed to add page in 'error' state: %v\n", err)
			}
		}

	} else {
		// Chunk the page into sections and add it to the embedding queue
		if result.PageID > 0 {
			chunks, err := embedding.ChunkText(result.Content.Content, src.Embeddings.ChunkSize, src.Embeddings.ChunkOverlap)

			if err != nil {
				fmt.Printf("error chunking page: %v\n", err)
			}

			// Filter out empty chunks
			filtered := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				if len(strings.TrimSpace(chunk)) != 0 {
					filtered = append(filtered, chunk)
				}
			}

			err = db.AddToEmbedQueue(context.Background(), result.PageID, filtered)

			if err != nil {
				fmt.Printf("error adding page chunks to embed queue: %v\n", err)
			}
		}

		// If the crawl completed successfully, mark the item as finished
		err = db.UpdateQueueEntry(context.Background(), item.ID, database.Finished)
		if err != nil {
			fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.URL, database.Finished, err)
		}
	}

	// Remove existing references and then populate new ones
	err = db.RemoveAllReferences(context.Background(), result.PageID)
	if err != nil {
		fmt.Printf("Error removing references: %v\n", err)
	}

	// Record existing pages that this page refers to
	filtered := filterURLs(db, src, result.URLs, false)
	for _, url := range filtered {
		doc, err := db.GetDocument(context.Background(), src.ID, url)
		if err != nil || doc == nil {
			continue
		}
		err = db.AddReferrer(context.Background(), result.PageID, doc.ID)
		if err != nil {
			fmt.Printf("Error recording referrer: %v\n", err)
		}
	}

	if item.Depth+1 >= src.MaxDepth {
		return // No need to add any new URLs
	}

	// Add URLs found in the crawl to the queue
	filtered = filterURLs(db, src, result.URLs, true)
	err = db.AddToQueue(context.Background(), src.ID, result.Canonical, filtered, item.Depth+1, false)
	if err != nil {
		fmt.Printf("Error adding URLs to queue: %v\n", err)
	}
}

func filterURLs(db database.Database, src config.Source, urls []string, newOnly bool) []string {
	filtered := []string{}

	for _, fullURL := range urls {
		res, err := url.Parse(fullURL)
		if err != nil {
			fmt.Printf("%v\n", err)
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
