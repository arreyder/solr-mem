// Package embed turns text into dense vector embeddings for semantic search.
//
// Providers are pluggable. Users configure one via environment variables.
// FromEnv checks them in this order (first match wins):
//
//  1. Ollama — local / LAN, free:
//     OLLAMA_EMBEDDING_URL    e.g. http://localhost:11434
//     OLLAMA_EMBEDDING_MODEL  e.g. nomic-embed-text (default)
//
//  2. OpenAI — managed, paid:
//     OPENAI_API_KEY
//     OPENAI_EMBEDDING_MODEL  e.g. text-embedding-3-small (default)
//
// If no provider is configured, FromEnv returns (nil, nil) and the server
// runs in BM25-only mode. Callers MUST handle a nil provider as a signal to
// skip embedding rather than erroring.
//
// NOTE: The Solr schema's vectorDimension is static. Ollama's nomic-embed-text
// is 768, mxbai-embed-large is 1024, OpenAI text-embedding-3-small is 1536.
// When switching providers, update solr/managed-schema.xml's vectorDimension
// to match the chosen model and reload the configset before running.
package embed

import (
	"context"
	"os"
	"strings"
)

// Provider produces a dense vector for a piece of text. Implementations
// should be safe for concurrent use.
type Provider interface {
	// Embed returns the embedding for text. The returned slice length must
	// equal Dim() (after the first successful call) and must be stable
	// across calls on the same provider.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dim is the output dimension. Some providers learn this on the first
	// successful call (returning 0 until then); others know it upfront.
	Dim() int

	// Name is a short identifier for logging / metrics.
	Name() string
}

// FromEnv constructs a provider from environment variables. Returns (nil, nil)
// if no provider is configured — callers should treat nil as "embeddings
// disabled" rather than an error.
//
// Ollama is checked first so a local server preempts an accidentally-present
// OpenAI key (free/private beats paid/remote by default).
func FromEnv() (Provider, error) {
	if url := strings.TrimSpace(os.Getenv("OLLAMA_EMBEDDING_URL")); url != "" {
		model := strings.TrimSpace(os.Getenv("OLLAMA_EMBEDDING_MODEL"))
		return NewOllama(url, model), nil
	}
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		model := strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_MODEL"))
		return NewOpenAI(key, model), nil
	}
	return nil, nil
}
