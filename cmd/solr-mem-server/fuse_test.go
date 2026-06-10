package main

import (
	"testing"

	"github.com/arreyder/solr-mem/internal/solr"
)

func resp(ids ...string) *solr.QueryResponse {
	docs := make([]map[string]any, len(ids))
	for i, id := range ids {
		docs[i] = map[string]any{"id": id}
	}
	return &solr.QueryResponse{NumFound: len(ids), Docs: docs}
}

func order(r *solr.QueryResponse) []string {
	out := make([]string, len(r.Docs))
	for i, d := range r.Docs {
		out[i] = docID(d)
	}
	return out
}

func TestFuseResponses_RRF(t *testing.T) {
	// b ranks high in both lists -> should win. d is semantic-only -> still included.
	lexical := resp("a", "b", "c")
	semantic := resp("b", "d", "a")

	fused := fuseResponses(lexical, semantic, 10)
	got := order(fused)

	// b: 1/61 + 1/61 (rank0 both) = highest.
	if got[0] != "b" {
		t.Fatalf("expected 'b' first, got %v", got)
	}
	// All four unique ids present, deduped.
	if len(got) != 4 {
		t.Fatalf("expected 4 unique docs, got %v", got)
	}
	// d (semantic-only) is included.
	if !contains(got, "d") {
		t.Errorf("semantic-only 'd' missing: %v", got)
	}
}

func TestFuseResponses_LimitAndNilSemantic(t *testing.T) {
	fused := fuseResponses(resp("a", "b", "c"), nil, 2)
	if got := order(fused); len(got) != 2 || got[0] != "a" {
		t.Fatalf("limit/nil-semantic: got %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
