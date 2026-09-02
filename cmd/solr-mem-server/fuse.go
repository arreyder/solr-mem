package main

import (
	"sort"

	"github.com/arreyder/solr-mem/internal/solr"
)

// rrfK is the reciprocal-rank-fusion constant. 60 is the widely-used default
// from the original RRF paper; it damps the influence of any single ranker's
// top positions so the two lists combine smoothly.
const rrfK = 60

func docID(d map[string]any) string {
	s, _ := d["id"].(string)
	return s
}

// fuseResponses combines lexical and semantic (KNN) result lists with
// reciprocal rank fusion: score(d) = Σ 1/(rrfK + rank) across the lists it
// appears in. Returns a response with docs ordered by fused score (desc),
// de-duplicated by id and capped to limit, with highlighting merged from both.
// Ties break by lexical order first, then semantic — deterministic regardless
// of map iteration.
func fuseResponses(lexical, semantic *solr.QueryResponse, limit int) *solr.QueryResponse {
	scores := map[string]float64{}
	docByID := map[string]map[string]any{}
	var order []string // deterministic seed order: lexical first, then semantic-only
	seen := map[string]bool{}

	accumulate := func(resp *solr.QueryResponse) {
		if resp == nil {
			return
		}
		for rank, d := range resp.Docs {
			id := docID(d)
			if id == "" {
				continue
			}
			scores[id] += 1.0 / float64(rrfK+rank+1) // rank is 0-based
			if !seen[id] {
				seen[id] = true
				order = append(order, id)
				docByID[id] = d
			}
		}
	}
	accumulate(lexical)
	accumulate(semantic)

	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})
	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}

	docs := make([]map[string]any, 0, len(order))
	for _, id := range order {
		docs = append(docs, docByID[id])
	}

	hl := map[string]map[string][]string{}
	for _, resp := range []*solr.QueryResponse{lexical, semantic} {
		if resp == nil {
			continue
		}
		for k, v := range resp.Highlighting {
			hl[k] = v
		}
	}

	out := &solr.QueryResponse{Docs: docs, Highlighting: hl}
	if lexical != nil {
		out.Facets = lexical.Facets
		out.NumFound = lexical.NumFound
	}
	// Don't report fewer than we're actually returning — semantic-only hits
	// aren't counted in the lexical NumFound.
	if len(docs) > out.NumFound {
		out.NumFound = len(docs)
	}
	return out
}
