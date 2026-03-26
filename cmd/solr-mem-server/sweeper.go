package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

const defaultSweeperInterval = 60 * time.Second

// startSweeper launches a background goroutine that periodically deletes
// expired memories from Solr.
func startSweeper(ctx context.Context, client *solr.Client) {
	interval := defaultSweeperInterval
	if v := os.Getenv("SWEEPER_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			interval = time.Duration(secs) * time.Second
		}
	}

	go func() {
		log.Printf("Expiration sweeper started (interval: %s)", interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Expiration sweeper stopped")
				return
			case <-ticker.C:
				sweep(ctx, client)
			}
		}
	}()
}

func sweep(ctx context.Context, client *solr.Client) {
	// Delete all documents where expires_at is in the past
	query := fmt.Sprintf("expires_at:[* TO %s]", time.Now().UTC().Format(time.RFC3339))
	if err := client.DeleteByQuery(ctx, query); err != nil {
		log.Printf("Sweeper error: %v", err)
	}
}
