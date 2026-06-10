package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func listMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
	params := solr.QueryParams{
		Query:       "*:*",
		Rows:        getInt(args, "limit", 20),
		Start:       getInt(args, "start", 0),
		Sort:        getString(args, "sort"),
		Highlight:   false,
		Facet:       true,
		FacetFields: []string{"memory_type", "tags", "agent_id", "source", "lifetime"},
	}

	if params.Sort == "" {
		params.Sort = "created_at desc"
	}

	// Optional field projection (Solr fl) for lean payloads; always keep id.
	if fields := getStringSlice(args, "fields"); len(fields) > 0 {
		params.Fields = ensureFields(fields, "id")
	}

	if v := getString(args, "agent_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("agent_id:%q", v))
	}
	if v := getString(args, "memory_type"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("memory_type:%q", v))
	}
	if v := getString(args, "source"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("source:%q", v))
	}
	if v := getString(args, "session_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("session_id:%q", v))
	}
	if v := getString(args, "lifetime"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("lifetime:%q", v))
	}

	resp, err := solrClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list failed: %w", err)
	}

	return ToolOutput{
		Text:       formatListResults(resp),
		Structured: resp,
	}, nil
}

func formatListResults(resp *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total memories: %d\n\n", resp.NumFound))

	for i, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		content, _ := doc["content"].(string)
		memType, _ := doc["memory_type"].(string)
		createdAt, _ := doc["created_at"].(string)

		sb.WriteString(fmt.Sprintf("%d. [%s] ", i+1, id))
		if title != "" {
			sb.WriteString(title)
		} else if len(content) > 80 {
			sb.WriteString(content[:80] + "...")
		} else {
			sb.WriteString(content)
		}
		if memType != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", memType))
		}
		if createdAt != "" {
			sb.WriteString(fmt.Sprintf(" - %s", createdAt))
		}
		sb.WriteString("\n")
	}

	if len(resp.Facets) > 0 {
		sb.WriteString("\n--- Overview ---\n")
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
