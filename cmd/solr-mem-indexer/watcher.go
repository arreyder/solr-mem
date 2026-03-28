package main

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"
)

// Watcher polls repositories for new commits and triggers incremental indexing.
type Watcher struct {
	indexer *Indexer
	cfg     *Config
}

// NewWatcher creates a new repository watcher.
func NewWatcher(indexer *Indexer, cfg *Config) *Watcher {
	return &Watcher{
		indexer: indexer,
		cfg:     cfg,
	}
}

// Run starts the polling loop. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Watcher stopped")
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *Watcher) poll(ctx context.Context) {
	for _, repo := range w.cfg.Repos {
		// Try to fetch latest changes
		if err := gitFetch(repo.Path, repo.Branch); err != nil {
			log.Printf("Warning: git fetch failed for %s: %v", repo.Path, err)
		}

		if err := w.indexer.IndexRepo(ctx, repo); err != nil {
			log.Printf("Error indexing %s: %v", repo.Path, err)
		}
	}
}

func gitFetch(repoPath, branch string) error {
	// Only fetch if it's a git repo with a remote
	cmd := exec.Command("git", "-C", repoPath, "remote")
	out, err := cmd.Output()
	if err != nil {
		return nil // no remote, skip fetch
	}
	remotes := strings.TrimSpace(string(out))
	if remotes == "" {
		return nil
	}

	// Fetch the tracked branch
	remote := strings.Split(remotes, "\n")[0]
	cmd = exec.Command("git", "-C", repoPath, "fetch", remote, branch)
	return cmd.Run()
}
