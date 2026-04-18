package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAI provides embeddings via the OpenAI /v1/embeddings API. It is the
// reference provider; other providers follow the same interface.
type OpenAI struct {
	apiKey  string
	model   string
	dim     int
	baseURL string
	client  *http.Client
}

// DefaultOpenAIModel is OpenAI's current small text embedding model. It emits
// 1536-dimensional vectors, which the Solr schema must match.
const (
	DefaultOpenAIModel = "text-embedding-3-small"
	DefaultOpenAIDim   = 1536
)

// NewOpenAI constructs a client. If model is empty, DefaultOpenAIModel is used.
func NewOpenAI(apiKey, model string) *OpenAI {
	if model == "" {
		model = DefaultOpenAIModel
	}
	return &OpenAI{
		apiKey:  apiKey,
		model:   model,
		dim:     DefaultOpenAIDim,
		baseURL: "https://api.openai.com/v1",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implements Provider.
func (o *OpenAI) Name() string { return "openai:" + o.model }

// Dim implements Provider.
func (o *OpenAI) Dim() int { return o.dim }

// Embed implements Provider. A non-200 response or malformed payload
// returns an error with enough context for debugging.
func (o *OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("embed: empty text")
	}

	body, err := json.Marshal(map[string]any{
		"model": o.model,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

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
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	v := out.Data[0].Embedding
	if len(v) != o.dim {
		// Update our advertised dim on first successful call so queries
		// don't drift. OpenAI allows a `dimensions` override; we don't
		// set one here, so the response should match DefaultOpenAIDim.
		o.dim = len(v)
	}
	return v, nil
}
