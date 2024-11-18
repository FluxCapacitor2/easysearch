package main

import (
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
			processCrawlQueue(db, config, src)
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
			processEmbedQueue(db, config, src)
		})); err != nil {
			fmt.Printf("Error creating embedding job: %v", err)
		}
	}

	if err := db.Cleanup(); err != nil {
		fmt.Printf("error running Cleanup on startup: %v\n", err)
	}

	if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
		err := db.Cleanup()
		if err != nil {
			fmt.Printf("Error cleaning queue: %v\n", err)
		}

		err = db.StartEmbeddings(func(sourceID string) (chunkSize int, chunkOverlap int) {
			for _, src := range config.Sources {
				if src.ID == sourceID {
					return src.Embeddings.ChunkSize, src.Embeddings.ChunkOverlap
				}
			}
			return 200, 30
		})
		if err != nil {
			fmt.Printf("Error queueing pages that need embeddings: %v\n", err)
		}
	})); err != nil {
		fmt.Printf("Failed to create cleanup job: %v\n", err)
	}

	scheduler.Start()
}

func processEmbedQueue(db database.Database, config *config.Config, src config.Source) {
	item, err := db.PopEmbedQueue(src.ID)
	if err != nil {
		fmt.Printf("Failed to get next item in embed queue: %v\n", err)
	}
	if item == nil {
		// The queue is empty
		return
	}

	markFailure := func() {
		err := db.UpdateEmbedQueueEntry(item.ID, database.Error)
		if err != nil {
			fmt.Printf("failed to mark embedding queue item as Error: %v\n", err)
		}
	}

	vector, err := embedding.GetEmbeddings(src.Embeddings.OpenAIBaseURL, src.Embeddings.Model, src.Embeddings.APIKey, item.Content)
	if err != nil {
		fmt.Printf("error getting embeddings: %v\n", err)
		markFailure()
		return
	}

	err = db.AddEmbedding(item.PageID, src.ID, item.ChunkIndex, item.Content, vector)
	if err != nil {
		fmt.Printf("error saving embedding: %v\n", err)
		markFailure()
		return
	}

	err = db.UpdateEmbedQueueEntry(item.ID, database.Finished)
	if err != nil {
		fmt.Printf("failed to mark embedding queue item as Finished: %v\n", err)
	}
}

func processCrawlQueue(db database.Database, config *config.Config, src config.Source) {
	// Pop the oldest item off the queue and crawl it.
	item, err := db.PopQueue(src.ID)
	if err != nil {
		fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
	}
	if item == nil {
		// The queue is empty
		return
	}

	// The `item` is nil when there are no items in the queue
	result, err := crawler.Crawl(src, item.Depth, item.Referrer, db, item.URL)

	if err != nil {
		// Mark the queue entry as errored
		fmt.Printf("Error crawling URL %v from source %v: %v\n", item.URL, src.ID, err)
		{
			err := db.UpdateQueueEntry(item.ID, database.Error)
			if err != nil {
				fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.URL, database.Error, err)
			}
		}

		// Add an entry to the pages table to prevent immediately recrawling the same URL when referred from other sources.
		// Additionally, if refresh is enabled, another crawl attempt will be made after the refresh interval passes.
		if result != nil {
			_, err := db.AddDocument(src.ID, item.Depth, item.Referrer, result.Canonical, database.Error, "", "", "", err.Error())
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

			err = db.AddToEmbedQueue(result.PageID, filtered)

			if err != nil {
				fmt.Printf("error adding page chunks to embed queue: %v\n", err)
			}
		}

		// If the crawl completed successfully, mark the item as finished
		err = db.UpdateQueueEntry(item.ID, database.Finished)
		if err != nil {
			fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.URL, database.Finished, err)
		}
	}

	if item.Depth+1 >= src.MaxDepth {
		return // No need to add any new URLs
	}

	// Add URLs found in the crawl to the queue
	filtered := filterURLs(db, src, result.URLs)

	if item.Depth+1 <= src.MaxDepth {
		err := db.AddToQueue(src.ID, result.Canonical, filtered, item.Depth+1, false)
		if err != nil {
			fmt.Printf("Error adding URLs to queue: %v\n", err)
		}
	}
}

func filterURLs(db database.Database, src config.Source, urls []string) []string {
	filtered := []string{}

	for _, fullURL := range urls {
		res, err := url.Parse(fullURL)
		if err != nil {
			fmt.Printf("%v\n", err)
		} else {

			crawled, err := db.HasDocument(src.ID, fullURL)

			if err == nil && *crawled {
				continue
			}

			if slices.Contains(src.AllowedDomains, res.Hostname()) {
				filtered = append(filtered, fullURL)
			}
		}
	}
	return filtered
}
