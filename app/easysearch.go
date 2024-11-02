package main

import (
	"fmt"
	"net/url"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/server"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
)

// TODO: look into dependency injection instead of passing the DB and config into every function call
// TODO: add a command-line option to rebuild the search index (https://sqlite.org/fts5.html#the_rebuild_command)

func main() {

	// Load configuration
	config, err := config.Read()

	if err != nil {
		panic(fmt.Sprintf("Invalid configuration: %v", err))
	}

	gocron.AfterJobRunsWithPanic(func(jobID uuid.UUID, jobName string, recoverData interface{}) {
		fmt.Printf("Panic in job %v (ID: %v): %+v\n", jobName, jobID, recoverData)
	})

	// Set up a database connection using the specified driver
	var db database.Database

	switch config.DB.Driver {
	case "sqlite":
		sqlite, err := database.SQLiteFromFile(config.DB.ConnectionString)
		if err != nil {
			panic(fmt.Sprintf("Error opening SQLite database: %v", err))
		}
		db = sqlite
	default:
		panic(fmt.Sprintf("Unknown database driver: %v. Valid drivers include: sqlite.", config.DB.Driver))
	}

	{
		// Create DB tables if they don't exist (and set SQLite to WAL mode)
		err := db.Setup()

		if err != nil {
			panic(fmt.Sprintf("Failed to set up database: %v", err))
		}
	}

	// Continuously pop items off each source's queue and crawl them
	go startQueueJob(db, config)

	// If the base page for a source hasn't been crawled yet, queue it
	go startCrawl(db, config)

	// Refresh pages automatically after a certain amount of time
	go startRefreshJob(db, config)

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
					err = db.AddToQueue(src.ID, canonical.String(), []string{canonical.String()}, 0, false)
					if err != nil {
						fmt.Printf("Failed to add page %v to queue: %v\n", src.URL, err)
					}
				}
			}
		}
	}
}
