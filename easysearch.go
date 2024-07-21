package main

import (
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// TODO: look into dependency injection instead of passing the DB and config into every function call

func main() {

	// Load configuration
	config, err := readConfig()

	if err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	// Set up a database connection using the specified driver
	var db Database

	switch config.DB.Driver {
	case "sqlite":
		sqlite, err := createSQLiteDatabase(config.DB.ConnectionString)
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
		err := db.setup()

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
	serve(db, config)
}

func startCrawl(db Database, config *config) {
	// Find all sites listed in the configuration that haven't been crawled yet.
	// Then, add their base URLs to the queue.

	for _, src := range config.Sources {
		exists, err := db.hasDocument(src.ID, src.URL)

		if err != nil {
			fmt.Printf("Failed to look up document %v in pages table\n", err)
		} else {
			if !*exists {
				// If the document wasn't found, it should be added to the queue
				parsed, err := url.Parse(src.URL)
				if err != nil {
					fmt.Printf("Failed to parse start URL for source %v (%v): %v\n", src.ID, src.URL, err)
				} else {
					canonical, err := canonicalize(src.ID, db, parsed)
					if err != nil {
						fmt.Printf("Failed to find canonical URL for page %v: %v\n", parsed.String(), err)
						continue
					}
					err = db.addToQueue(src.ID, []string{canonical.String()}, 0)
					if err != nil {
						fmt.Printf("Failed to add page %v to queue: %v\n", src.URL, err)
					}
				}
			}
		}
	}
}

func consumeQueue(db Database, config *config) {

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	for _, src := range config.Sources {
		interval := 60.0 / float64(src.Speed)

		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(interval*float64(time.Second))), gocron.NewTask(func() {
			// Pop the oldest item off the queue and crawl it.
			// This will result in other items being added to the queue, continuing the cycle.
			item, err := db.getFirstInQueue(src.ID)
			if err != nil {
				fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
			}
			if item != nil {
				{
					err := db.updateQueueEntry(src.ID, item.url, Processing)
					if err != nil {
						fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Processing, err)
					}
				}
				// The `item` is nil when there are no items in the queue
				result, err := crawl(src, item.depth, db, item.url)

				if err != nil {
					fmt.Printf("Error crawling URL %v from source %v: %v\n", item.url, src.ID, err)
					{
						err := db.updateQueueEntry(src.ID, item.url, Error)
						if err != nil {
							fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Error, err)
						}
					}

					// Add an entry to the pages table to prevent immediately recrawling the same URL when referred from other sources
					if result != nil {
						_, err := db.addDocument(src.ID, item.depth, result.canonical, Error, "", "", "")
						if err != nil {
							fmt.Printf("Failed to add page in 'error' state: %v\n", err)
						}
					}

				} else {
					// fmt.Printf("Crawl complete: %+v\n", result)
					err := db.updateQueueEntry(src.ID, item.url, Finished)
					if err != nil {
						fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Finished, err)
					}
				}

				{

					filtered := []string{}

					for _, fullURL := range result.urls {
						res, err := url.Parse(fullURL)
						if err != nil {
							fmt.Printf("%v\n", err)
						} else {

							crawled, err := db.hasDocument(src.ID, fullURL)

							if err == nil && *crawled {
								continue
							}

							if slices.Contains(src.AllowedDomains, res.Hostname()) {
								filtered = append(filtered, fullURL)
							}
						}
					}

					if item.depth+1 <= src.MaxDepth {
						err := db.addToQueue(src.ID, filtered, item.depth+1)
						if err != nil {
							fmt.Printf("Error adding URLs to queue: %v\n", err)
						}
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
			err := db.cleanQueue()
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

func handleRefresh(db Database, config *config) {
	// Refresh existing URLs after their source's specified period

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	{
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(1*time.Minute)), gocron.NewTask(func() {
			for _, src := range config.Sources {
				if src.Refresh.Enabled {
					err := db.queuePagesOlderThan(src.ID, src.Refresh.MinAge)

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
