package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func getSymbolTool(ctx context.Context, args map[string]any) (any, error) {
	symbolName := getString(args, "symbol_name")
	if symbolName == "" {
		return nil, fmt.Errorf("symbol_name is required")
	}

	// Search for the symbol by exact name
	params := solr.QueryParams{
		Query:     fmt.Sprintf("symbol_name_exact:%q", symbolName),
		Rows:      5,
		Highlight: false,
	}
	params.FilterQueries = append(params.FilterQueries, `doc_level:"symbol"`)

	if fq := repoFilterQuery(getString(args, "repo_url")); fq != "" {
		params.FilterQueries = append(params.FilterQueries, fq)
	}
	if v := getString(args, "language"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("language:%q", v))
	}

	resp, err := codeClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if resp.NumFound == 0 {
		return fmt.Sprintf("No symbol found with name %q", symbolName), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d symbol(s) named %q.\n\n", resp.NumFound, symbolName))

	for i, doc := range resp.Docs {
		title, _ := doc["title"].(string)
		content, _ := doc["content"].(string)
		filePath, _ := doc["file_path"].(string)
		symbolType, _ := doc["symbol_type"].(string)
		metadata, _ := doc["metadata"].(string)
		language, _ := doc["language"].(string)
		parentID, _ := doc["parent_id"].(string)

		sb.WriteString(fmt.Sprintf("--- Symbol %d ---\n", i+1))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Signature: %s\n", title))
		}
		sb.WriteString(fmt.Sprintf("Type: %s\n", symbolType))
		sb.WriteString(fmt.Sprintf("File: %s\n", filePath))
		sb.WriteString(fmt.Sprintf("Language: %s\n", language))
		if metadata != "" {
			sb.WriteString(fmt.Sprintf("Metadata: %s\n", metadata))
		}
		sb.WriteString(fmt.Sprintf("Source:\n%s\n", content))

		// Include related context if requested
		if getBool(args, "include_related", false) && parentID != "" {
			relatedCtx := getRelatedContext(ctx, parentID)
			if relatedCtx != "" {
				sb.WriteString(fmt.Sprintf("\n--- File Context ---\n%s\n", relatedCtx))
			}
		}

		sb.WriteString("\n")
	}

	fr := repoFreshnessFromDocs(ctx, resp.Docs)
	sb.WriteString(freshnessText(fr))
	return ToolOutput{
		Text:       sb.String(),
		Structured: codeEnvelope{QueryResponse: resp, RepoIndex: fr},
	}, nil
}

func getRelatedContext(ctx context.Context, parentID string) string {
	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:     fmt.Sprintf("id:%q", parentID),
		Rows:      1,
		Highlight: false,
	})
	if err != nil || len(resp.Docs) == 0 {
		return ""
	}

	doc := resp.Docs[0]
	title, _ := doc["title"].(string)
	content, _ := doc["content"].(string)

	if len(content) > 1000 {
		content = content[:1000] + "..."
	}

	return fmt.Sprintf("%s\n%s", title, content)
}
