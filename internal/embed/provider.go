// Package embed turns text into dense vector embeddings for semantic search.
//
// Providers are pluggable. Users configure one via environment variables:
//
//	OPENAI_API_KEY        -> OpenAI text-embedding-3-small (1536 dim by default)
//	OPENAI_EMBEDDING_MODEL -> override model (optional)
//
// If no provider is configured, FromEnv returns (nil, nil) and the server
// runs in BM25-only mode. Callers MUST handle a nil provider as a signal to
// skip embedding rather than erroring.
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
	// equal Dim() and must be stable across calls on the same provider.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dim is the output dimension. Used to validate schema configuration.
	Dim() int

	// Name is a short identifier for logging / metrics.
	Name() string
}

// FromEnv constructs a provider from environment variables. Returns (nil, nil)
// if no provider is configured — callers should treat nil as "embeddings
// disabled" rather than an error.
func FromEnv() (Provider, error) {
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		model := strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_MODEL"))
		return NewOpenAI(key, model), nil
	}
	return nil, nil
}
