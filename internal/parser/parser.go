// Package parser extracts symbols from source code files.
package parser

// Symbol represents a named code element extracted from a source file.
type Symbol struct {
	Name      string   // Symbol name (e.g., "NewClient", "Document")
	Type      string   // function, method, type, struct, interface, const, var
	Signature string   // Full signature (e.g., "func NewClient(baseURL string) *Client")
	Body      string   // Full source code of the symbol
	DocString string   // Documentation comment
	LineStart int      // 1-based starting line
	LineEnd   int      // 1-based ending line
	Receiver  string   // Go method receiver (e.g., "*Client")
	Children  []Symbol // Nested symbols (e.g., methods on a struct)

	// Extracted relationships (populated by enhanced parsers)
	Calls            []string // Function/method calls: ["c.Get", "fmt.Errorf"]
	TypesUsed        []string // Types referenced: ["context.Context", "RepoConfig"]
	Params           []Param  // Parsed parameters
	Returns          []string // Return types
	Fields           []Field  // Struct fields (for structs)
	InterfaceMethods []string // Method signatures (for interfaces)
}

// Param represents a function/method parameter.
type Param struct {
	Name string
	Type string
}

// Field represents a struct field.
type Field struct {
	Name     string
	Type     string
	Tag      string
	Exported bool
}

// InterfaceHint records a var _ Interface = (*Type)(nil) pattern.
type InterfaceHint struct {
	Interface string // Interface name (possibly qualified)
	Type      string // Implementing type name
}

// FileInfo holds metadata about a parsed source file.
type FileInfo struct {
	PackageName    string            // Package/module name
	Imports        []string          // Import paths
	ImportAliases  map[string]string // alias → import path
	Symbols        []Symbol          // Top-level symbols
	InterfaceHints []InterfaceHint   // var _ Interface = (*Type)(nil) patterns
	TotalLines     int               // Total line count
}

// Parser extracts symbols and metadata from source code.
type Parser interface {
	// Parse extracts symbols from a source file.
	Parse(filePath string, content []byte) (*FileInfo, error)
	// Languages returns the languages this parser supports.
	Languages() []string
}
