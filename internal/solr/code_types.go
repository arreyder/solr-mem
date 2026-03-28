package solr

import "time"

// CodeDocument represents a code artifact stored in the Solr code collection.
type CodeDocument struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Title     string    `json:"title,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	Metadata  string    `json:"metadata,omitempty"`
	Format    string    `json:"format,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Repository
	RepoURL string `json:"repo_url,omitempty"`
	RepoID  string `json:"repo_id,omitempty"`

	// Hierarchy
	DocLevel   string   `json:"doc_level"`
	ParentID   string   `json:"parent_id,omitempty"`
	RelatedIDs []string `json:"related_ids,omitempty"`
	Importance float64  `json:"importance,omitempty"`

	// File
	FilePath  string `json:"file_path,omitempty"`
	Language  string `json:"language,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`

	// Symbol
	SymbolName      string `json:"symbol_name,omitempty"`
	SymbolNameExact string `json:"symbol_name_exact,omitempty"`
	SymbolType      string `json:"symbol_type,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	LineStart       int    `json:"line_start,omitempty"`
	LineEnd         int    `json:"line_end,omitempty"`

	// Enrichment: pre-analyzed relationships
	SourceCode    string   `json:"source_code,omitempty"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	ReceiverType  string   `json:"receiver_type,omitempty"`
	Calls         []string `json:"calls,omitempty"`
	CalledBy      []string `json:"called_by,omitempty"`
	TypesUsed     []string `json:"types_used,omitempty"`
	Implements    []string `json:"implements,omitempty"`
	ImplementedBy []string `json:"implemented_by,omitempty"`
	Methods       []string `json:"methods,omitempty"`
	TestFile      string   `json:"test_file,omitempty"`
	TestedBy      []string `json:"tested_by,omitempty"`
}
