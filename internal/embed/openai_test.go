package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/wrong Authorization header: %q", got)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("wrong content-type: %q", ct)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("bad body: %v", err)
		}
		if body["model"] != DefaultOpenAIModel {
			t.Errorf("wrong model: %v", body["model"])
		}
		if body["input"] != "hello" {
			t.Errorf("wrong input: %v", body["input"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAI("test-key", "")
	p.baseURL = server.URL

	v, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(v) != 3 {
		t.Errorf("expected 3 dims, got %d", len(v))
	}
	if v[0] != 0.1 || v[1] != 0.2 || v[2] != 0.3 {
		t.Errorf("unexpected vector: %v", v)
	}
	// After a successful call with unexpected dim, Dim() should adapt.
	if p.Dim() != 3 {
		t.Errorf("dim should update to actual response size: got %d", p.Dim())
	}
}

func TestOpenAIEmbedEmptyInput(t *testing.T) {
	p := NewOpenAI("k", "")
	if _, err := p.Embed(context.Background(), ""); err == nil {
		t.Error("expected error for empty text")
	}
}

func TestOpenAIEmbedErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := NewOpenAI("k", "")
	p.baseURL = server.URL

	_, err := p.Embed(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should include status code, got: %v", err)
	}
}

func TestOpenAIEmbedEmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()

	p := NewOpenAI("k", "")
	p.baseURL = server.URL

	if _, err := p.Embed(context.Background(), "x"); err == nil {
		t.Error("expected error on empty data array")
	}
}

func TestOpenAINameAndDim(t *testing.T) {
	p := NewOpenAI("k", "")
	if p.Name() != "openai:"+DefaultOpenAIModel {
		t.Errorf("name: got %s", p.Name())
	}
	if p.Dim() != DefaultOpenAIDim {
		t.Errorf("dim: got %d", p.Dim())
	}

	p2 := NewOpenAI("k", "text-embedding-3-large")
	if !strings.HasSuffix(p2.Name(), "text-embedding-3-large") {
		t.Errorf("custom model not reflected in name: %s", p2.Name())
	}
}

func TestFromEnvNoProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	p, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil provider, got %v", p)
	}
}

func TestFromEnvOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_EMBEDDING_MODEL", "custom-model")
	p, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if !strings.Contains(p.Name(), "custom-model") {
		t.Errorf("provider should use configured model: %s", p.Name())
	}
}
