package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func memoryStatsTool(ctx context.Context, args map[string]any) (any, error) {
	params := solr.QueryParams{
		Query:       "*:*",
		Rows:        0,
		Facet:       true,
		FacetFields: []string{"memory_type", "tags", "agent_id", "source", "lifetime"},
	}

	if v := getString(args, "agent_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("agent_id:%q", v))
	}

	resp, err := solrClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stats query failed: %w", err)
	}

	// Get oldest and newest memories
	oldestParams := solr.QueryParams{
		Query:  "*:*",
		Rows:   1,
		Sort:   "created_at asc",
		Fields: []string{"created_at"},
	}
	newestParams := solr.QueryParams{
		Query:  "*:*",
		Rows:   1,
		Sort:   "created_at desc",
		Fields: []string{"created_at"},
	}

	if v := getString(args, "agent_id"); v != "" {
		fq := fmt.Sprintf("agent_id:%q", v)
		oldestParams.FilterQueries = append(oldestParams.FilterQueries, fq)
		newestParams.FilterQueries = append(newestParams.FilterQueries, fq)
	}

	oldest, _ := solrClient.Query(ctx, oldestParams)
	newest, _ := solrClient.Query(ctx, newestParams)

	// Retrieval feedback: most-surfaced memories + count of never-retrieved
	// ("dead") ones. -retrieval_count:[1 TO *] matches both 0 and missing
	// (memories indexed before the field existed / never surfaced).
	agentFQ := []string(nil)
	if v := getString(args, "agent_id"); v != "" {
		agentFQ = []string{fmt.Sprintf("agent_id:%q", v)}
	}
	topRetrieved, _ := solrClient.Query(ctx, solr.QueryParams{
		Query:         "retrieval_count:[1 TO *]",
		Rows:          5,
		Sort:          "retrieval_count desc",
		Fields:        []string{"id", "title", "retrieval_count", "last_retrieved_at"},
		FilterQueries: agentFQ,
	})
	neverRetrieved, _ := solrClient.Query(ctx, solr.QueryParams{
		Query:         "-retrieval_count:[1 TO *]",
		Rows:          0,
		FilterQueries: agentFQ,
	})

	return ToolOutput{
		Text:       formatStats(resp, oldest, newest, topRetrieved, neverRetrieved),
		Structured: resp,
	}, nil
}

func formatStats(resp *solr.QueryResponse, oldest, newest, topRetrieved, neverRetrieved *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total memories: %d\n\n", resp.NumFound))

	// Date range
	if oldest != nil && len(oldest.Docs) > 0 {
		if v, ok := oldest.Docs[0]["created_at"].(string); ok {
			sb.WriteString(fmt.Sprintf("Oldest: %s\n", v))
		}
	}
	if newest != nil && len(newest.Docs) > 0 {
		if v, ok := newest.Docs[0]["created_at"].(string); ok {
			sb.WriteString(fmt.Sprintf("Newest: %s\n", v))
		}
	}
	sb.WriteString("\n")

	// Retrieval feedback summary
	if neverRetrieved != nil {
		sb.WriteString(fmt.Sprintf("Never retrieved (dead): %d\n", neverRetrieved.NumFound))
	}
	if topRetrieved != nil && len(topRetrieved.Docs) > 0 {
		sb.WriteString("Most retrieved:\n")
		for _, d := range topRetrieved.Docs {
			title, _ := d["title"].(string)
			count := toInt(d["retrieval_count"])
			last, _ := d["last_retrieved_at"].(string)
			if title == "" {
				title, _ = d["id"].(string)
			}
			sb.WriteString(fmt.Sprintf("  %d  %s", count, title))
			if last != "" {
				sb.WriteString(fmt.Sprintf("  (last %s)", last))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	// Facets
	for field, counts := range resp.Facets {
		sb.WriteString(fmt.Sprintf("By %s:\n", field))
		for _, fc := range counts {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", fc.Value, fc.Count))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// toInt coerces a Solr numeric field (often float64 over JSON) to int.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
