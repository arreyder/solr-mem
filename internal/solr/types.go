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
	SessionID   string    `json:"session_id,omitempty"`
	RelatedIDs  []string  `json:"related_ids,omitempty"`
	Format      string    `json:"format,omitempty"`
	ContentHash string    `json:"content_hash,omitempty"`
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
