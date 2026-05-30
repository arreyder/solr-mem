package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func searchCodeTool(ctx context.Context, args map[string]any) (any, error) {
	query := getString(args, "query")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	params := solr.QueryParams{
		Query:     query,
		Rows:      getInt(args, "limit", 10),
		Highlight: getBool(args, "highlight", true),
		Facet:     getBool(args, "facet", false),
	}

	if fq := repoFilterQuery(getString(args, "repo_url")); fq != "" {
		params.FilterQueries = append(params.FilterQueries, fq)
	}
	if v := getString(args, "language"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("language:%q", v))
	}
	if v := getString(args, "doc_level"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("doc_level:%q", v))
	}
	if v := getString(args, "symbol_type"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("symbol_type:%q", v))
	}
	if v := getString(args, "file_path"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("file_path:%q", v))
	}

	if params.Facet {
		params.FacetFields = []string{"language", "doc_level", "symbol_type", "repo_id", "package_name"}
	}

	resp, err := codeClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	fr := repoFreshnessFromDocs(ctx, resp.Docs)
	return ToolOutput{
		Text:       formatCodeResults(resp) + freshnessText(fr),
		Structured: codeEnvelope{QueryResponse: resp, RepoIndex: fr},
	}, nil
}

func formatCodeResults(resp *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d code documents.\n\n", resp.NumFound))

	for i, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		docLevel, _ := doc["doc_level"].(string)
		filePath, _ := doc["file_path"].(string)
		symbolType, _ := doc["symbol_type"].(string)
		language, _ := doc["language"].(string)
		content, _ := doc["content"].(string)

		sb.WriteString(fmt.Sprintf("--- Result %d ---\n", i+1))
		sb.WriteString(fmt.Sprintf("ID: %s\n", id))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", title))
		}
		sb.WriteString(fmt.Sprintf("Level: %s\n", docLevel))
		if filePath != "" {
			sb.WriteString(fmt.Sprintf("File: %s\n", filePath))
		}
		if symbolType != "" {
			sb.WriteString(fmt.Sprintf("Symbol Type: %s\n", symbolType))
		}
		if language != "" {
			sb.WriteString(fmt.Sprintf("Language: %s\n", language))
		}

		// Show highlights if available, otherwise show content
		if hl, ok := resp.Highlighting[id]; ok {
			for field, snippets := range hl {
				sb.WriteString(fmt.Sprintf("%s: %s\n", field, strings.Join(snippets, " ... ")))
			}
		} else if content != "" {
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			sb.WriteString(fmt.Sprintf("Content:\n%s\n", content))
		}

		if tags, ok := doc["tags"]; ok {
			tagsJSON, _ := json.Marshal(tags)
			sb.WriteString(fmt.Sprintf("Tags: %s\n", tagsJSON))
		}

		sb.WriteString("\n")
	}

	if len(resp.Facets) > 0 {
		sb.WriteString("--- Facets ---\n")
		for field, counts := range resp.Facets {
			sb.WriteString(fmt.Sprintf("%s: ", field))
			parts := make([]string, 0, len(counts))
			for _, fc := range counts {
				parts = append(parts, fmt.Sprintf("%s(%d)", fc.Value, fc.Count))
			}
			sb.WriteString(strings.Join(parts, ", "))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
