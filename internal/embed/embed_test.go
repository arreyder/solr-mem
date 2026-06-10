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
	v, err := e.Embed(context.Background(), "x")
	if err != nil || v != nil {
		t.Fatalf("Disabled.Embed = %v, %v; want nil, nil", v, err)
	}
}

func TestOllamaEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var req map[string]any
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		if req["model"] != "nomic-embed-text" || req["prompt"] != "hello" {
			t.Errorf("unexpected request body: %v", req)
		}
		io.WriteString(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "nomic-embed-text", 3)
	if !o.Enabled() || o.Dim() != 3 {
		t.Fatalf("Enabled/Dim wrong: %v %d", o.Enabled(), o.Dim())
	}
	v, err := o.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed err: %v", err)
	}
	if len(v) != 3 || v[0] != 0.1 {
		t.Fatalf("vector = %v", v)
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
