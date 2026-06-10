package embed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabled(t *testing.T) {
	var e Embedder = Disabled{}
	if e.Enabled() {
		t.Fatal("Disabled must report not enabled")
	}
	if v, err := e.EmbedDocument(context.Background(), "x"); err != nil || v != nil {
		t.Fatalf("Disabled.EmbedDocument = %v, %v; want nil, nil", v, err)
	}
	if v, err := e.EmbedQuery(context.Background(), "x"); err != nil || v != nil {
		t.Fatalf("Disabled.EmbedQuery = %v, %v; want nil, nil", v, err)
	}
}

func TestOllamaEmbedAndPrefixes(t *testing.T) {
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var req map[string]any
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		gotPrompt, _ = req["prompt"].(string)
		io.WriteString(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "nomic-embed-text", 3)
	o.docPrefix = "search_document: "
	o.queryPrefix = "search_query: "

	v, err := o.EmbedDocument(context.Background(), "hello")
	if err != nil || len(v) != 3 || v[0] != 0.1 {
		t.Fatalf("EmbedDocument = %v, %v", v, err)
	}
	if gotPrompt != "search_document: hello" {
		t.Errorf("doc prompt = %q, want prefixed", gotPrompt)
	}

	if _, err := o.EmbedQuery(context.Background(), "hello"); err != nil {
		t.Fatalf("EmbedQuery err: %v", err)
	}
	if gotPrompt != "search_query: hello" {
		t.Errorf("query prompt = %q, want prefixed", gotPrompt)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 3); got != "hel" {
		t.Errorf("got %q", got)
	}
	if got := truncateRunes("hi", 10); got != "hi" {
		t.Errorf("no-trunc got %q", got)
	}
	if got := truncateRunes("hello", 0); got != "hello" {
		t.Errorf("max<=0 must be no-op, got %q", got)
	}
	// multi-byte safe
	if got := truncateRunes("héllo", 2); got != "hé" {
		t.Errorf("utf8 got %q", got)
	}
}

func TestParseEmbeddingDimMismatch(t *testing.T) {
	if _, err := parseEmbedding(strings.NewReader(`{"embedding":[1,2]}`), 3); err == nil {
		t.Fatal("expected dim-mismatch error")
	}
	if _, err := parseEmbedding(strings.NewReader(`{"embedding":[]}`), 0); err == nil {
		t.Fatal("expected empty-vector error")
	}
}
