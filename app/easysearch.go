package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/crawler"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/fluxcapacitor2/easysearch/app/server"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	slogctx "github.com/veqryn/slog-context"
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
		slog.Error("Cron job panicked", "jobName", jobName, "jobId", jobID, "recoverData", recoverData)
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
		err := db.Setup(context.Background())

		if err != nil {
			panic(fmt.Sprintf("Failed to set up database: %v", err))
		}

		start := time.Now()
		err = db.CreateSpellfixIndex(context.Background())

		if err != nil {
			slog.Warn("Failed to create spellfix index", "error", err)
		} else {
			slog.Info("Created spellfix index", "time", fmt.Sprintf("%dms", time.Since(start).Milliseconds()))
		}

		for _, src := range config.Sources {
			if src.Embeddings.Enabled {
				err := db.SetupVectorTables(context.Background(), src.ID, src.Embeddings.Dimensions)
				if err != nil {
					panic(fmt.Sprintf("Failed to set up embeddings database tables for source %v: %v", src.ID, err))
				}
			}
		}
	}

	// Continuously pop items off each source's queue and crawl them
	go scheduleJobs(db, config)

	// If the base page for a source hasn't been crawled yet, queue it
	go startCrawl(context.Background(), db, config)

	// Refresh pages automatically after a certain amount of time
	go startRefreshJob(db, config)

	// Create an API server
	server.Start(db, config)
}

func startCrawl(ctx context.Context, db database.Database, config *config.Config) {
	// Find all sites listed in the configuration that haven't been crawled yet.
	// Then, add their base URLs to the queue.

	for _, src := range config.Sources {
		exists, err := db.HasDocument(context.Background(), src.ID, src.URL)

		if err != nil {
			slogctx.Error(ctx, "Failed to look up document", "sourceId", src.ID, "url", src.URL, "error", err)
		} else {
			if !*exists {
				// If the document wasn't found, it should be added to the queue
				parsed, err := url.Parse(src.URL)
				if err != nil {
					slogctx.Error(ctx, "Failed to parse start URL", "sourceId", src.ID, "url", src.URL, "error", err)
				} else {
					canonical, err := crawler.Canonicalize(ctx, src.ID, db, parsed)
					if err != nil {
						slogctx.Error(ctx, "Failed to find canonical URL for page", "sourceId", src.ID, "url", parsed.String(), "error", err)
						continue
					}
					err = db.AddToQueue(context.Background(), src.ID, canonical.String(), []string{canonical.String()}, 0, false)
					if err != nil {
						slogctx.Error(ctx, "Failed to add page to queue", "sourceId", src.ID, "url", src.URL, err)
					}
				}
			}
		}
	}
}
