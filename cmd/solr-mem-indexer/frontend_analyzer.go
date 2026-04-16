package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

// FrontendAnalyzer extracts testable UI selectors from TSX/TS/JSX files.
type FrontendAnalyzer struct {
	solr *solr.Client
}

// SelectorInfo describes a single extracted selector.
type SelectorInfo struct {
	Type    string // "data-testid", "aria-label", "form-input", "role", "href", "intl-message", "aria-haspopup", "tab-id"
	Value   string
	Line    int
	Dynamic bool   // true if value contains interpolation (e.g., ${...})
	Element string // optional: element type context (e.g., "button", "FormattedMessage")
}

// ComponentSelectors holds all selectors extracted from a single file.
type ComponentSelectors struct {
	FilePath      string
	ComponentName string
	Selectors     []SelectorInfo
}

func NewFrontendAnalyzer(client *solr.Client) *FrontendAnalyzer {
	return &FrontendAnalyzer{solr: client}
}

// Pre-compiled extraction patterns.
var (
	// Matches: data-testid="val", data-testid='val', data-testid={'val'}, data-testid={`val`}
	reDataTestID = regexp.MustCompile("data-testid\\s*=\\s*(?:\"([^\"]+)\"|'([^']+)'|\\{['\"]([^'\"]+)['\"]\\}|\\{`([^`]+)`\\})")
	reAriaLabel  = regexp.MustCompile("aria-label\\s*=\\s*(?:\"([^\"]+)\"|'([^']+)'|\\{['\"]([^'\"]+)['\"]\\}|\\{`([^`]+)`\\})")
	// Also match object/prop syntax: name: "val", name: 'val'
	reDataTestIDProp = regexp.MustCompile("[\"']?data-testid[\"']?\\s*:\\s*[\"']([^\"']+)[\"']")
	reAriaLabelProp  = regexp.MustCompile("[\"']?aria-label[\"']?\\s*:\\s*[\"']([^\"']+)[\"']")
	reNameAttr       = regexp.MustCompile("\\bname\\s*[=:]\\s*[\"']([^\"']+)[\"']")
	reRoleAttr       = regexp.MustCompile("\\brole\\s*[=:]\\s*[\"']([^\"']+)[\"']")
	reHrefAttr       = regexp.MustCompile("\\bhref\\s*[=:]\\s*[\"']([^\"']+)[\"']")
	reExportComp     = regexp.MustCompile(`^export\s+(?:default\s+)?(?:function|const|class)\s+(\w+)`)

	// Form elements that make a nearby name= attribute relevant.
	reFormElement = regexp.MustCompile(`(?i)<(?:input|select|textarea|Switch|Checkbox|Radio|C1Switch|C1Checkbox|C1Select|C1LabeledSwitch|C1LabeledSelect|C1LabeledRadioGroup)\b`)

	// Link elements that make a nearby href= attribute relevant.
	reLinkElement = regexp.MustCompile(`(?i)<(?:a|Link|NavLink|C1Link|C1LinkButton)\b`)

	// intl.formatMessage({...defaultMessage: "value"...}) — inline or assigned to variable.
	// Captures the defaultMessage value from intl.formatMessage calls.
	reIntlFormatMessage = regexp.MustCompile(`intl\.formatMessage\(\s*\{[^}]*defaultMessage:\s*['"]((?:[^'\\"]|\\.)*)['"]`)

	// <FormattedMessage ... defaultMessage="value" ... /> — JSX component (single-line).
	reFormattedMessage = regexp.MustCompile(`<FormattedMessage\b[^>]*defaultMessage\s*=\s*['"]((?:[^'\\"]|\\.)*)['"]`)

	// defaultMessage="value" on its own line (for multiline FormattedMessage).
	reDefaultMessageAttr = regexp.MustCompile(`^\s*defaultMessage\s*=\s*['"]((?:[^'\\"]|\\.)*)['"]`)

	// Detects a <FormattedMessage opening on a line (possibly multiline).
	reFormattedMessageOpen = regexp.MustCompile(`<FormattedMessage\b`)

	// aria-label={intl.formatMessage({...defaultMessage: "value"...})}
	reAriaLabelIntl = regexp.MustCompile(`aria-label\s*=\s*\{\s*intl\.formatMessage\(\s*\{[^}]*defaultMessage:\s*['"]((?:[^'\\"]|\\.)*)['"]`)

	// aria-haspopup="value" or aria-haspopup={'value'} or 'aria-haspopup': 'value'
	reAriaHasPopup     = regexp.MustCompile(`aria-haspopup\s*=\s*(?:"([^"]+)"|'([^']+)'|\{['"]((?:[^'\\"]|\\.)*)['"]\})`)
	reAriaHasPopupProp = regexp.MustCompile(`['"]\s*aria-haspopup['"]\s*:\s*['"]((?:[^'\\"]|\\.)*)['"]\s`)

	// Tab ID patterns: id={`prefix-tab-${key}`} or data-testid={`prefix-tab-${key}`}
	reTabID = regexp.MustCompile("(?:id|data-testid)\\s*=\\s*\\{\\s*`([^`]*-tab(?:panel)?-[^`]*)`\\s*\\}")

	// Element context: detect enclosing JSX element on same or nearby lines.
	reJSXElement = regexp.MustCompile(`<(\w+)\b`)
)

// isFrontendFile returns true for TSX/TS/JSX files.
func isFrontendFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".tsx" || ext == ".ts" || ext == ".jsx"
}

// AnalyzeRepo walks the repo and extracts selectors from all frontend files.
func (fa *FrontendAnalyzer) AnalyzeRepo(ctx context.Context, repoPath, repoID, commitSHA string) error {
	var files []string
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if shouldSkipPath(path, repoPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && isFrontendFile(path) && !shouldSkipFile(path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk repo for frontend files: %w", err)
	}

	log.Printf("Frontend selector analysis: found %d frontend files", len(files))

	return fa.analyzeFiles(ctx, repoPath, repoID, commitSHA, files)
}

// AnalyzeChangedFiles re-extracts selectors for a list of changed file paths.
// Paths should be relative to repoPath.
func (fa *FrontendAnalyzer) AnalyzeChangedFiles(ctx context.Context, repoPath, repoID, commitSHA string, relPaths []string) error {
	var absPaths []string
	for _, rel := range relPaths {
		abs := filepath.Join(repoPath, rel)
		if isFrontendFile(abs) {
			// Delete existing selector doc for this file.
			selID := codeDocID("sel", repoID, rel)
			if err := fa.solr.Delete(ctx, selID); err != nil {
				log.Printf("Warning: failed to delete old selector doc for %s: %v", rel, err)
			}
			// Only re-analyze if file still exists (not deleted).
			if _, err := os.Stat(abs); err == nil {
				absPaths = append(absPaths, abs)
			}
		}
	}

	if len(absPaths) == 0 {
		return nil
	}

	return fa.analyzeFiles(ctx, repoPath, repoID, commitSHA, absPaths)
}

func (fa *FrontendAnalyzer) analyzeFiles(ctx context.Context, repoPath, repoID, commitSHA string, absPaths []string) error {
	now := time.Now().UTC()
	var docs []solr.CodeDocument
	filesWithSelectors := 0

	for _, absPath := range absPaths {
		content, err := os.ReadFile(absPath)
		if err != nil {
			log.Printf("Warning: cannot read %s: %v", absPath, err)
			continue
		}
		if len(content) > 1024*1024 {
			continue
		}

		relPath, _ := filepath.Rel(repoPath, absPath)
		cs := extractSelectors(relPath, content)
		if len(cs.Selectors) == 0 {
			continue
		}

		filesWithSelectors++
		doc := buildSelectorDoc(repoPath, repoID, relPath, commitSHA, cs, now)
		docs = append(docs, doc)

		if len(docs) >= 200 {
			if err := fa.solr.AddCode(ctx, docs...); err != nil {
				return fmt.Errorf("batch index selector docs: %w", err)
			}
			docs = docs[:0]
		}
	}

	if len(docs) > 0 {
		if err := fa.solr.AddCode(ctx, docs...); err != nil {
			return fmt.Errorf("final batch index selector docs: %w", err)
		}
	}

	log.Printf("Frontend selector analysis: %d files with selectors (of %d frontend files)", filesWithSelectors, len(absPaths))
	return nil
}

// extractSelectors scans file content for testable UI selectors.
func extractSelectors(relPath string, content []byte) *ComponentSelectors {
	lines := strings.Split(string(content), "\n")
	cs := &ComponentSelectors{FilePath: relPath}

	// Context window: track recent lines for form/link element proximity.
	const contextWindow = 5

	for i, line := range lines {
		lineNum := i + 1

		// Extract component name from exports.
		if cs.ComponentName == "" {
			if m := reExportComp.FindStringSubmatch(line); m != nil {
				cs.ComponentName = m[1]
			}
		}

		// data-testid - always relevant.
		for _, val := range matchAttr(reDataTestID, reDataTestIDProp, line) {
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "data-testid",
				Value:   val,
				Line:    lineNum,
				Dynamic: strings.Contains(val, "${"),
			})
		}

		// aria-label - always relevant.
		// First check for aria-label={intl.formatMessage({defaultMessage: "..."})}
		ariaLabelIntlSeen := make(map[string]bool)
		for _, m := range reAriaLabelIntl.FindAllStringSubmatch(line, -1) {
			val := m[1]
			ariaLabelIntlSeen[val] = true
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "aria-label",
				Value:   val,
				Line:    lineNum,
				Element: nearestElement(lines, i),
			})
		}
		// Then match literal aria-label values (skip if already captured via intl).
		for _, val := range matchAttr(reAriaLabel, reAriaLabelProp, line) {
			if !ariaLabelIntlSeen[val] {
				cs.Selectors = append(cs.Selectors, SelectorInfo{
					Type:    "aria-label",
					Value:   val,
					Line:    lineNum,
					Dynamic: strings.Contains(val, "${"),
				})
			}
		}

		// aria-haspopup - relevant for menu triggers.
		for _, val := range matchAttr(reAriaHasPopup, reAriaHasPopupProp, line) {
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "aria-haspopup",
				Value:   val,
				Line:    lineNum,
				Element: nearestElement(lines, i),
			})
		}

		// role - always relevant.
		for _, m := range reRoleAttr.FindAllStringSubmatch(line, -1) {
			val := m[1]
			// Skip generic structural roles.
			if val == "presentation" || val == "none" {
				continue
			}
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:  "role",
				Value: val,
				Line:  lineNum,
			})
		}

		// intl.formatMessage({defaultMessage: "..."}) — standalone calls (not already captured as aria-label).
		for _, m := range reIntlFormatMessage.FindAllStringSubmatch(line, -1) {
			val := m[1]
			if ariaLabelIntlSeen[val] {
				continue // already captured as aria-label
			}
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "intl-message",
				Value:   val,
				Line:    lineNum,
				Element: nearestElement(lines, i),
			})
		}

		// <FormattedMessage defaultMessage="..." /> — single-line case.
		for _, m := range reFormattedMessage.FindAllStringSubmatch(line, -1) {
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "intl-message",
				Value:   m[1],
				Line:    lineNum,
				Element: "FormattedMessage",
			})
		}

		// Multiline <FormattedMessage>: defaultMessage= on its own line,
		// with <FormattedMessage on a preceding line (within 4 lines).
		if reFormattedMessage.FindStringSubmatch(line) == nil {
			if m := reDefaultMessageAttr.FindStringSubmatch(line); m != nil {
				if hasNearbyPattern(lines, i, 4, reFormattedMessageOpen) {
					cs.Selectors = append(cs.Selectors, SelectorInfo{
						Type:    "intl-message",
						Value:   m[1],
						Line:    lineNum,
						Element: "FormattedMessage",
					})
				}
			}
		}

		// Tab ID patterns: id={`prefix-tab-key`} or data-testid={`prefix-tab-key`}
		for _, m := range reTabID.FindAllStringSubmatch(line, -1) {
			cs.Selectors = append(cs.Selectors, SelectorInfo{
				Type:    "tab-id",
				Value:   m[1],
				Line:    lineNum,
				Dynamic: strings.Contains(m[1], "${"),
			})
		}

		// name= - only relevant near form elements.
		if nameMatches := reNameAttr.FindAllStringSubmatch(line, -1); len(nameMatches) > 0 {
			if hasNearbyPattern(lines, i, contextWindow, reFormElement) {
				for _, m := range nameMatches {
					cs.Selectors = append(cs.Selectors, SelectorInfo{
						Type:  "form-input",
						Value: m[1],
						Line:  lineNum,
					})
				}
			}
		}

		// href= - only relevant near link elements.
		if hrefMatches := reHrefAttr.FindAllStringSubmatch(line, -1); len(hrefMatches) > 0 {
			if hasNearbyPattern(lines, i, contextWindow, reLinkElement) {
				for _, m := range hrefMatches {
					val := m[1]
					// Skip fragment-only and javascript: hrefs.
					if strings.HasPrefix(val, "#") || strings.HasPrefix(val, "javascript:") {
						continue
					}
					cs.Selectors = append(cs.Selectors, SelectorInfo{
						Type:    "href",
						Value:   val,
						Line:    lineNum,
						Dynamic: strings.Contains(val, "${"),
					})
				}
			}
		}
	}

	// If no component name found, derive from filename.
	if cs.ComponentName == "" {
		base := filepath.Base(relPath)
		cs.ComponentName = strings.TrimSuffix(base, filepath.Ext(base))
		if cs.ComponentName == "index" {
			cs.ComponentName = filepath.Base(filepath.Dir(relPath))
		}
	}

	return cs
}

// nearestElement looks backwards from lineIdx for the nearest JSX element opening tag.
func nearestElement(lines []string, lineIdx int) string {
	// Check current line and up to 3 lines back for a JSX element.
	start := lineIdx - 3
	if start < 0 {
		start = 0
	}
	for i := lineIdx; i >= start; i-- {
		if m := reJSXElement.FindAllStringSubmatch(lines[i], -1); len(m) > 0 {
			// Return the last element on the line (closest to the attribute).
			return m[len(m)-1][1]
		}
	}
	return ""
}

// matchAttr extracts attribute values using a JSX attribute regex and a prop syntax regex.
// The JSX regex has multiple capture groups (one per quote style), so we pick the first non-empty.
func matchAttr(jsxRe, propRe *regexp.Regexp, line string) []string {
	var vals []string
	seen := make(map[string]bool)

	for _, m := range jsxRe.FindAllStringSubmatch(line, -1) {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				if !seen[m[i]] {
					vals = append(vals, m[i])
					seen[m[i]] = true
				}
				break
			}
		}
	}

	if propRe != nil {
		for _, m := range propRe.FindAllStringSubmatch(line, -1) {
			if m[1] != "" && !seen[m[1]] {
				vals = append(vals, m[1])
				seen[m[1]] = true
			}
		}
	}

	return vals
}

// hasNearbyPattern checks if any line within a window around lineIdx matches the pattern.
func hasNearbyPattern(lines []string, lineIdx, window int, re *regexp.Regexp) bool {
	start := lineIdx - window
	if start < 0 {
		start = 0
	}
	end := lineIdx + window
	if end >= len(lines) {
		end = len(lines) - 1
	}
	for i := start; i <= end; i++ {
		if re.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// buildSelectorDoc creates a CodeDocument for a file's selectors.
func buildSelectorDoc(repoPath, repoID, relPath, commitSHA string, cs *ComponentSelectors, now time.Time) solr.CodeDocument {
	content := buildSelectorYAML(cs)
	fileID := codeDocID("file", repoID, relPath)

	tags := []string{"frontend", "selector", cs.ComponentName}
	// Add tags for each selector type found.
	typeSet := make(map[string]bool)
	for _, s := range cs.Selectors {
		typeSet[s.Type] = true
	}
	for t := range typeSet {
		tags = append(tags, t)
	}

	return solr.CodeDocument{
		ID:         codeDocID("sel", repoID, relPath),
		Content:    content,
		Title:      fmt.Sprintf("Selectors: %s (%s)", cs.ComponentName, relPath),
		Tags:       tags,
		Format:     "yaml",
		RepoURL:    repoPath,
		RepoID:     repoID,
		DocLevel:   "selector",
		ParentID:   fileID,
		FilePath:   relPath,
		Language:   "typescript",
		SymbolType: "frontend-selectors",
		CommitSHA:  commitSHA,
		Importance: 0.7,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// buildSelectorYAML produces structured YAML content for a selector document.
func buildSelectorYAML(cs *ComponentSelectors) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("component: %s\n", cs.ComponentName))
	sb.WriteString(fmt.Sprintf("file: %s\n", cs.FilePath))

	// Group selectors by type.
	byType := make(map[string][]SelectorInfo)
	for _, s := range cs.Selectors {
		byType[s.Type] = append(byType[s.Type], s)
	}

	// Stable ordering for reproducibility.
	typeOrder := []string{"data-testid", "aria-label", "aria-haspopup", "intl-message", "tab-id", "role", "form-input", "href"}

	sb.WriteString("selectors:\n")
	for _, t := range typeOrder {
		sels, ok := byType[t]
		if !ok {
			continue
		}
		sb.WriteString(fmt.Sprintf("  %s:\n", t))
		for _, s := range sels {
			sb.WriteString(fmt.Sprintf("    - value: %q\n", s.Value))
			sb.WriteString(fmt.Sprintf("      line: %d\n", s.Line))
			if s.Element != "" {
				sb.WriteString(fmt.Sprintf("      element: %s\n", s.Element))
			}
			if s.Dynamic {
				sb.WriteString("      dynamic: true\n")
			}
		}
	}

	// Summary counts.
	sb.WriteString("summary:\n")
	sb.WriteString(fmt.Sprintf("  total: %d\n", len(cs.Selectors)))
	// Sort type names for stable output.
	var types []string
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", t, len(byType[t])))
	}

	return sb.String()
}
