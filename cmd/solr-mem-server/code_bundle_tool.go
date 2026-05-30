package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func codeContextBundleTool(ctx context.Context, args map[string]any) (any, error) {
	symbolName := getString(args, "symbol_name")
	if symbolName == "" {
		return nil, fmt.Errorf("symbol_name is required")
	}

	includeSource := getBool(args, "include_source", true)
	depth := getInt(args, "depth", 1)
	if depth > 2 {
		depth = 2
	}

	// Step 1: Find the target symbol
	params := solr.QueryParams{
		Query:     fmt.Sprintf("symbol_name_exact:%q", symbolName),
		Rows:      5,
		Highlight: false,
	}
	params.FilterQueries = append(params.FilterQueries, `doc_level:"symbol"`)
	if fq := repoFilterQuery(getString(args, "repo_url")); fq != "" {
		params.FilterQueries = append(params.FilterQueries, fq)
	}

	resp, err := codeClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	if resp.NumFound == 0 {
		return fmt.Sprintf("No symbol found with name %q", symbolName), nil
	}

	// Use the first result
	targetDoc := resp.Docs[0]

	var sb strings.Builder
	var allDocs []map[string]any

	// === Target Symbol ===
	sb.WriteString("=== Target Symbol ===\n")
	content, _ := targetDoc["content"].(string)
	sb.WriteString(content)
	sb.WriteString("\n")
	allDocs = append(allDocs, targetDoc)

	// === Source Code ===
	if includeSource {
		sourceCode, _ := targetDoc["source_code"].(string)
		if sourceCode != "" {
			sb.WriteString("\n=== Source Code ===\n")
			sb.WriteString(sourceCode)
			sb.WriteString("\n")
		}
	}

	// === Callees ===
	calls := getStringSliceFromDoc(targetDoc, "calls")
	if len(calls) > 0 && depth >= 1 {
		calleeDocs := fetchRelatedSymbols(ctx, calls, targetDoc)
		if len(calleeDocs) > 0 {
			sb.WriteString(fmt.Sprintf("\n=== Calls (%d resolved) ===\n", len(calleeDocs)))
			for _, doc := range calleeDocs {
				title, _ := doc["title"].(string)
				filePath, _ := doc["file_path"].(string)
				lineStart, _ := doc["line_start"].(float64)
				sb.WriteString(fmt.Sprintf("  - %s  [%s:%d]\n", title, filePath, int(lineStart)))
				allDocs = append(allDocs, doc)
			}
		}
	}

	// === Callers ===
	calledBy := getStringSliceFromDoc(targetDoc, "called_by")
	if len(calledBy) > 0 {
		sb.WriteString(fmt.Sprintf("\n=== Called By (%d) ===\n", len(calledBy)))
		// Fetch caller symbols
		callerDocs := fetchByQualifiedNames(ctx, calledBy)
		for _, doc := range callerDocs {
			title, _ := doc["title"].(string)
			filePath, _ := doc["file_path"].(string)
			lineStart, _ := doc["line_start"].(float64)
			sb.WriteString(fmt.Sprintf("  - %s  [%s:%d]\n", title, filePath, int(lineStart)))
			allDocs = append(allDocs, doc)
		}
		if len(callerDocs) == 0 {
			for _, c := range calledBy {
				sb.WriteString(fmt.Sprintf("  - %s\n", c))
			}
		}
	}

	// === Types Used ===
	typesUsed := getStringSliceFromDoc(targetDoc, "types_used")
	if len(typesUsed) > 0 {
		typeDocs := fetchTypeSymbols(ctx, typesUsed, targetDoc)
		if len(typeDocs) > 0 {
			sb.WriteString(fmt.Sprintf("\n=== Types Used (%d resolved) ===\n", len(typeDocs)))
			for _, doc := range typeDocs {
				title, _ := doc["title"].(string)
				filePath, _ := doc["file_path"].(string)
				symbolType, _ := doc["symbol_type"].(string)
				sb.WriteString(fmt.Sprintf("  - [%s] %s  [%s]\n", symbolType, title, filePath))
				allDocs = append(allDocs, doc)
			}
		}
	}

	// === Package Context ===
	if depth >= 1 {
		parentID, _ := targetDoc["parent_id"].(string)
		if parentID != "" {
			// Get file doc
			fileResp, err := codeClient.Query(ctx, solr.QueryParams{
				Query: fmt.Sprintf("id:%q", parentID),
				Rows:  1,
			})
			if err == nil && len(fileResp.Docs) > 0 {
				fileDoc := fileResp.Docs[0]
				fileParentID, _ := fileDoc["parent_id"].(string)
				// Get package doc
				if fileParentID != "" {
					pkgResp, err := codeClient.Query(ctx, solr.QueryParams{
						Query: fmt.Sprintf("id:%q", fileParentID),
						Rows:  1,
					})
					if err == nil && len(pkgResp.Docs) > 0 {
						pkgDoc := pkgResp.Docs[0]
						pkgTitle, _ := pkgDoc["title"].(string)
						pkgContent, _ := pkgDoc["content"].(string)
						sb.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", pkgTitle, pkgContent))
						allDocs = append(allDocs, pkgDoc)
					}
				}
			}
		}
	}

	fr := repoFreshnessFromDocs(ctx, allDocs)
	sb.WriteString(freshnessText(fr))
	return ToolOutput{
		Text: sb.String(),
		Structured: map[string]any{
			"documents":  allDocs,
			"repo_index": fr,
		},
	}, nil
}

// fetchRelatedSymbols looks up call targets by name within the same repo.
func fetchRelatedSymbols(ctx context.Context, calls []string, targetDoc map[string]any) []map[string]any {
	if len(calls) == 0 {
		return nil
	}

	repoID, _ := targetDoc["repo_id"].(string)

	// Build a query for the call target names
	var names []string
	for _, call := range calls {
		// Extract the method/function name (last segment after dot)
		parts := strings.Split(call, ".")
		name := parts[len(parts)-1]
		names = append(names, fmt.Sprintf("%q", name))
	}
	if len(names) > 20 {
		names = names[:20]
	}

	query := fmt.Sprintf("symbol_name_exact:(%s)", strings.Join(names, " OR "))
	fqs := []string{`doc_level:"symbol"`, `-tags:"vendor"`}
	if repoID != "" {
		fqs = append(fqs, fmt.Sprintf("repo_id:%q", repoID))
	}

	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:         query,
		FilterQueries: fqs,
		Rows:          30,
		Highlight:     false,
	})
	if err != nil || resp.NumFound == 0 {
		return nil
	}
	return resp.Docs
}

// fetchByQualifiedNames looks up symbols by their qualified names.
func fetchByQualifiedNames(ctx context.Context, qualNames []string) []map[string]any {
	if len(qualNames) == 0 {
		return nil
	}

	var quoted []string
	for _, qn := range qualNames {
		quoted = append(quoted, fmt.Sprintf("%q", qn))
	}
	if len(quoted) > 20 {
		quoted = quoted[:20]
	}

	query := fmt.Sprintf("qualified_name:(%s)", strings.Join(quoted, " OR "))

	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:     query,
		Rows:      20,
		Highlight: false,
	})
	if err != nil || resp.NumFound == 0 {
		return nil
	}
	return resp.Docs
}

// fetchTypeSymbols looks up type definitions by name.
func fetchTypeSymbols(ctx context.Context, typeNames []string, targetDoc map[string]any) []map[string]any {
	if len(typeNames) == 0 {
		return nil
	}

	repoID, _ := targetDoc["repo_id"].(string)

	var names []string
	for _, t := range typeNames {
		// Extract type name (handle qualified like "pkg.Type")
		parts := strings.Split(t, ".")
		name := parts[len(parts)-1]
		names = append(names, fmt.Sprintf("%q", name))
	}
	if len(names) > 15 {
		names = names[:15]
	}

	query := fmt.Sprintf("symbol_name_exact:(%s)", strings.Join(names, " OR "))
	fqs := []string{`doc_level:"symbol"`, `symbol_type:("struct" OR "interface" OR "type")`}
	if repoID != "" {
		fqs = append(fqs, fmt.Sprintf("repo_id:%q", repoID))
	}

	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:         query,
		FilterQueries: fqs,
		Rows:          20,
		Highlight:     false,
	})
	if err != nil || resp.NumFound == 0 {
		return nil
	}
	return resp.Docs
}

// getStringSliceFromDoc extracts a string slice from a Solr doc (handles []any).
func getStringSliceFromDoc(doc map[string]any, key string) []string {
	val, ok := doc[key]
	if !ok || val == nil {
		return nil
	}
	switch v := val.(type) {
	case []string:
		return v
	case []any:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		return []string{v}
	}
	return nil
}
