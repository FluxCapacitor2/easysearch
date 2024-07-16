package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func main() {

	// Load configuration
	config, err := readConfig()

	if err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	// Set up a database connection using the specified driver
	var db Database

	switch config.Db.Driver {
	case "sqlite":
		sqlite, err := createSQLiteDatabase(config.Db.ConnectionString)
		if err != nil {
			panic(fmt.Sprintf("Error opening SQLite database: %v", err))
		}
		db = sqlite
	// case "postgres":
	// 	postgres, err := createPostgresDatabase(config.Db.ConnectionString)
	// 	if err != nil {
	// 		panic(fmt.Sprintf("Error opening Postgres database: %v", err))
	// 	}
	// 	db = postgres
	default:
		panic(fmt.Sprintf("Unknown database driver: %v. Valid drivers include: sqlite, postgres.", config.Db.Driver))
	}

	{
		// Create DB tables if they don't exist (and set SQLite to WAL mode)
		err := db.setup()

		if err != nil {
			panic(fmt.Sprintf("Failed to set up database: %v", err))
		}
	}

	// Create an API server
	http.Handle("/", new(httpHandler))
	http.HandleFunc("/search", func(w http.ResponseWriter, req *http.Request) {
		src := req.URL.Query()["source"]
		q := req.URL.Query().Get("q")
		if q != "" && src != nil && len(src) > 0 {
			results, err := db.search(src, q)
			if err != nil {
				w.Write([]byte(fmt.Sprintf("Error searching!\n\n%v", err)))
			} else {
				json, err := json.Marshal(results)
				if err == nil {
					w.Header().Add("Content-Type", "application/json")
					if len(results) == 0 {
						// By default, marshalling an empty array results in `null`. Instead, return an empty results list.
						w.Write([]byte(`{"success":"true","results":[]}`))
					} else {
						w.Write([]byte(fmt.Sprintf(`{"success":"true","results":%s}`, json)))
					}
				} else {
					w.Write([]byte(fmt.Sprintf("Error formatting JSON: %v\n", err)))
				}
			}
		} else {
			w.WriteHeader(400)
			w.Write([]byte(`{"success":"false","error": "Bad request"}`))
		}
	})

	go consumeQueue(db, config)
	go startCrawl(db, config)
	go handleRefresh(db, config)

	addr := fmt.Sprintf("%v:%v", config.Http.Listen, config.Http.Port)
	fmt.Printf("Listening on http://%v\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func startCrawl(db Database, config *config) {
	// Find all sites listed in the configuration that haven't been crawled yet.
	// Then, add their base URLs to the queue.

	for _, src := range config.Sources {
		exists, err := db.hasDocument(src.Id, src.Url)

		if err != nil {
			fmt.Printf("Failed to look up document %v in pages table\n", err)
		} else {
			if !*exists {
				// If the document wasn't found, it should be added to the queue
				parsed, err := url.Parse(src.Url)
				if err != nil {
					fmt.Printf("Failed to parse start URL for source %v (%v): %v\n", src.Id, src.Url, err)
				} else {
					formatted := canonicalize(parsed).String()
					err := db.addToQueue(src.Id, []string{formatted}, 0)
					if err != nil {
						fmt.Printf("Failed to add page %v to queue: %v\n", src.Url, err)
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
			item, err := db.getFirstInQueue(src.Id)
			if err != nil {
				fmt.Printf("Failed to get next item in crawl queue: %v\n", err)
			}
			if item != nil {
				{
					err := db.updateQueueEntry(src.Id, item.url, Processing)
					if err != nil {
						fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Processing, err)
					}
				}
				// The `item` is nil when there are no items in the queue
				urls, err := crawl(src, db, item.url)

				if err != nil {
					fmt.Printf("Error crawling URL %v from source %v: %v\n", item.url, src.Id, err)
					err := db.updateQueueEntry(src.Id, item.url, Error)
					if err != nil {
						fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Error, err)
					}
				} else {
					err := db.updateQueueEntry(src.Id, item.url, Finished)
					if err != nil {
						fmt.Printf("Failed to update queue item status for page %v to %v: %v\n", item.url, Finished, err)
					}
				}

				{

					filtered := []string{}

					for _, fullUrl := range urls {
						res, err := url.Parse(fullUrl)
						if err != nil {
							fmt.Printf("%v\n", err)
						} else {
							if slices.Contains(src.AllowedDomains, res.Hostname()) {
								filtered = append(filtered, fullUrl)
							}
						}
					}

					if item.depth+1 <= src.MaxDepth {
						err := db.addToQueue(src.Id, filtered, item.depth+1)
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
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(1*time.Hour)), gocron.NewTask(func() {
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
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(1*time.Hour)), gocron.NewTask(func() {
			for _, src := range config.Sources {
				if src.Refresh.Enabled {
					db.queuePagesOlderThan(src.Id, src.Refresh.MinAge)
				}
			}
		}))

		if err != nil {
			panic(fmt.Sprintf("Failed to create gocron job: %v\n", err))
		}
	}

	scheduler.Start()
}
