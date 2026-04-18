package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/arreyder/solr-mem/internal/retrieval"
	"github.com/arreyder/solr-mem/internal/solr"
)

func searchMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
	query := getString(args, "query")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	rows := getInt(args, "limit", 10)

	// Build filter queries shared by both BM25 and KNN passes. This keeps
	// semantic hits scoped to the same agent / session / tag set as keyword
	// hits.
	filters := buildSearchFilters(args)

	bm25Params := solr.QueryParams{
		Query:         query,
		Rows:          rows,
		Highlight:     getBool(args, "highlight", true),
		Facet:         getBool(args, "facet", false),
		FilterQueries: filters,
	}
	if bm25Params.Facet {
		bm25Params.FacetFields = []string{"memory_type", "tags", "agent_id", "source"}
	}

	bm25Resp, err := solrClient.Query(ctx, bm25Params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Semantic (KNN) stream: only runs when the server has an embed provider
	// configured and the caller hasn't opted out via semantic=false.
	semantic := getBool(args, "semantic", true) && embedProvider != nil
	knnTopK := getInt(args, "knn_topk", rows*3) // fetch wider so fusion has something to cross-pollinate

	resp := bm25Resp
	if semantic {
		if fused, err := runHybrid(ctx, query, filters, bm25Resp, knnTopK, rows); err == nil && fused != nil {
			resp = fused
		} else if err != nil {
			log.Printf("search: semantic fallback to BM25-only: %v", err)
		}
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

// buildSearchFilters translates the search_memories args into Solr fq clauses.
func buildSearchFilters(args map[string]any) []string {
	var filters []string
	if v := getString(args, "agent_id"); v != "" {
		filters = append(filters, fmt.Sprintf("agent_id:%q", v))
	}
	if v := getString(args, "memory_type"); v != "" {
		filters = append(filters, fmt.Sprintf("memory_type:%q", v))
	}
	if v := getString(args, "source"); v != "" {
		filters = append(filters, fmt.Sprintf("source:%q", v))
	}
	if tags := getStringSlice(args, "tags"); len(tags) > 0 {
		for _, tag := range tags {
			filters = append(filters, fmt.Sprintf("tags:%q", tag))
		}
	}
	if v := getString(args, "session_id"); v != "" {
		filters = append(filters, fmt.Sprintf("session_id:%q", v))
	}
	if v := getString(args, "lifetime"); v != "" {
		filters = append(filters, fmt.Sprintf("lifetime:%q", v))
	}
	if impMin := getString(args, "importance_min"); impMin != "" {
		filters = append(filters, fmt.Sprintf("importance:[%s TO *]", impMin))
	}
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
		filters = append(filters, fmt.Sprintf("created_at:[%s TO %s]", fromVal, toVal))
	}
	return filters
}

// runHybrid embeds the query, runs a KNN search alongside the BM25 hits
// already in bm25Resp, and fuses them with RRF. Returns a synthetic
// QueryResponse whose Docs are in fused order and whose Facets/Highlighting
// are inherited from the BM25 response. Returns (nil, nil) if the embed
// failed softly (KNN skipped) — caller stays on BM25.
func runHybrid(ctx context.Context, query string, filters []string, bm25Resp *solr.QueryResponse, knnTopK, finalRows int) (*solr.QueryResponse, error) {
	vec, err := embedProvider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	knnResp, err := runKNN(ctx, solrClient, vec, filters, knnTopK)
	if err != nil {
		return nil, fmt.Errorf("knn query: %w", err)
	}

	bm25IDs := idsFromDocs(bm25Resp.Docs)
	knnIDs := idsFromDocs(knnResp.Docs)

	fused := retrieval.FuseIDs([]retrieval.Stream{
		{Name: "bm25", IDs: bm25IDs},
		{Name: "vec", IDs: knnIDs},
	}, retrieval.RRFWithTopK(finalRows))

	// Hydrate: prefer the BM25 doc (has highlighting) over the KNN doc for
	// any ID present in both.
	byID := make(map[string]map[string]any, len(bm25Resp.Docs)+len(knnResp.Docs))
	for _, d := range knnResp.Docs {
		if id, ok := d["id"].(string); ok {
			byID[id] = d
		}
	}
	for _, d := range bm25Resp.Docs {
		if id, ok := d["id"].(string); ok {
			byID[id] = d
		}
	}

	ordered := make([]map[string]any, 0, len(fused))
	for _, id := range fused {
		if d, ok := byID[id]; ok {
			ordered = append(ordered, d)
		}
	}

	return &solr.QueryResponse{
		NumFound:     len(ordered),
		Docs:         ordered,
		Highlighting: bm25Resp.Highlighting,
		Facets:       bm25Resp.Facets,
	}, nil
}

func idsFromDocs(docs []map[string]any) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if id, ok := d["id"].(string); ok {
			out = append(out, id)
		}
	}
	return out
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
