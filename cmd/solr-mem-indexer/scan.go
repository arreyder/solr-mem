package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const configFileName = ".solr-mem.yaml"

// ScanConfig represents the contents of a .solr-mem.yaml file.
type ScanConfig struct {
	Repos []ScanRepo `yaml:"repos"`
}

// ScanRepo is a single repo entry in a .solr-mem.yaml file.
type ScanRepo struct {
	Path   string `yaml:"path"`
	Branch string `yaml:"branch,omitempty"`
}

// configFileState tracks a discovered config file and its last modification time.
type configFileState struct {
	path    string
	modTime time.Time
}

// ScanForConfigs walks rootDir looking for .solr-mem.yaml files and returns
// the merged list of RepoConfigs with paths resolved relative to each config file,
// plus the state of each config file found (for change detection).
func ScanForConfigs(rootDir, defaultBranch string) ([]RepoConfig, []configFileState, error) {
	var repos []RepoConfig
	var states []configFileState

	// maxDepth limits how deep we walk looking for config files.
	// Typical layout: rootDir/org/repo/.solr-mem.yaml (depth 3).
	const maxDepth = 4
	rootDepth := strings.Count(filepath.Clean(rootDir), string(filepath.Separator))

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - rootDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}
			if shouldSkipScanDir(filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != configFileName {
			return nil
		}

		cfgDir := filepath.Dir(path)
		found, err := parseConfigFile(path, cfgDir, defaultBranch)
		if err != nil {
			log.Printf("Warning: skipping %s: %v", path, err)
			return nil
		}

		states = append(states, configFileState{
			path:    path,
			modTime: info.ModTime(),
		})
		repos = append(repos, found...)
		return nil
	})

	return repos, states, err
}

func parseConfigFile(path, baseDir, defaultBranch string) ([]RepoConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var cfg ScanConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	var repos []RepoConfig
	for _, r := range cfg.Repos {
		repoPath := r.Path
		if !filepath.IsAbs(repoPath) {
			repoPath = filepath.Join(baseDir, repoPath)
		}
		branch := r.Branch
		if branch == "" {
			branch = defaultBranch
		}
		repos = append(repos, RepoConfig{
			Path:   repoPath,
			Branch: branch,
		})
	}

	log.Printf("Loaded %d repos from %s", len(repos), path)
	return repos, nil
}

func shouldSkipScanDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "local_vendor",
		".idea", ".vscode", ".cursor",
		"dist", "build", ".next", ".nuxt", "target", "bin",
		"__pycache__", ".mypy_cache", ".pytest_cache":
		return true
	}
	// Skip bare git directories
	if strings.HasSuffix(name, ".git") {
		return true
	}
	return false
}
