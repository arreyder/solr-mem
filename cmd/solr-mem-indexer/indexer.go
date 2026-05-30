package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/parser"
	"github.com/arreyder/solr-mem/internal/solr"
)

// Indexer processes git repositories into Solr code documents.
type Indexer struct {
	solr     *solr.Client
	cfg      *Config
	registry *parser.Registry
}

// NewIndexer creates a new repository indexer.
func NewIndexer(client *solr.Client, cfg *Config) *Indexer {
	return &Indexer{
		solr:     client,
		cfg:      cfg,
		registry: parser.NewRegistry(),
	}
}

// IndexRepo performs a full or incremental index of a repository.
func (idx *Indexer) IndexRepo(ctx context.Context, repo RepoConfig) error {
	repoPath, err := idx.resolveRepo(repo)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}

	// Use the original source path for a stable repo ID, not the clone dir path
	repoID := shortHash(repo.Path)
	commitSHA, err := gitHeadSHA(repoPath)
	if err != nil {
		return fmt.Errorf("get HEAD SHA: %w", err)
	}

	// Check if we've already indexed this commit
	lastSHA, err := idx.getLastIndexedSHA(ctx, repoID)
	if err != nil {
		log.Printf("Warning: could not check last indexed SHA: %v", err)
	}

	if lastSHA == commitSHA {
		log.Printf("Repository %s already at %s, skipping", repoPath, commitSHA[:8])
		idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "complete", "already up to date", 0, 0)
		return nil
	}

	var indexErr error
	if lastSHA != "" {
		indexErr = idx.incrementalIndex(ctx, repoPath, repoID, repo, lastSHA, commitSHA)
	} else {
		indexErr = idx.fullIndex(ctx, repoPath, repoID, repo, commitSHA)
	}

	if indexErr != nil {
		idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "error", indexErr.Error(), 0, 0)
		return indexErr
	}

	return nil
}

// InvalidateRepo clears the stored repo-level doc so the next IndexRepo sees no
// last-indexed SHA and performs a fresh full index (which also rebuilds the
// cross-reference and package/repo docs that the incremental path skips). Used
// by the force-reindex control path.
func (idx *Indexer) InvalidateRepo(ctx context.Context, repo RepoConfig) error {
	repoID := shortHash(repo.Path)
	return idx.solr.DeleteByQuery(ctx, fmt.Sprintf("repo_id:%q AND doc_level:%q", repoID, "repo"))
}

// writeStatus writes or updates a status document for a repo in the code collection.
func (idx *Indexer) writeStatus(ctx context.Context, repoID, repoPath, commitSHA, state, message string, filesProcessed, filesTotal int) {
	now := time.Now().UTC()
	content := fmt.Sprintf("state: %s\nrepo: %s\ncommit: %s\nfiles_processed: %d\nfiles_total: %d",
		state, repoPath, commitSHA, filesProcessed, filesTotal)
	if message != "" {
		content += fmt.Sprintf("\nmessage: %s", message)
	}

	doc := solr.CodeDocument{
		ID:        fmt.Sprintf("status:%s", repoID),
		Content:   content,
		Title:     fmt.Sprintf("Index status: %s", filepath.Base(repoPath)),
		Tags:      []string{"status", state, filepath.Base(repoPath)},
		Format:    "yaml",
		RepoURL:   repoPath,
		RepoID:    repoID,
		DocLevel:  "status",
		CommitSHA: commitSHA,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := idx.solr.AddCode(ctx, doc); err != nil {
		log.Printf("Warning: failed to write status for %s: %v", repoPath, err)
	}
}

func (idx *Indexer) fullIndex(ctx context.Context, repoPath, repoID string, repo RepoConfig, commitSHA string) error {
	log.Printf("Full index of %s at %s", repoPath, commitSHA[:8])
	idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "scanning", "", 0, 0)

	now := time.Now().UTC()

	// Collect all source files
	var files []string
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if shouldSkipPath(path, repoPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && parser.IsSourceFile(path) {
			relPath, _ := filepath.Rel(repoPath, path)
			if !shouldSkipFile(relPath) {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk repo: %w", err)
	}

	log.Printf("Found %d source files", len(files))
	idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "indexing", "", 0, len(files))

	// Track packages for package-level docs
	packages := make(map[string]*packageInfo)

	// Cross-reference index for pass 2
	crossRef := NewCrossRefIndex()

	// Process files in batches
	var allDocs []solr.CodeDocument
	filesProcessed := 0
	for _, filePath := range files {
		relPath, _ := filepath.Rel(repoPath, filePath)

		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: cannot read %s: %v", relPath, err)
			continue
		}

		// Skip very large files
		if len(content) > 1024*1024 { // 1MB
			log.Printf("Skipping large file: %s (%d bytes)", relPath, len(content))
			continue
		}

		p := idx.registry.ForFile(filePath)
		fileInfo, err := p.Parse(filePath, content)
		if err != nil {
			log.Printf("Warning: parse error for %s: %v", relPath, err)
			// Still create a file-level doc without symbols
			fileInfo = &parser.FileInfo{}
		}

		lang := parser.Language(filePath)
		fileID := codeDocID("file", repoID, relPath)
		pkgDir := filepath.Dir(relPath)
		vendor := isVendorPath(relPath)

		// Track package info
		pkg, ok := packages[pkgDir]
		if !ok {
			pkg = &packageInfo{
				dir:      pkgDir,
				files:    []string{},
				isVendor: vendor,
			}
			packages[pkgDir] = pkg
		}
		pkg.files = append(pkg.files, filepath.Base(relPath))
		if fileInfo.PackageName != "" {
			pkg.name = fileInfo.PackageName
		}
		for _, h := range fileInfo.InterfaceHints {
			pkg.interfaceHints = append(pkg.interfaceHints, interfaceHintInfo{
				typeName:  h.Type,
				ifaceName: h.Interface,
			})
		}

		// Generate file-level document
		fileLevelDoc := idx.buildFileDoc(fileID, repoPath, repoID, relPath, lang, commitSHA, fileInfo, vendor, now)
		allDocs = append(allDocs, fileLevelDoc)

		// Register interface hints
		crossRef.RegisterInterfaceHints(fileInfo.InterfaceHints)

		// Generate symbol-level documents
		for _, sym := range fileInfo.Symbols {
			symDoc := idx.buildSymbolDoc(repoPath, repoID, fileID, relPath, lang, commitSHA, sym, fileInfo.PackageName, vendor, now)
			allDocs = append(allDocs, symDoc)

			// Register in cross-reference index
			crossRef.RegisterSymbol(symDoc.ID, symDoc.QualifiedName, sym)

			// Track exported symbols for package doc
			if isExported(sym.Name) {
				pkg.symbols = append(pkg.symbols, sym)
			}
		}

		filesProcessed++

		// Batch index every 500 docs
		if len(allDocs) >= 500 {
			if err := idx.solr.AddCode(ctx, allDocs...); err != nil {
				return fmt.Errorf("batch index: %w", err)
			}
			log.Printf("Indexed %d documents (%d/%d files)...", len(allDocs), filesProcessed, len(files))
			idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "indexing", "", filesProcessed, len(files))
			allDocs = allDocs[:0]
		}
	}

	// Generate package-level documents
	for pkgDir, pkg := range packages {
		pkgDoc := idx.buildPackageDoc(repoPath, repoID, pkgDir, commitSHA, pkg, now)
		allDocs = append(allDocs, pkgDoc)
	}

	// Generate repo-level document
	repoDoc := idx.buildRepoDoc(repoPath, repoID, commitSHA, packages, files, now)
	allDocs = append(allDocs, repoDoc)

	// Index remaining docs
	if len(allDocs) > 0 {
		if err := idx.solr.AddCode(ctx, allDocs...); err != nil {
			return fmt.Errorf("final batch index: %w", err)
		}
	}

	log.Printf("Pass 1 complete: indexed %d files across %d packages", len(files), len(packages))
	idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "cross-referencing", "", filesProcessed, len(files))

	// Pass 2: Cross-reference resolution
	log.Printf("Starting pass 2: cross-referencing (%s)", crossRef.Stats())
	crossRef.DetectTestFiles(files, repoPath)
	crossRef.Resolve()
	log.Printf("Cross-references resolved (%s)", crossRef.Stats())

	if err := crossRef.ApplyUpdates(ctx, idx.solr); err != nil {
		log.Printf("Warning: cross-reference updates had errors: %v", err)
	}

	// Pass 3: Frontend selector analysis (opt-in)
	if repo.HasFeature("frontend-selectors") {
		log.Printf("Running frontend selector analysis for %s", repoPath)
		fa := NewFrontendAnalyzer(idx.solr)
		if err := fa.AnalyzeRepo(ctx, repoPath, repoID, commitSHA); err != nil {
			log.Printf("Warning: frontend selector analysis failed: %v", err)
		}
	}

	log.Printf("Full index complete: %d files, %d packages", len(files), len(packages))
	idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "complete", fmt.Sprintf("%d files, %d packages", len(files), len(packages)), len(files), len(files))
	return nil
}

func (idx *Indexer) incrementalIndex(ctx context.Context, repoPath, repoID string, repo RepoConfig, lastSHA, commitSHA string) error {
	log.Printf("Incremental index of %s: %s..%s", repoPath, lastSHA[:8], commitSHA[:8])

	// Get changed files
	changed, err := gitDiffFiles(repoPath, lastSHA, commitSHA)
	if err != nil {
		log.Printf("Warning: git diff failed, falling back to full index: %v", err)
		return idx.fullIndex(ctx, repoPath, repoID, repo, commitSHA)
	}

	if len(changed) == 0 {
		log.Printf("No file changes detected")
		return nil
	}

	log.Printf("Processing %d changed files", len(changed))
	now := time.Now().UTC()

	affectedPackages := make(map[string]bool)

	for _, change := range changed {
		relPath := change.path
		pkgDir := filepath.Dir(relPath)
		affectedPackages[pkgDir] = true

		switch change.status {
		case "D":
			// Delete file and its symbols
			if err := idx.solr.DeleteByQuery(ctx, fmt.Sprintf("repo_id:%q AND file_path:%q", repoID, relPath)); err != nil {
				log.Printf("Warning: failed to delete docs for %s: %v", relPath, err)
			}

		case "A", "M":
			// Delete existing docs for this file first (handles removed symbols)
			if err := idx.solr.DeleteByQuery(ctx, fmt.Sprintf("repo_id:%q AND file_path:%q", repoID, relPath)); err != nil {
				log.Printf("Warning: failed to clean docs for %s: %v", relPath, err)
			}

			absPath := filepath.Join(repoPath, relPath)
			if !parser.IsSourceFile(absPath) || shouldSkipFile(relPath) {
				continue
			}

			content, err := os.ReadFile(absPath)
			if err != nil {
				log.Printf("Warning: cannot read %s: %v", relPath, err)
				continue
			}

			if len(content) > 1024*1024 {
				continue
			}

			p := idx.registry.ForFile(absPath)
			fileInfo, err := p.Parse(absPath, content)
			if err != nil {
				log.Printf("Warning: parse error for %s: %v", relPath, err)
				fileInfo = &parser.FileInfo{}
			}

			lang := parser.Language(absPath)
			fileID := codeDocID("file", repoID, relPath)
			vendor := isVendorPath(relPath)

			var docs []solr.CodeDocument
			fileLevelDoc := idx.buildFileDoc(fileID, repoPath, repoID, relPath, lang, commitSHA, fileInfo, vendor, now)
			docs = append(docs, fileLevelDoc)

			for _, sym := range fileInfo.Symbols {
				symDoc := idx.buildSymbolDoc(repoPath, repoID, fileID, relPath, lang, commitSHA, sym, fileInfo.PackageName, vendor, now)
				docs = append(docs, symDoc)
			}

			if err := idx.solr.AddCode(ctx, docs...); err != nil {
				log.Printf("Warning: failed to index %s: %v", relPath, err)
			}
		}
	}

	// Update repo doc with new commit SHA
	repoDocID := codeDocID("repo", repoID, "")
	if err := idx.solr.Update(ctx, repoDocID, map[string]any{
		"commit_sha": commitSHA,
		"updated_at": now.Format(time.RFC3339),
	}); err != nil {
		log.Printf("Warning: failed to update repo doc: %v", err)
	}

	// Frontend selector analysis for changed files (opt-in)
	if repo.HasFeature("frontend-selectors") {
		var changedPaths []string
		for _, change := range changed {
			changedPaths = append(changedPaths, change.path)
		}
		if len(changedPaths) > 0 {
			fa := NewFrontendAnalyzer(idx.solr)
			if err := fa.AnalyzeChangedFiles(ctx, repoPath, repoID, commitSHA, changedPaths); err != nil {
				log.Printf("Warning: frontend selector analysis failed: %v", err)
			}
		}
	}

	log.Printf("Incremental index complete: %d files changed, %d packages affected", len(changed), len(affectedPackages))
	idx.writeStatus(ctx, repoID, repo.Path, commitSHA, "complete", fmt.Sprintf("%d files changed, %d packages affected", len(changed), len(affectedPackages)), len(changed), len(changed))
	return nil
}

// Document builders

func (idx *Indexer) buildRepoDoc(repoPath, repoID, commitSHA string, packages map[string]*packageInfo, files []string, now time.Time) solr.CodeDocument {
	content := buildRepoYAML(repoPath, commitSHA, idx.cfg.DefaultBranch, packages, files)

	// Collect languages for tags
	langSet := make(map[string]bool)
	for _, f := range files {
		langSet[parser.Language(f)] = true
	}
	var languages []string
	for lang := range langSet {
		languages = append(languages, lang)
	}

	return solr.CodeDocument{
		ID:         codeDocID("repo", repoID, ""),
		Content:    content,
		Title:      fmt.Sprintf("Repository: %s", filepath.Base(repoPath)),
		Tags:       append(languages, "repository", filepath.Base(repoPath)),
		Format:     "yaml",
		RepoURL:    repoPath,
		RepoID:     repoID,
		DocLevel:   "repo",
		CommitSHA:  commitSHA,
		Importance: 1.0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (idx *Indexer) buildPackageDoc(repoPath, repoID, pkgDir, commitSHA string, pkg *packageInfo, now time.Time) solr.CodeDocument {
	content := buildPackageYAML(pkg, pkgDir)

	pkgName := pkg.name
	if pkgName == "" {
		pkgName = filepath.Base(pkgDir)
	}

	repoDocID := codeDocID("repo", repoID, "")

	tags := []string{"package", pkgName}
	importance := 0.8
	if pkg.isVendor {
		tags = append(tags, "vendor")
		importance = 0.4
	}

	return solr.CodeDocument{
		ID:          codeDocID("pkg", repoID, pkgDir),
		Content:     content,
		Title:       fmt.Sprintf("Package: %s", pkgName),
		Tags:        tags,
		Format:      "yaml",
		RepoURL:     repoPath,
		RepoID:      repoID,
		DocLevel:    "package",
		ParentID:    repoDocID,
		FilePath:    pkgDir,
		PackageName: pkgName,
		CommitSHA:   commitSHA,
		Importance:  importance,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (idx *Indexer) buildFileDoc(fileID, repoPath, repoID, relPath, lang, commitSHA string, info *parser.FileInfo, vendor bool, now time.Time) solr.CodeDocument {
	pkgDir := filepath.Dir(relPath)
	pkgDocID := codeDocID("pkg", repoID, pkgDir)

	content := buildFileYAML(relPath, lang, info)

	tags := []string{"file", filepath.Base(relPath), lang}
	if info.PackageName != "" {
		tags = append(tags, info.PackageName)
	}

	importance := 0.6
	if vendor {
		tags = append(tags, "vendor")
		importance = 0.3
	}

	return solr.CodeDocument{
		ID:          fileID,
		Content:     content,
		Title:       fmt.Sprintf("File: %s", relPath),
		Tags:        tags,
		Format:      "yaml",
		RepoURL:     repoPath,
		RepoID:      repoID,
		DocLevel:    "file",
		ParentID:    pkgDocID,
		FilePath:    relPath,
		Language:    lang,
		PackageName: info.PackageName,
		CommitSHA:   commitSHA,
		Importance:  importance,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (idx *Indexer) buildSymbolDoc(repoPath, repoID, fileID, relPath, lang, commitSHA string, sym parser.Symbol, pkgName string, vendor bool, now time.Time) solr.CodeDocument {
	symID := codeDocID("sym", repoID, relPath+":"+sym.Name)

	// Pre-analyzed YAML content
	content := buildSymbolYAML(sym, relPath, pkgName)

	// Build qualified name for cross-referencing
	qualName := sym.Name
	if sym.Receiver != "" {
		qualName = strings.TrimPrefix(sym.Receiver, "*") + "." + sym.Name
	}
	if pkgName != "" {
		qualName = pkgName + "." + qualName
	}

	tags := []string{sym.Name, sym.Type, lang}
	if sym.Receiver != "" {
		tags = append(tags, strings.TrimPrefix(sym.Receiver, "*"))
	}

	importance := 0.5
	if vendor {
		tags = append(tags, "vendor")
		importance = 0.2
	}

	return solr.CodeDocument{
		ID:              symID,
		Content:         content,
		Title:           sym.Signature,
		Tags:            tags,
		Format:          "yaml",
		SourceCode:      sym.Body,
		RepoURL:         repoPath,
		RepoID:          repoID,
		DocLevel:        "symbol",
		ParentID:        fileID,
		FilePath:        relPath,
		Language:        lang,
		SymbolName:      sym.Name,
		SymbolNameExact: sym.Name,
		SymbolType:      sym.Type,
		PackageName:     pkgName,
		LineStart:       sym.LineStart,
		LineEnd:         sym.LineEnd,
		CommitSHA:       commitSHA,
		Importance:      importance,
		QualifiedName:   qualName,
		ReceiverType:    sym.Receiver,
		Calls:           sym.Calls,
		TypesUsed:       sym.TypesUsed,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// Helpers

type packageInfo struct {
	dir            string
	name           string
	files          []string
	symbols        []parser.Symbol
	isVendor       bool
	interfaceHints []interfaceHintInfo
}

type interfaceHintInfo struct {
	typeName  string
	ifaceName string
}

type fileChange struct {
	status string // A, M, D
	path   string
}

func (idx *Indexer) resolveRepo(repo RepoConfig) (string, error) {
	// If no clone dir is configured, use the repo path directly (one-shot only).
	// This is only safe when the indexer doesn't need to git reset.
	if idx.cfg.CloneDir == "" {
		if info, err := os.Stat(repo.Path); err == nil && info.IsDir() {
			// .git can be a directory (normal repo) or a file (worktree)
			gitDir := filepath.Join(repo.Path, ".git")
			if _, err := os.Stat(gitDir); err == nil {
				return repo.Path, nil
			}
		}
		return "", fmt.Errorf("no CLONE_DIR configured and %s is not a git repo", repo.Path)
	}

	// Always use a managed clone in CloneDir.
	// The repo path is treated as a source (local path or remote URL) to clone from.
	cloneDir := idx.cfg.CloneDir
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		return "", fmt.Errorf("create clone dir: %w", err)
	}

	repoDir := filepath.Join(cloneDir, sanitizeRepoName(repo.Path))
	branch := repo.Branch
	if branch == "" {
		branch = idx.cfg.DefaultBranch
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// Already cloned — fetch and reset to tracked branch
		log.Printf("Updating managed clone: %s (branch: %s)", repoDir, branch)
		if err := gitFetchAndReset(repoDir, branch); err != nil {
			return "", fmt.Errorf("update clone: %w", err)
		}
		return repoDir, nil
	}

	// Fresh clone from the source
	log.Printf("Cloning %s into %s (branch: %s)", repo.Path, repoDir, branch)
	cmd := exec.Command("git", "clone", "--branch", branch, "--single-branch", repo.Path, repoDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git clone %s: %w", repo.Path, err)
	}

	return repoDir, nil
}

func gitFetchAndReset(repoDir, branch string) error {
	// Fetch latest from origin
	cmd := exec.Command("git", "-C", repoDir, "fetch", "origin", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch: %s: %w", out, err)
	}

	// Reset to origin's branch tip — this is an indexer-owned clone, no local work to lose
	cmd = exec.Command("git", "-C", repoDir, "reset", "--hard", "origin/"+branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reset: %s: %w", out, err)
	}

	return nil
}

func sanitizeRepoName(repoURL string) string {
	// Turn "https://github.com/org/repo.git" or "git@github.com:org/repo" into "org_repo"
	name := repoURL
	name = strings.TrimSuffix(name, ".git")
	// Handle SSH URLs
	if idx := strings.LastIndex(name, ":"); idx > 0 && !strings.Contains(name[:idx], "/") {
		name = name[idx+1:]
	}
	// Handle HTTPS URLs
	for _, prefix := range []string{"https://", "http://"} {
		name = strings.TrimPrefix(name, prefix)
	}
	// Take last two path segments (org/repo)
	parts := strings.Split(name, "/")
	if len(parts) >= 2 {
		name = parts[len(parts)-2] + "_" + parts[len(parts)-1]
	} else if len(parts) == 1 {
		name = parts[0]
	}
	// Sanitize for filesystem
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	return name
}

func (idx *Indexer) getLastIndexedSHA(ctx context.Context, repoID string) (string, error) {
	resp, err := idx.solr.Query(ctx, solr.QueryParams{
		Query:         "*:*",
		FilterQueries: []string{fmt.Sprintf("repo_id:%q", repoID), `doc_level:"repo"`},
		Fields:        []string{"commit_sha"},
		Rows:          1,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Docs) == 0 {
		return "", nil
	}
	sha, _ := resp.Docs[0]["commit_sha"].(string)
	return sha, nil
}

func gitHeadSHA(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDiffFiles(repoPath, fromSHA, toSHA string) ([]fileChange, error) {
	cmd := exec.Command("git", "-C", repoPath, "diff", "--name-status", fromSHA+".."+toSHA)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	var changes []fileChange
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0][:1] // A, M, D, R (take first char for renames)
		path := parts[len(parts)-1]
		changes = append(changes, fileChange{status: status, path: path})
	}

	return changes, nil
}

func codeDocID(level, repoID, path string) string {
	if path == "" {
		return fmt.Sprintf("%s:%s", level, repoID)
	}
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%s:%s:%x", level, repoID, h[:6])
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:6])
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func shouldSkipPath(path, repoRoot string) bool {
	rel, _ := filepath.Rel(repoRoot, path)
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		switch part {
		// VCS and IDE
		case ".git", ".idea", ".vscode", ".cursor", ".rulesync",
			// Dependencies (node_modules skipped; vendor/local_vendor indexed with tags)
			"node_modules",
			// Build output
			"dist", "build", ".next", ".nuxt", "target", "bin",
			// Python caches
			"__pycache__", ".mypy_cache", ".pytest_cache",
			// Test fixtures
			"testdata", "test_fixtures",
			// Database migrations (generated SQL, not useful for code context)
			"migrations",
			// Mock directories
			"mocks":
			return true
		}
		if strings.HasPrefix(part, ".") && part != "." {
			return true
		}
	}
	return false
}

// isVendorPath returns true if the path is under vendor/ or local_vendor/.
func isVendorPath(relPath string) bool {
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if part == "vendor" || part == "local_vendor" {
			return true
		}
	}
	return false
}

// shouldSkipFile returns true for generated or non-useful files based on filename patterns.
func shouldSkipFile(relPath string) bool {
	base := filepath.Base(relPath)

	// Generated protobuf code
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, ".pb.gw.go") || strings.HasSuffix(base, ".pb.pgdb.go") {
		return true
	}
	// Generated code patterns
	if strings.HasSuffix(base, "_generated.go") || strings.HasSuffix(base, ".gen.go") || strings.HasSuffix(base, "_gen.go") {
		return true
	}
	// Mock files
	if strings.HasPrefix(base, "mock_") || strings.HasSuffix(base, "_mock.go") || strings.HasSuffix(base, "_mock_test.go") {
		return true
	}
	// Compressed/binary data checked in
	if strings.HasSuffix(base, ".zst") || strings.HasSuffix(base, ".gz") || strings.HasSuffix(base, ".tar") || strings.HasSuffix(base, ".zip") {
		return true
	}
	// Lock files and checksums
	if base == "go.sum" || base == "package-lock.json" || base == "yarn.lock" || base == "pnpm-lock.yaml" {
		return true
	}
	// Minified/bundled JS
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".bundle.js") || strings.HasSuffix(base, ".chunk.js") {
		return true
	}

	return false
}
