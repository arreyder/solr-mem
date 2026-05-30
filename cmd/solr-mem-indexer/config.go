package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds indexer configuration loaded from environment variables.
type Config struct {
	SolrURL       string        // Solr code collection URL
	SolrConfigDir string        // Path to Solr config files for code collection
	Repos         []RepoConfig  // Repositories to index
	PollInterval  time.Duration // How often to check for new commits
	CloneDir      string        // Where to clone/store repos
	DefaultBranch string        // Branch to track
	ScanDir       string        // Root directory to scan for .solr-mem.yaml files
	ControlAddr   string        // Local address for the force-reindex control endpoint ("" / "off" disables)
}

// RepoConfig describes a single repository to index.
type RepoConfig struct {
	Path     string   // Local filesystem path or remote URL
	Branch   string   // Branch to track (overrides default)
	Features []string // Optional features to enable (e.g., "frontend-selectors")
}

// HasFeature returns true if the named feature is enabled for this repo.
func (r RepoConfig) HasFeature(name string) bool {
	for _, f := range r.Features {
		if f == name {
			return true
		}
	}
	return false
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		SolrURL:       envOr("SOLR_URL_CODE", "http://pax89.local:8983/solr/code"),
		SolrConfigDir: envOr("SOLR_CONFIG_DIR", ""),
		CloneDir:      envOr("CLONE_DIR", "/tmp/solr-mem-repos"),
		DefaultBranch: envOr("INDEX_BRANCH", "main"),
		ControlAddr:   envOr("INDEXER_CONTROL_ADDR", "127.0.0.1:7071"),
	}

	// Allow explicitly disabling the control endpoint.
	if v := strings.ToLower(cfg.ControlAddr); v == "off" || v == "disabled" || v == "none" {
		cfg.ControlAddr = ""
	}

	// Poll interval
	intervalSec, err := strconv.Atoi(envOr("INDEX_INTERVAL", "300"))
	if err != nil {
		return nil, fmt.Errorf("invalid INDEX_INTERVAL: %w", err)
	}
	cfg.PollInterval = time.Duration(intervalSec) * time.Second

	// Parse repos
	reposStr := os.Getenv("INDEX_REPOS")
	if reposStr == "" {
		// Check for CLI args
		return cfg, nil
	}

	for _, r := range strings.Split(reposStr, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		cfg.Repos = append(cfg.Repos, RepoConfig{
			Path:   r,
			Branch: cfg.DefaultBranch,
		})
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
