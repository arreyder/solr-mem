package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arreyder/solr-mem/internal/solr"
)

// fakeProvider returns a deterministic 3-dim vector for any input, or errors
// when Fail is non-nil.
type fakeProvider struct {
	mu    sync.Mutex
	calls []string
	Fail  map[string]error // text -> err
}

func (f *fakeProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	f.calls = append(f.calls, text)
	f.mu.Unlock()
	if err, ok := f.Fail[text]; ok {
		return nil, err
	}
	return []float32{0.1, 0.2, 0.3}, nil
}
func (f *fakeProvider) Dim() int     { return 3 }
func (f *fakeProvider) Name() string { return "fake" }

// fakeSolr is a minimal httptest.Server standing in for the memories
// collection. It supports /select (returning the configured docs, shrunk
// after each update call) and /update (accepting atomic updates and
// removing the target doc from the "missing embedding" set).
type fakeSolr struct {
	mu      sync.Mutex
	docs    []map[string]any // mutable "collection"
	queries int
	updates [][]map[string]any // captured update payloads per request
}

func newFakeSolr(initial []map[string]any) *fakeSolr {
	return &fakeSolr{docs: initial}
}

func (s *fakeSolr) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/select"):
			s.handleSelect(w, r)
		case strings.Contains(r.URL.Path, "/update"):
			s.handleUpdate(w, r)
		default:
			http.Error(w, "not found: "+r.URL.Path, 404)
		}
	})
}

func (s *fakeSolr) handleSelect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++

	fqs := r.URL.Query()["fq"]
	missingOnly := false
	for _, fq := range fqs {
		if fq == "-_exists_:embedding" {
			missingOnly = true
			break
		}
	}

	var out []map[string]any
	for _, d := range s.docs {
		if missingOnly {
			if v, ok := d["embedding"]; ok && v != nil {
				if arr, ok := v.([]any); ok && len(arr) > 0 {
					continue
				}
				if _, ok := v.([]float32); ok {
					continue
				}
			}
		}
		out = append(out, d)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"response": map[string]any{
			"numFound": len(out),
			"start":    0,
			"docs":     out,
		},
	})
}

func (s *fakeSolr) handleUpdate(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var payload []map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.updates = append(s.updates, payload)

	// Apply atomic updates to our in-memory corpus.
	for _, upd := range payload {
		id, _ := upd["id"].(string)
		for i, d := range s.docs {
			if d["id"] != id {
				continue
			}
			for k, v := range upd {
				if k == "id" {
					continue
				}
				if setter, ok := v.(map[string]any); ok {
					if val, has := setter["set"]; has {
						// Normalize to []any for consistent downstream checks.
						if vec, ok := val.([]float32); ok {
							arr := make([]any, len(vec))
							for j, f := range vec {
								arr[j] = float64(f)
							}
							val = arr
						}
						s.docs[i][k] = val
					}
				}
			}
		}
	}
	_, _ = w.Write([]byte(`{"responseHeader":{"status":0}}`))
}

func TestRunEmbedsMissingDocs(t *testing.T) {
	fs := newFakeSolr([]map[string]any{
		{"id": "a", "title": "A", "content": "alpha"},
		{"id": "b", "title": "B", "content": "beta"},
	})
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	client := solr.NewClient(srv.URL)
	provider := &fakeProvider{}

	stats, err := Run(context.Background(), client, provider, Options{BatchSize: 10, Concurrency: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Embedded != 2 {
		t.Errorf("expected 2 embedded, got %d", stats.Embedded)
	}
	if stats.Written != 2 {
		t.Errorf("expected 2 written, got %d", stats.Written)
	}
	if stats.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", stats.Errors)
	}
	if len(provider.calls) != 2 {
		t.Errorf("expected 2 provider calls, got %d", len(provider.calls))
	}
}

func TestRunDryRunSkipsWrite(t *testing.T) {
	fs := newFakeSolr([]map[string]any{
		{"id": "a", "title": "A", "content": "alpha"},
	})
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	client := solr.NewClient(srv.URL)
	provider := &fakeProvider{}

	stats, err := Run(context.Background(), client, provider, Options{DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Embedded != 1 {
		t.Errorf("expected 1 embedded, got %d", stats.Embedded)
	}
	if stats.Written != 0 {
		t.Errorf("dry-run should write nothing, got %d", stats.Written)
	}
	if len(fs.updates) != 0 {
		t.Errorf("dry-run should not POST updates, got %d payloads", len(fs.updates))
	}
}

func TestRunRespectsMaxDocs(t *testing.T) {
	var docs []map[string]any
	for i := 0; i < 10; i++ {
		docs = append(docs, map[string]any{
			"id":      fmt.Sprintf("d-%d", i),
			"title":   "t",
			"content": "c",
		})
	}
	fs := newFakeSolr(docs)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	client := solr.NewClient(srv.URL)
	provider := &fakeProvider{}

	stats, err := Run(context.Background(), client, provider, Options{BatchSize: 3, MaxDocs: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Embedded != 5 {
		t.Errorf("MaxDocs=5: expected 5 embedded, got %d", stats.Embedded)
	}
}

func TestRunContinuesThroughEmbedErrors(t *testing.T) {
	fs := newFakeSolr([]map[string]any{
		{"id": "good", "title": "G", "content": "g"},
		{"id": "bad", "title": "B", "content": "b"},
	})
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	client := solr.NewClient(srv.URL)
	provider := &fakeProvider{
		Fail: map[string]error{"B\n\nb": fmt.Errorf("simulated failure")},
	}

	stats, err := Run(context.Background(), client, provider, Options{Concurrency: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Errors != 1 {
		t.Errorf("expected 1 error, got %d", stats.Errors)
	}
	if stats.Embedded != 1 {
		t.Errorf("expected 1 successful embed, got %d", stats.Embedded)
	}
	if stats.Written != 1 {
		t.Errorf("expected 1 write, got %d", stats.Written)
	}
}

func TestRunStopsWhenNoMissingRemain(t *testing.T) {
	// After the first batch updates every doc, the next fetch should return
	// zero and we terminate.
	fs := newFakeSolr([]map[string]any{
		{"id": "a", "title": "A", "content": "a"},
	})
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	client := solr.NewClient(srv.URL)
	provider := &fakeProvider{}

	_, err := Run(context.Background(), client, provider, Options{BatchSize: 10})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Expect exactly 2 selects: first returns the doc, second returns empty.
	if fs.queries != 2 {
		t.Errorf("expected 2 queries (work + empty probe), got %d", fs.queries)
	}
}

func TestRunRefusesWithoutProvider(t *testing.T) {
	fs := newFakeSolr(nil)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	client := solr.NewClient(srv.URL)

	if _, err := Run(context.Background(), client, nil, Options{}); err == nil {
		t.Error("expected error when provider is nil")
	}
}

func TestBuildEmbedText(t *testing.T) {
	cases := []struct{ title, content, want string }{
		{"t", "c", "t\n\nc"},
		{"", "c", "c"},
		{"t", "", "t"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := buildEmbedText(c.title, c.content); got != c.want {
			t.Errorf("buildEmbedText(%q,%q)=%q, want %q", c.title, c.content, got, c.want)
		}
	}
}
