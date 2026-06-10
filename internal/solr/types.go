package solr

import "time"

// Document represents a memory stored in Solr.
type Document struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id,omitempty"`
	MemoryType string    `json:"memory_type,omitempty"`
	Content    string    `json:"content"`
	Title      string    `json:"title,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Source     string    `json:"source,omitempty"`
	Importance float64   `json:"importance,omitempty"`
	Metadata   string    `json:"metadata,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	ExpiresAt  string    `json:"expires_at,omitempty"`
	Lifetime   string    `json:"lifetime,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	RelatedIDs []string  `json:"related_ids,omitempty"`
	Format     string    `json:"format,omitempty"`
	// Embedding is the dense semantic vector. Omitted when embeddings are
	// disabled so the field never reaches Solr in lexical-only mode.
	Embedding []float32 `json:"embedding1024,omitempty"`
}

// QueryParams holds parameters for a Solr search query.
type QueryParams struct {
	Query         string
	FilterQueries []string
	Fields        []string
	Sort          string
	Start         int
	Rows          int
	Highlight     bool
	Facet         bool
	FacetFields   []string
	// MM overrides the edismax minimum-should-match for this query. Empty
	// leaves the request handler default (solrconfig) in place. Use "1" for
	// OR-style recall (any term), "100%" to require all terms.
	MM string
}

// QueryResponse holds the parsed response from a Solr search.
type QueryResponse struct {
	NumFound     int
	Start        int
	Docs         []map[string]any
	Highlighting map[string]map[string][]string
	Facets       map[string][]FacetCount
}

// FacetCount represents a single facet value and its count.
type FacetCount struct {
	Value string
	Count int
}
