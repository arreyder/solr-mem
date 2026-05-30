package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama provides embeddings via a local (or LAN) Ollama server's
// /api/embeddings endpoint. The output dimension depends on the model —
// it's discovered from the first successful call rather than hardcoded.
type Ollama struct {
	baseURL string
	model   string
	dim     int
	client  *http.Client
}

// DefaultOllamaModel is a small, fast general-purpose embedding model.
// 768 dim, ~137 MB on disk.
const DefaultOllamaModel = "nomic-embed-text"

// NewOllama constructs a client. baseURL should be the Ollama server root
// (e.g. "http://localhost:11434"). If model is empty, DefaultOllamaModel is
// used.
func NewOllama(baseURL, model string) *Ollama {
	baseURL = strings.TrimRight(baseURL, "/")
	if model == "" {
		model = DefaultOllamaModel
	}
	return &Ollama{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements Provider.
func (o *Ollama) Name() string { return "ollama:" + o.model }

// Dim returns 0 until the first successful call discovers the true
// dimension. Callers using Dim() to preconfigure the Solr schema must
// make at least one embed call first (or hardcode the dim from docs).
func (o *Ollama) Dim() int { return o.dim }

// Embed implements Provider. On success, updates the cached dim with the
// actual response length so downstream consumers can introspect it.
func (o *Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("embed: empty text")
	}

	body, err := json.Marshal(map[string]any{
		"model":  o.model,
		"prompt": text,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, string(snippet))
	}

	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if len(out.Embedding) == 0 {
		// Ollama returns 200 with empty embedding on some error cases
		// (e.g. unknown model). Treat as an error.
		return nil, fmt.Errorf("embed: empty embedding returned (model %q may not be pulled)", o.model)
	}
	if o.dim == 0 || o.dim != len(out.Embedding) {
		o.dim = len(out.Embedding)
	}
	return out.Embedding, nil
}
