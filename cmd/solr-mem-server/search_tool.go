package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func searchMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
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

	// Build filter queries
	if v := getString(args, "agent_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("agent_id:%q", v))
	}
	if v := getString(args, "memory_type"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("memory_type:%q", v))
	}
	if v := getString(args, "source"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("source:%q", v))
	}
	if tags := getStringSlice(args, "tags"); len(tags) > 0 {
		for _, tag := range tags {
			params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("tags:%q", tag))
		}
	}
	if v := getString(args, "session_id"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("session_id:%q", v))
	}
	if v := getString(args, "lifetime"); v != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("lifetime:%q", v))
	}

	// Importance filter
	impMin := getString(args, "importance_min")
	if impMin != "" {
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("importance:[%s TO *]", impMin))
	}

	// Date range filters
	from := getString(args, "from")
	to := getString(args, "to")
	if from != "" || to != "" {
		fromVal := "*"
		toVal := "*"
		if from != "" {
			fromVal = from
		}
		if to != "" {
			toVal = to
		}
		params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("created_at:[%s TO %s]", fromVal, toVal))
	}

	if params.Facet {
		params.FacetFields = []string{"memory_type", "tags", "agent_id", "source"}
	}

	resp, err := solrClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Cap per session so one chatty session can't dominate results.
	// Default 3; pass 0 to disable.
	sessionCap := getInt(args, "session_cap", 3)
	if sessionCap > 0 {
		resp.Docs = diversifyBySession(resp.Docs, func(d map[string]any) string {
			s, _ := d["session_id"].(string)
			return s
		}, sessionCap)
	}

	return ToolOutput{
		Text:       formatSearchResults(resp),
		Structured: resp,
	}, nil
}

func formatSearchResults(resp *solr.QueryResponse) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memories.\n\n", resp.NumFound))

	for i, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		content, _ := doc["content"].(string)
		memType, _ := doc["memory_type"].(string)
		agentID, _ := doc["agent_id"].(string)

		sb.WriteString(fmt.Sprintf("--- Result %d ---\n", i+1))
		sb.WriteString(fmt.Sprintf("ID: %s\n", id))
		if title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", title))
		}
		if memType != "" {
			sb.WriteString(fmt.Sprintf("Type: %s\n", memType))
		}
		if agentID != "" {
			sb.WriteString(fmt.Sprintf("Agent: %s\n", agentID))
		}

		// Show highlights if available, otherwise show content
		if hl, ok := resp.Highlighting[id]; ok {
			for field, snippets := range hl {
				sb.WriteString(fmt.Sprintf("%s highlights: %s\n", field, strings.Join(snippets, " ... ")))
			}
		} else if content != "" {
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("Content: %s\n", content))
		}

		// Show tags
		if tags, ok := doc["tags"]; ok {
			tagsJSON, _ := json.Marshal(tags)
			sb.WriteString(fmt.Sprintf("Tags: %s\n", tagsJSON))
		}

		sb.WriteString("\n")
	}

	// Show facets if present
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
