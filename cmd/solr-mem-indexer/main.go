package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/arreyder/solr-mem/internal/solr"
)

func main() {
	var (
		repoPath  string
		oneShot   bool
		watchMode bool
	)

	flag.StringVar(&repoPath, "repo", "", "Path to a local git repository to index")
	flag.BoolVar(&oneShot, "once", false, "Index once and exit (no polling)")
	flag.BoolVar(&watchMode, "watch", false, "Run in watch mode, polling for changes")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI repo overrides env
	if repoPath != "" {
		cfg.Repos = []RepoConfig{{
			Path:   repoPath,
			Branch: cfg.DefaultBranch,
		}}
	}

	if len(cfg.Repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repositories configured. Use --repo <path> or INDEX_REPOS env var.")
		flag.Usage()
		os.Exit(1)
	}

	solrClient := solr.NewClient(cfg.SolrURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ensure the code collection exists (creates it if needed)
	log.Printf("Ensuring code collection at %s...", cfg.SolrURL)
	if err := solrClient.EnsureCollection(ctx, cfg.SolrConfigDir); err != nil {
		log.Printf("Warning: could not auto-create collection: %v", err)
		log.Printf("The collection may need to be created manually. Attempting to continue...")
	}

	// Verify Solr connectivity
	if err := solrClient.Ping(ctx); err != nil {
		log.Fatalf("Cannot reach Solr at %s: %v", cfg.SolrURL, err)
	}
	log.Printf("Connected to Solr at %s", cfg.SolrURL)

	// Acquire process-level lock to prevent concurrent indexing
	lock := NewFileLock(cfg.CloneDir)
	if err := lock.Lock(); err != nil {
		log.Fatalf("Cannot acquire lock: %v", err)
	}
	defer lock.Unlock()
	log.Printf("Acquired index lock")

	indexer := NewIndexer(solrClient, cfg)

	if oneShot || !watchMode {
		// Index all configured repos once
		for _, repo := range cfg.Repos {
			log.Printf("Indexing repository: %s", repo.Path)
			if err := indexer.IndexRepo(ctx, repo); err != nil {
				log.Fatalf("Failed to index %s: %v", repo.Path, err)
			}
			log.Printf("Successfully indexed: %s", repo.Path)
		}
		if !watchMode {
			return
		}
	}

	// Watch mode: poll for changes
	log.Printf("Starting watch mode (poll interval: %s)", cfg.PollInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	watcher := NewWatcher(indexer, cfg)
	go watcher.Run(ctx)

	sig := <-sigCh
	log.Printf("Received %s, shutting down...", sig)
	cancel()
}
