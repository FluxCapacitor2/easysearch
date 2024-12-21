package main

import (
	"context"
	"fmt"
	"time"

	"github.com/fluxcapacitor2/easysearch/app/config"
	"github.com/fluxcapacitor2/easysearch/app/database"
	"github.com/go-co-op/gocron/v2"
)

// Refresh existing URLs after their source's specified period by adding them to the crawl queue
func startRefreshJob(db database.Database, config *config.Config) {

	scheduler, err := gocron.NewScheduler()

	if err != nil {
		panic(fmt.Sprintf("Failed to create gocron scheduler: %v", err))
	}

	{
		_, err := scheduler.NewJob(gocron.DurationJob(time.Duration(1*time.Minute)), gocron.NewTask(func() {
			for _, src := range config.Sources {
				if src.Refresh.Enabled {
					err := db.QueuePagesOlderThan(context.Background(), src.ID, src.Refresh.MinAge)

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
