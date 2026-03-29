package solr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
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
	transport := &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
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

// Add indexes one or more memory documents into Solr.
func (c *Client) Add(ctx context.Context, docs ...Document) error {
	return c.AddJSON(ctx, docs)
}

// AddCode indexes one or more code documents into Solr.
func (c *Client) AddCode(ctx context.Context, docs ...CodeDocument) error {
	return c.AddJSON(ctx, docs)
}

// AddJSON indexes any JSON-serializable slice of documents into Solr.
func (c *Client) AddJSON(ctx context.Context, docs any) error {
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

// Update performs an atomic update on a document by ID with retry and backoff.
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

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
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
			lastErr = fmt.Errorf("solr update: %w", err)
			continue
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			lastErr = err
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

// BulkUpdate performs atomic updates on multiple documents in a single request.
func (c *Client) BulkUpdate(ctx context.Context, updates []map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	body, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("marshal bulk update: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/update?commitWithin=5000",
			bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("solr bulk update: %w", err)
			continue
		}
		defer resp.Body.Close()

		if err := c.checkResponse(resp); err != nil {
			lastErr = err
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
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

// EnsureCollection creates the Solr collection if it doesn't already exist.
// It uses the configDir to upload the schema/config files.
// baseURL should be like "http://host:8983/solr/code" — the collection name is extracted from the path.
func (c *Client) EnsureCollection(ctx context.Context, configDir string) error {
	// Check if collection already exists via ping
	if err := c.Ping(ctx); err == nil {
		return nil // already exists
	}

	// Extract Solr base URL and collection name from baseURL
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	collection := path.Base(u.Path)
	solrBase := strings.TrimSuffix(c.baseURL, "/solr/"+collection)

	// Create collection using the Solr ConfigSet API + Collections API
	// First, try creating via the core admin API (standalone mode, not SolrCloud)
	createURL := fmt.Sprintf("%s/solr/admin/cores?action=CREATE&name=%s&instanceDir=%s&configSet=%s",
		solrBase, collection, collection, collection)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, createURL, nil)
	if err != nil {
		return fmt.Errorf("create collection request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("create collection %s failed (%d): %s", collection, resp.StatusCode, body)
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("solr returned %d: %s", resp.StatusCode, body)
}
