package solr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client communicates with a Solr core via its REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Solr client for the given core URL.
// baseURL should be like "http://localhost:8983/solr/memories".
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Ping checks that the Solr core is reachable.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/admin/ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("solr ping failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("solr ping returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

// Add indexes one or more documents into Solr.
func (c *Client) Add(ctx context.Context, docs ...Document) error {
	body, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("marshal docs: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/update/json/docs?commitWithin=1000",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("solr add: %w", err)
	}
	defer resp.Body.Close()

	return c.checkResponse(resp)
}

// Query executes a search query against Solr and returns parsed results.
func (c *Client) Query(ctx context.Context, params QueryParams) (*QueryResponse, error) {
	url := c.baseURL + "/select?" + BuildQueryString(params)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solr query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("solr query returned %d: %s", resp.StatusCode, body)
	}

	return ParseQueryResponse(resp.Body)
}

// Update performs an atomic update on a document by ID.
func (c *Client) Update(ctx context.Context, id string, fields map[string]any) error {
	// Build atomic update payload: {"id": "...", "field": {"set": value}}
	doc := map[string]any{"id": id}
	for k, v := range fields {
		doc[k] = map[string]any{"set": v}
	}

	body, err := json.Marshal([]any{doc})
	if err != nil {
		return fmt.Errorf("marshal update: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/update?commitWithin=1000",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("solr update: %w", err)
	}
	defer resp.Body.Close()

	return c.checkResponse(resp)
}

// Delete removes a document by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	payload := map[string]any{
		"delete": map[string]any{"id": id},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal delete: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/update?commitWithin=1000",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("solr delete: %w", err)
	}
	defer resp.Body.Close()

	return c.checkResponse(resp)
}

// MoreLikeThis finds documents similar to the given document ID.
func (c *Client) MoreLikeThis(ctx context.Context, id string, rows int, filterQueries []string) (*QueryResponse, error) {
	v := url.Values{}
	v.Set("q", fmt.Sprintf("id:%q", id))
	v.Set("mlt", "true")
	v.Set("mlt.fl", "content,title,tags")
	v.Set("mlt.mintf", "1")
	v.Set("mlt.mindf", "1")
	v.Set("mlt.minwl", "3")
	v.Set("rows", fmt.Sprintf("%d", rows))
	v.Set("wt", "json")
	for _, fq := range filterQueries {
		v.Add("fq", fq)
	}

	reqURL := c.baseURL + "/mlt?" + v.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solr mlt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("solr mlt returned %d: %s", resp.StatusCode, body)
	}

	return ParseQueryResponse(resp.Body)
}

// DeleteByQuery removes all documents matching the given query.
func (c *Client) DeleteByQuery(ctx context.Context, query string) error {
	payload := map[string]any{
		"delete": map[string]any{"query": query},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal delete by query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/update?commitWithin=1000",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("solr delete by query: %w", err)
	}
	defer resp.Body.Close()

	return c.checkResponse(resp)
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("solr returned %d: %s", resp.StatusCode, body)
}
