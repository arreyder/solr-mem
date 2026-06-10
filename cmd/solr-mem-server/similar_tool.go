package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func similarMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
	id := getString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	rows := getInt(args, "limit", 5)

	var filterQueries []string
	if v := getString(args, "agent_id"); v != "" {
		filterQueries = append(filterQueries, fmt.Sprintf("agent_id:%q", v))
	}

	// Optional field projection for lean payloads; always keep id.
	var fields []string
	if f := getStringSlice(args, "fields"); len(f) > 0 {
		fields = ensureFields(f, "id")
	}

	resp, err := solrClient.MoreLikeThis(ctx, id, rows, filterQueries, fields)
	if err != nil {
		return nil, fmt.Errorf("similar search failed: %w", err)
	}

	return ToolOutput{
		Text:       formatSimilarResults(id, resp),
		Structured: resp,
	}, nil
}

func formatSimilarResults(sourceID string, resp *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories similar to %s.\n\n", resp.NumFound, sourceID))

	for i, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		content, _ := doc["content"].(string)
		memType, _ := doc["memory_type"].(string)

		sb.WriteString(fmt.Sprintf("--- Similar %d ---\n", i+1))
		sb.WriteString(fmt.Sprintf("ID: %s\n", id))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", title))
		}
		if memType != "" {
			sb.WriteString(fmt.Sprintf("Type: %s\n", memType))
		}
		if content != "" {
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("Content: %s\n", content))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
