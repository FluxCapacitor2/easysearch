package main

import (
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
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
			crawlFirstInQueue(db, src)
		})); err != nil {
			fmt.Printf("Error creating crawl job: %v", err)
		}
	}

	if _, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
		err := db.CleanQueue()
		if err != nil {
			fmt.Printf("Error cleaning queue: %v\n", err)
		}
	})); err != nil {
		fmt.Printf("Failed to create cleanup job: %v\n", err)
	}

	scheduler.Start()
}

func crawlFirstInQueue(db database.Database, src config.Source) {
	// Pop the oldest item off the queue and crawl it.
	item, err := db.PopQueue(src.ID)
	if err != nil {
		fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
	}
	if item != nil {
		// The `item` is nil when there are no items in the queue
		result, err := crawler.Crawl(src, item.Depth, item.Referrer, db, item.URL)

		if err != nil {
			// Mark the queue entry as errored
			fmt.Printf("Error crawling URL %v from source %v: %v\n", item.URL, src.ID, err)
			{
				err := db.UpdateQueueEntry(src.ID, item.URL, database.Error)
				if err != nil {
					fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.URL, database.Error, err)
				}
			}

			// Add an entry to the pages table to prevent immediately recrawling the same URL when referred from other sources.
			// Additionally, if refresh is enabled, another crawl attempt will be made after the refresh interval passes.
			if result != nil {
				err := db.AddDocument(src.ID, item.Depth, item.Referrer, result.Canonical, database.Error, "", "", "")
				if err != nil {
					fmt.Printf("Failed to add page in 'error' state: %v\n", err)
				}
			}

		} else {
			// If the crawl completed successfully, mark the item as finished
			err := db.UpdateQueueEntry(src.ID, item.URL, database.Finished)
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
			err := db.AddToQueue(src.ID, result.Canonical, filtered, item.Depth+1)
			if err != nil {
				fmt.Printf("Error adding URLs to queue: %v\n", err)
			}
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
