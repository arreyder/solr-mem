package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arreyder/solr-mem/internal/embed"
	"github.com/arreyder/solr-mem/internal/solr"
)

func main() {
	var (
		solrURL     = flag.String("solr-url", getenv("SOLR_URL", "http://localhost:8983/solr/memories"), "Memories collection URL")
		batchSize   = flag.Int("batch-size", 50, "Docs per Solr page")
		concurrency = flag.Int("concurrency", 4, "Parallel embed calls")
		dryRun      = flag.Bool("dry-run", false, "Compute embeddings but do not write")
		maxDocs     = flag.Int("max-docs", 0, "Cap total docs embedded (0 = unlimited)")
		force       = flag.Bool("force", false, "Re-embed docs that already have an embedding")
		pauseMS     = flag.Int("pause-ms", 0, "Sleep between batches in milliseconds")
	)
	flag.Parse()

	provider, err := embed.FromEnv()
	if err != nil {
		log.Fatalf("embed provider init: %v", err)
	}
	if provider == nil {
		log.Fatalf("no embedding provider configured — set OPENAI_API_KEY")
	}
	log.Printf("backfill: provider=%s dim=%d", provider.Name(), provider.Dim())

	client := solr.NewClient(*solrURL)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	stats, err := Run(ctx, client, provider, Options{
		BatchSize:   *batchSize,
		Concurrency: *concurrency,
		DryRun:      *dryRun,
		MaxDocs:     *maxDocs,
		Force:       *force,
		Pause:       time.Duration(*pauseMS) * time.Millisecond,
	})
	if err != nil {
		log.Fatalf("backfill: %v", err)
	}
	log.Printf("backfill done: scanned=%d skipped=%d embedded=%d written=%d errors=%d",
		stats.Scanned, stats.Skipped, stats.Embedded, stats.Written, stats.Errors)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
