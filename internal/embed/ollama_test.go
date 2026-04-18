package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaEmbedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("bad body: %v", err)
		}
		if body["model"] != DefaultOllamaModel {
			t.Errorf("wrong model: %v", body["model"])
		}
		if body["prompt"] != "hello world" {
			t.Errorf("wrong prompt: %v", body["prompt"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float32{0.1, 0.2, 0.3, 0.4},
		})
	}))
	defer server.Close()

	p := NewOllama(server.URL, "")
	v, err := p.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(v) != 4 {
		t.Errorf("expected 4 dims, got %d", len(v))
	}
	// Dim should be learned from the response.
	if p.Dim() != 4 {
		t.Errorf("dim should update after first call, got %d", p.Dim())
	}
}

func TestOllamaCustomModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "mxbai-embed-large" {
			t.Errorf("expected mxbai-embed-large, got %v", body["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{1.0}})
	}))
	defer server.Close()

	p := NewOllama(server.URL, "mxbai-embed-large")
	if _, err := p.Embed(context.Background(), "x"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(p.Name(), "mxbai-embed-large") {
		t.Errorf("name should include model: %s", p.Name())
	}
}

func TestOllamaTrailingSlashInURL(t *testing.T) {
	// NewOllama should trim trailing slash so we don't double it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{0.0}})
	}))
	defer server.Close()

	p := NewOllama(server.URL+"/", "")
	if _, err := p.Embed(context.Background(), "x"); err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestOllamaEmptyInput(t *testing.T) {
	p := NewOllama("http://irrelevant", "")
	if _, err := p.Embed(context.Background(), ""); err == nil {
		t.Error("expected error for empty text")
	}
}

func TestOllamaErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	p := NewOllama(server.URL, "")
	_, err := p.Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should include status, got: %v", err)
	}
}

func TestOllamaEmptyEmbedding(t *testing.T) {
	// Ollama returns 200 with an empty embedding when the model isn't pulled.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{}})
	}))
	defer server.Close()

	p := NewOllama(server.URL, "")
	_, err := p.Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on empty embedding")
	}
	if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("error should name the failure mode, got: %v", err)
	}
}

func TestFromEnvPrefersOllama(t *testing.T) {
	// When both Ollama and OpenAI are configured, Ollama wins (local/free beats remote/paid).
	t.Setenv("OLLAMA_EMBEDDING_URL", "http://mac.local:11434")
	t.Setenv("OLLAMA_EMBEDDING_MODEL", "mxbai-embed-large")
	t.Setenv("OPENAI_API_KEY", "sk-test")

	p, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p == nil {
		t.Fatal("expected a provider")
	}
	if !strings.HasPrefix(p.Name(), "ollama:") {
		t.Errorf("expected ollama provider to win, got %s", p.Name())
	}
	if !strings.Contains(p.Name(), "mxbai-embed-large") {
		t.Errorf("custom model should be reflected: %s", p.Name())
	}
}

func TestFromEnvOllamaAloneUsesDefault(t *testing.T) {
	t.Setenv("OLLAMA_EMBEDDING_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_EMBEDDING_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")

	p, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p == nil {
		t.Fatal("expected a provider")
	}
	if !strings.Contains(p.Name(), DefaultOllamaModel) {
		t.Errorf("should default to %s, got %s", DefaultOllamaModel, p.Name())
	}
}
