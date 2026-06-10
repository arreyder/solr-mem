// Package embed provides text embeddings for semantic memory search.
//
// The default backend is an Ollama-compatible HTTP endpoint. When no endpoint
// is configured the package returns a disabled embedder, so the rest of the
// system degrades gracefully to lexical-only search instead of failing.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Embedder turns text into a dense vector. Implementations must be safe for
// concurrent use.
//
// Query and document embedding are distinct because some models (e.g.
// nomic-embed-text) are trained for asymmetric retrieval and expect different
// task prefixes on the stored text vs. the search query. Always embed stored
// memories with EmbedDocument and search queries with EmbedQuery so the two
// land in the same space.
type Embedder interface {
	// Enabled reports whether embeddings are available. When false, callers
	// should fall back to lexical-only behavior.
	Enabled() bool
	// Dim is the vector dimension (must match the Solr DenseVectorField).
	Dim() int
	// EmbedDocument embeds text to be stored/indexed.
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
	// EmbedQuery embeds a search query.
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// FromEnv builds an Embedder from EMBED_URL / EMBED_MODEL / EMBED_DIM.
// If EMBED_URL is empty, returns a disabled embedder (semantic search off).
func FromEnv() Embedder {
	url := os.Getenv("EMBED_URL")
	if url == "" {
		return Disabled{}
	}
	model := os.Getenv("EMBED_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}
	dim := 768
	if v := os.Getenv("EMBED_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			dim = n
		}
	}
	o := NewOllama(url, model, dim)
	if v := os.Getenv("EMBED_MAX_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			o.maxChars = n
		}
	}
	// Task prefixes: default to nomic's for nomic models, off otherwise.
	// EMBED_*_PREFIX env overrides either way (set to " " to force-clear).
	if strings.Contains(strings.ToLower(model), "nomic") {
		o.docPrefix = "search_document: "
		o.queryPrefix = "search_query: "
	}
	if v, ok := os.LookupEnv("EMBED_DOC_PREFIX"); ok {
		o.docPrefix = v
	}
	if v, ok := os.LookupEnv("EMBED_QUERY_PREFIX"); ok {
		o.queryPrefix = v
	}
	return o
}

// Disabled is a no-op embedder used when no backend is configured.
type Disabled struct{}

func (Disabled) Enabled() bool { return false }
func (Disabled) Dim() int      { return 0 }
func (Disabled) EmbedDocument(context.Context, string) ([]float32, error) {
	return nil, nil
}
func (Disabled) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, nil
}

// Ollama embeds via an Ollama-compatible /api/embeddings endpoint.
type Ollama struct {
	baseURL     string
	model       string
	dim         int
	maxChars    int    // truncate input to this many runes (model context guard)
	docPrefix   string // task prefix for stored documents
	queryPrefix string // task prefix for search queries
	client      *http.Client
}

// defaultMaxChars keeps embed input under typical small-model context windows
// (nomic-embed-text ~2048 tokens). Conservative at ~4 chars/token.
const defaultMaxChars = 6000

// NewOllama builds an Ollama embedder. baseURL is the host root, e.g.
// "http://pax99.local:11434".
func NewOllama(baseURL, model string, dim int) *Ollama {
	return &Ollama{
		baseURL:  baseURL,
		model:    model,
		dim:      dim,
		maxChars: defaultMaxChars,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *Ollama) Enabled() bool { return true }
func (o *Ollama) Dim() int      { return o.dim }

func (o *Ollama) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return o.embed(ctx, o.docPrefix+text)
}

func (o *Ollama) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return o.embed(ctx, o.queryPrefix+text)
}

func (o *Ollama) embed(ctx context.Context, text string) ([]float32, error) {
	text = truncateRunes(text, o.maxChars)
	body, err := json.Marshal(map[string]any{"model": o.model, "prompt": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed returned %d: %s", resp.StatusCode, b)
	}
	return parseEmbedding(resp.Body, o.dim)
}

// truncateRunes caps s to at most max runes (UTF-8 safe). max<=0 means no cap.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// parseEmbedding decodes an Ollama embeddings response and validates the
// dimension. Split out for testing.
func parseEmbedding(r io.Reader, wantDim int) ([]float32, error) {
	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embedding: %w", err)
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("embedding response had no vector")
	}
	if wantDim > 0 && len(out.Embedding) != wantDim {
		return nil, fmt.Errorf("embedding dim %d != expected %d", len(out.Embedding), wantDim)
	}
	return out.Embedding, nil
}
