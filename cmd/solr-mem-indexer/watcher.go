package main

import (
	"context"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher polls repositories for new commits and triggers incremental indexing.
// It uses fsnotify to react immediately to:
//   - .solr-mem.yaml config file changes (reload repo list)
//   - git ref changes in configured repos (re-index on pull/fetch)
//
// Polling on PollInterval is kept as a fallback for environments where
// filesystem events don't propagate (e.g. Docker bind mounts on macOS).
type Watcher struct {
	indexer         *Indexer
	cfg             *Config
	lastConfigState []configFileState
	configChanged   chan struct{}
	repoChanged     chan string // repo path that changed
}

// NewWatcher creates a new repository watcher.
func NewWatcher(indexer *Indexer, cfg *Config) *Watcher {
	return &Watcher{
		indexer:       indexer,
		cfg:           cfg,
		configChanged: make(chan struct{}, 1),
		repoChanged:   make(chan string, 16),
	}
}

// Run starts the watch loop. It blocks until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	go w.watchFiles(ctx)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Watcher stopped")
			return
		case <-w.configChanged:
			log.Println("Config change detected, re-indexing all repos...")
			w.poll(ctx)
		case repoPath := <-w.repoChanged:
			w.indexSingleRepo(ctx, repoPath)
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// indexSingleRepo finds and indexes a specific repo by path.
// Drains any additional queued changes for the same or other repos first (debounce).
func (w *Watcher) indexSingleRepo(ctx context.Context, repoPath string) {
	// Drain any queued repo changes to avoid redundant re-indexes
	changed := map[string]bool{repoPath: true}
	for {
		select {
		case p := <-w.repoChanged:
			changed[p] = true
		default:
			goto done
		}
	}
done:
	for p := range changed {
		for _, repo := range w.cfg.Repos {
			if repo.Path == p {
				log.Printf("Git ref changed, re-indexing %s", p)
				if err := w.indexer.IndexRepo(ctx, repo); err != nil {
					log.Printf("Error indexing %s: %v", p, err)
				}
				break
			}
		}
	}
}

// watchFiles runs a single fsnotify watcher for both config files and repo git dirs.
func (w *Watcher) watchFiles(ctx context.Context) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Warning: fsnotify unavailable, relying on polling: %v", err)
		return
	}
	defer fsw.Close()

	w.refreshWatches(fsw)

	rescanTicker := time.NewTicker(60 * time.Second)
	defer rescanTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fsw.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			w.handleFSEvent(event)
		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			log.Printf("Warning: fsnotify error: %v", err)
		case <-rescanTicker.C:
			w.refreshWatches(fsw)
		}
	}
}

// handleFSEvent routes a filesystem event to the appropriate channel.
func (w *Watcher) handleFSEvent(event fsnotify.Event) {
	base := filepath.Base(event.Name)

	// Config file change
	if base == configFileName {
		log.Printf("Config file changed: %s (%s)", event.Name, event.Op)
		select {
		case w.configChanged <- struct{}{}:
		default:
		}
		return
	}

	// Git ref change — look up which repo this git dir belongs to
	if repoPath, ok := w.gitDirToRepo(filepath.Dir(event.Name)); ok {
		select {
		case w.repoChanged <- repoPath:
		default:
		}
	}
}

// gitDirToRepo maps a git directory path back to the repo path it belongs to.
func (w *Watcher) gitDirToRepo(gitDir string) (string, bool) {
	for _, repo := range w.cfg.Repos {
		repoGitDir := resolveGitDir(repo.Path)
		if repoGitDir == "" {
			continue
		}
		// Match if the event dir is the git dir itself or a parent (e.g. refs/heads/)
		if gitDir == repoGitDir || strings.HasPrefix(gitDir, repoGitDir+string(filepath.Separator)) {
			return repo.Path, true
		}
		// Also check the common dir (bare.git) for packed-refs and loose refs
		commonDir := resolveGitCommonDir(repo.Path)
		if commonDir != "" && (gitDir == commonDir || strings.HasPrefix(gitDir, commonDir+string(filepath.Separator))) {
			return repo.Path, true
		}
	}
	return "", false
}

// refreshWatches sets up fsnotify watches for config files and repo git dirs.
func (w *Watcher) refreshWatches(fsw *fsnotify.Watcher) {
	watched := make(map[string]bool)
	for _, p := range fsw.WatchList() {
		watched[p] = true
	}

	// Watch config files
	if w.cfg.ScanDir != "" {
		_, states, err := ScanForConfigs(w.cfg.ScanDir, w.cfg.DefaultBranch)
		if err != nil {
			log.Printf("Warning: config scan for watch setup failed: %v", err)
		}
		for _, s := range states {
			if !watched[s.path] {
				if err := fsw.Add(s.path); err != nil {
					log.Printf("Warning: cannot watch config %s: %v", s.path, err)
				} else {
					log.Printf("Watching config: %s", s.path)
				}
				watched[s.path] = true
			}
		}
	}

	// Watch git dirs for each configured repo
	for _, repo := range w.cfg.Repos {
		// Watch the worktree git dir (has HEAD, FETCH_HEAD, etc.)
		gitDir := resolveGitDir(repo.Path)
		if gitDir != "" && !watched[gitDir] {
			if err := fsw.Add(gitDir); err != nil {
				log.Printf("Warning: cannot watch git dir %s: %v", gitDir, err)
			} else {
				log.Printf("Watching git dir: %s", gitDir)
			}
			watched[gitDir] = true
		}

		// Watch the common dir's refs/heads/ for branch updates (packed refs get
		// unpacked to loose refs on update). Also watch common dir for packed-refs.
		commonDir := resolveGitCommonDir(repo.Path)
		if commonDir != "" {
			refsDir := filepath.Join(commonDir, "refs", "heads")
			if info, err := os.Stat(refsDir); err == nil && info.IsDir() && !watched[refsDir] {
				if err := fsw.Add(refsDir); err != nil {
					log.Printf("Warning: cannot watch refs %s: %v", refsDir, err)
				} else {
					log.Printf("Watching refs: %s", refsDir)
				}
				watched[refsDir] = true
			}
			if !watched[commonDir] {
				if err := fsw.Add(commonDir); err != nil {
					log.Printf("Warning: cannot watch common dir %s: %v", commonDir, err)
				} else {
					log.Printf("Watching common dir: %s", commonDir)
				}
				watched[commonDir] = true
			}
		}
	}
}

// resolveGitDir returns the git directory for a repo path.
// For normal repos this is <path>/.git, for worktrees it follows the gitdir pointer.
func resolveGitDir(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}
	return filepath.Clean(dir)
}

// resolveGitCommonDir returns the common git directory (the bare.git for worktrees,
// same as git dir for normal repos).
func resolveGitCommonDir(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}
	return filepath.Clean(dir)
}

func (w *Watcher) poll(ctx context.Context) {
	// Re-scan for config files if using scan-dir mode
	if w.cfg.ScanDir != "" {
		repos, states, err := ScanForConfigs(w.cfg.ScanDir, w.cfg.DefaultBranch)
		if err != nil {
			log.Printf("Warning: config scan failed: %v", err)
		} else if configsChanged(w.lastConfigState, states) {
			log.Printf("Config files changed, reloaded %d repos", len(repos))
			w.cfg.Repos = repos
			w.lastConfigState = states
		}
	}

	for i, repo := range w.cfg.Repos {
		// Splay between repos to avoid hammering Solr
		if i > 0 {
			splay := time.Duration(rand.Intn(10)+5) * time.Second
			log.Printf("Splay: waiting %s before next repo", splay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(splay):
			}
		}

		// Try to fetch latest changes
		if err := gitFetch(repo.Path, repo.Branch); err != nil {
			log.Printf("Warning: git fetch failed for %s: %v", repo.Path, err)
		}

		if err := w.indexer.IndexRepo(ctx, repo); err != nil {
			log.Printf("Error indexing %s: %v", repo.Path, err)
		}
	}
}

// configsChanged returns true if the set of config files or their mtimes differ.
func configsChanged(prev, curr []configFileState) bool {
	if len(prev) != len(curr) {
		return true
	}
	m := make(map[string]time.Time, len(prev))
	for _, s := range prev {
		m[s.path] = s.modTime
	}
	for _, s := range curr {
		if t, ok := m[s.path]; !ok || !t.Equal(s.modTime) {
			return true
		}
	}
	return false
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
	if err := cmd.Run(); err != nil {
		return err
	}

	// Fast-forward local branch to the remote ref. Without this, gitHeadSHA
	// keeps reading the stale local HEAD even though origin/<branch> has
	// advanced, and IndexRepo skips with "already up to date". The mirror
	// at repoPath is indexer-owned and never carries local commits, so a
	// hard reset is safe and idempotent.
	cmd = exec.Command("git", "-C", repoPath, "reset", "--hard", remote+"/"+branch)
	return cmd.Run()
}
