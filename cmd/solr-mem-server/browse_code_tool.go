package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func browseCodeTool(ctx context.Context, args map[string]any) (any, error) {
	params := solr.QueryParams{
		Query:     "*:*",
		Rows:      getInt(args, "limit", 50),
		Highlight: false,
		Sort:      "doc_level asc, importance desc",
	}

	// Build filters based on what's provided
	hasFilter := false

	if v := getString(args, "parent_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("parent_id:%q", v))
		hasFilter = true
	}

	if fq := repoFilterQuery(getString(args, "repo_url")); fq != "" {
		params.FilterQueries = append(params.FilterQueries, fq)
		hasFilter = true
	}

	if v := getString(args, "file_path"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("file_path:%q", v))
		hasFilter = true
	}

	if v := getString(args, "doc_level"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("doc_level:%q", v))
		hasFilter = true
	}

	// If no filters, show all repos
	if !hasFilter {
		params.FilterQueries = append(params.FilterQueries, `doc_level:"repo"`)
	}

	resp, err := codeClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("browse failed: %w", err)
	}

	fr := repoFreshnessFromDocs(ctx, resp.Docs)
	return ToolOutput{
		Text:       formatBrowseResults(resp) + freshnessText(fr),
		Structured: codeEnvelope{QueryResponse: resp, RepoIndex: fr},
	}, nil
}

func formatBrowseResults(resp *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d documents.\n\n", resp.NumFound))

	for _, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		docLevel, _ := doc["doc_level"].(string)
		filePath, _ := doc["file_path"].(string)
		symbolType, _ := doc["symbol_type"].(string)
		language, _ := doc["language"].(string)

		// Compact format for browsing
		switch docLevel {
		case "repo":
			sb.WriteString(fmt.Sprintf("[repo] %s  (id: %s)\n", title, id))
		case "package":
			sb.WriteString(fmt.Sprintf("  [pkg] %s  path=%s  (id: %s)\n", title, filePath, id))
		case "file":
			sb.WriteString(fmt.Sprintf("    [file] %s  lang=%s  (id: %s)\n", filePath, language, id))
		case "symbol":
			lineStart, _ := doc["line_start"].(float64)
			sb.WriteString(fmt.Sprintf("      [%s] %s  L%d  (id: %s)\n", symbolType, title, int(lineStart), id))
		default:
			sb.WriteString(fmt.Sprintf("[%s] %s  (id: %s)\n", docLevel, title, id))
		}
	}

	return sb.String()
}
