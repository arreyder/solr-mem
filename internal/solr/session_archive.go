package solr

import "time"

// SessionArchiveDocument is an immutable raw OMP session chunk. It belongs to
// the dedicated omp_sessions core and is deliberately not embedded.
type SessionArchiveDocument struct {
	ID            string    `json:"id"`
	ArchiveID     string    `json:"archive_id"`
	SessionID     string    `json:"session_id"`
	EventType     string    `json:"event_type"`
	ChunkIndex    int       `json:"chunk_index,omitempty"`
	ChunkCount    int       `json:"chunk_count,omitempty"`
	ContentSHA256 string    `json:"content_sha256,omitempty"`
	Content       string    `json:"content,omitempty"`
	Host          string    `json:"host,omitempty"`
	CWD           string    `json:"cwd,omitempty"`
	RepoOrigin    string    `json:"repo_origin,omitempty"`
	GitHead       string    `json:"git_head,omitempty"`
	EventAt       time.Time `json:"event_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Metadata      string    `json:"metadata,omitempty"`
}
