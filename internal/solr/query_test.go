package solr

import (
	"net/url"
	"testing"
)

func parseBuilt(t *testing.T, p QueryParams) url.Values {
	t.Helper()
	vals, err := url.ParseQuery(BuildQueryString(p))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	return vals
}

func TestBuildQueryString_MM(t *testing.T) {
	// Default: no mm emitted, so the request-handler default (solrconfig) wins.
	if got := parseBuilt(t, QueryParams{Query: "a b c"}).Get("mm"); got != "" {
		t.Fatalf("expected no mm by default, got %q", got)
	}
	// Explicit OR-recall override.
	if got := parseBuilt(t, QueryParams{Query: "a b c", MM: "1"}).Get("mm"); got != "1" {
		t.Fatalf("mm=1: got %q", got)
	}
	if got := parseBuilt(t, QueryParams{Query: "a", MM: "100%"}).Get("mm"); got != "100%" {
		t.Fatalf("mm=100%%: got %q", got)
	}
}

func TestBuildQueryString_FieldsAndStart(t *testing.T) {
	vals := parseBuilt(t, QueryParams{
		Query:  "x",
		Fields: []string{"id", "title", "importance"},
		Start:  20,
	})
	if got := vals.Get("fl"); got != "id,title,importance" {
		t.Fatalf("fl: got %q", got)
	}
	if got := vals.Get("start"); got != "20" {
		t.Fatalf("start: got %q", got)
	}

	// start=0 is the default and should be omitted.
	if got := parseBuilt(t, QueryParams{Query: "x"}).Get("start"); got != "" {
		t.Fatalf("expected no start at 0, got %q", got)
	}
	// No fields => no fl (full doc).
	if got := parseBuilt(t, QueryParams{Query: "x"}).Get("fl"); got != "" {
		t.Fatalf("expected no fl by default, got %q", got)
	}
}

func TestBuildMLTValues(t *testing.T) {
	// With a fields projection: fl is set for the RETURNED docs, while mlt.fl
	// (the similarity field set) stays untouched.
	v := buildMLTValues("abc", 5, nil, []string{"id", "title", "importance"})
	if got := v.Get("fl"); got != "id,title,importance" {
		t.Fatalf("fl: got %q", got)
	}
	if got := v.Get("mlt.fl"); got != "content,title,tags" {
		t.Fatalf("mlt.fl must be unchanged, got %q", got)
	}
	if got := v.Get("q"); got != `id:"abc"` {
		t.Fatalf("q: got %q", got)
	}

	// Without fields: no fl, full docs returned.
	if got := buildMLTValues("abc", 5, nil, nil).Get("fl"); got != "" {
		t.Fatalf("expected no fl by default, got %q", got)
	}
}
