package main

import (
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/server"
	"github.com/go-co-op/gocron/v2"
)

// TODO: look into dependency injection instead of passing the DB and config into every function call
// TODO: add a command-line option to rebuild the search index (https://sqlite.org/fts5.html#the_rebuild_command)

func main() {

	// Load configuration
	config, err := config.Read()

	if err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	// Set up a database connection using the specified driver
	var db database.Database

	switch config.DB.Driver {
	case "sqlite":
		sqlite, err := database.SQLite(config.DB.ConnectionString)
		if err != nil {
			panic(fmt.Sprintf("Error opening SQLite database: %v", err))
		}
		db = sqlite
	// case "postgres":
	// 	postgres, err := createPostgresDatabase(config.DB.ConnectionString)
	// 	if err != nil {
	// 		panic(fmt.Sprintf("Error opening Postgres database: %v", err))
	// 	}
	// 	db = postgres
	default:
		panic(fmt.Sprintf("Unknown database driver: %v. Valid drivers include: sqlite, postgres.", config.DB.Driver))
	}

	{
		// Create DB tables if they don't exist (and set SQLite to WAL mode)
		err := db.Setup()

		if err != nil {
			panic(fmt.Sprintf("Failed to set up database: %v", err))
		}
	}

	// Continuously pop items off each source's queue and crawl them
	go consumeQueue(db, config)

	// If the base page for a source hasn't been crawled yet, queue it
	go startCrawl(db, config)

	// Refresh pages automatically after a certain amount of days
	go handleRefresh(db, config)

	// Create an API server
	server.Start(db, config)
}

func startCrawl(db database.Database, config *config.Config) {
	// Find all sites listed in the configuration that haven't been crawled yet.
	// Then, add their base URLs to the queue.

	for _, src := range config.Sources {
		exists, err := db.HasDocument(src.ID, src.URL)

		if err != nil {
			fmt.Printf("Failed to look up document '%v'/'%v' in pages table: %v\n", src.ID, src.URL, err)
		} else {
			if !*exists {
				// If the document wasn't found, it should be added to the queue
				parsed, err := url.Parse(src.URL)
				if err != nil {
					fmt.Printf("Failed to parse start URL for source %v (%v): %v\n", src.ID, src.URL, err)
				} else {
					canonical, err := crawler.Canonicalize(src.ID, db, parsed)
					if err != nil {
						fmt.Printf("Failed to find canonical URL for page %v: %v\n", parsed.String(), err)
						continue
					}
					err = db.AddToQueue(src.ID, []string{canonical.String()}, 0)
					if err != nil {
						fmt.Printf("Failed to add page %v to queue: %v\n", src.URL, err)
					}
				}
			}
		}
	}
}

func consumeQueue(db database.Database, config *config.Config) {

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	for _, src := range config.Sources {
		interval := 60.0 / float64(src.Speed)

		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(interval*float64(time.Second))), gocron.NewTask(func() {
			// Pop the oldest item off the queue and crawl it.
			item, err := db.PopQueue(src.ID)
			if err != nil {
				fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
			}
			if item != nil {
				// The `item` is nil when there are no items in the queue
				result, err := crawler.Crawl(src, item.Depth, db, item.URL)

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
						err := db.AddDocument(src.ID, item.Depth, result.Canonical, database.Error, "", "", "")
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
					err := db.AddToQueue(src.ID, filtered, item.Depth+1)
					if err != nil {
						fmt.Printf("Error adding URLs to queue: %v\n", err)
					}
				}
			}
		}))

		if err != nil {
			fmt.Printf("Error creating crawl job: %v", err)
		}
	}

	{
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(5*time.Minute)), gocron.NewTask(func() {
			err := db.CleanQueue()
			if err != nil {
				fmt.Printf("Error cleaning queue: %v\n", err)
			}
		}))

		if err != nil {
			fmt.Printf("Failed to create cleanup job: %v\n", err)
		}
	}

	scheduler.Start()
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

func handleRefresh(db database.Database, config *config.Config) {
	// Refresh existing URLs after their source's specified period

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	{
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(1*time.Minute)), gocron.NewTask(func() {
			for _, src := range config.Sources {
				if src.Refresh.Enabled {
					err := db.QueuePagesOlderThan(src.ID, src.Refresh.MinAge)

					if err != nil {
						fmt.Printf("Error processing refresh for source %v: %v\n", src.ID, err)
					}
				}
			}
		}))

		if err != nil {
			panic(fmt.Sprintf("Failed to create gocron job: %v\n", err))
		}
	}

	scheduler.Start()
}
