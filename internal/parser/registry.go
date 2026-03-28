package parser

import (
	"path/filepath"
	"strings"
)

// Registry maps file extensions to parsers.
type Registry struct {
	parsers  map[string]Parser // extension -> parser
	fallback Parser
}

// NewRegistry creates a registry with the Go parser and a heuristic fallback.
func NewRegistry() *Registry {
	r := &Registry{
		parsers:  make(map[string]Parser),
		fallback: &HeuristicParser{},
	}
	r.Register(&GoParser{})
	return r
}

// Register adds a parser for its supported languages/extensions.
func (r *Registry) Register(p Parser) {
	for _, lang := range p.Languages() {
		r.parsers[lang] = p
	}
}

// ForFile returns the appropriate parser for a file path.
func (r *Registry) ForFile(path string) Parser {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if p, ok := r.parsers[ext]; ok {
		return p
	}
	return r.fallback
}

// Language returns the language identifier for a file extension.
func Language(path string) string {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	switch ext {
	case "go":
		return "go"
	case "py":
		return "python"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "tsx":
		return "typescript"
	case "jsx":
		return "javascript"
	case "rs":
		return "rust"
	case "java":
		return "java"
	case "rb":
		return "ruby"
	case "c", "h":
		return "c"
	case "cpp", "cc", "cxx", "hpp":
		return "cpp"
	case "cs":
		return "csharp"
	case "sh", "bash":
		return "shell"
	case "yaml", "yml":
		return "yaml"
	case "json":
		return "json"
	case "md":
		return "markdown"
	case "sql":
		return "sql"
	default:
		return ext
	}
}

// IsSourceFile returns true if the file extension indicates a source code file worth parsing.
func IsSourceFile(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	switch ext {
	case "go", "py", "js", "ts", "tsx", "jsx", "rs", "java", "rb",
		"c", "h", "cpp", "cc", "cxx", "hpp", "cs", "sh", "bash",
		"yaml", "yml", "json", "toml", "xml", "sql", "proto",
		"tf", "hcl", "lua", "zig", "swift", "kt", "scala",
		"ex", "exs", "erl", "hs", "ml", "clj", "r", "R",
		"php", "pl", "pm":
		return true
	}
	return false
}
