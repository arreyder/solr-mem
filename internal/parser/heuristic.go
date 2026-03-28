package parser

import (
	"regexp"
	"strings"
)

// HeuristicParser is a fallback parser that uses regex patterns to extract symbols.
type HeuristicParser struct{}

func (p *HeuristicParser) Languages() []string {
	return nil // fallback, not registered for specific languages
}

// Common function/class declaration patterns across languages.
var patterns = []struct {
	re       *regexp.Regexp
	symType  string
	nameIdx  int
	sigGroup int // capture group for full signature, 0 = whole match
}{
	// Python: def, class, async def
	{regexp.MustCompile(`^(class\s+(\w+).*?:)`), "class", 2, 1},
	{regexp.MustCompile(`^(async\s+def\s+(\w+)\s*\(.*?\).*?:)`), "function", 2, 1},
	{regexp.MustCompile(`^(def\s+(\w+)\s*\(.*?\).*?:)`), "function", 2, 1},

	// JavaScript/TypeScript: function, class, const/let arrow functions, export
	{regexp.MustCompile(`^(export\s+)?(class\s+(\w+))`), "class", 3, 0},
	{regexp.MustCompile(`^(export\s+)?(async\s+)?function\s+(\w+)\s*\(`), "function", 3, 0},
	{regexp.MustCompile(`^(export\s+)?(const|let|var)\s+(\w+)\s*=\s*(async\s+)?\(`), "function", 3, 0},
	{regexp.MustCompile(`^(export\s+)?(const|let|var)\s+(\w+)\s*=\s*(async\s+)?\w*\s*=>`), "function", 3, 0},

	// Rust: fn, struct, impl, trait, enum
	{regexp.MustCompile(`^(pub\s+)?(struct\s+(\w+))`), "struct", 3, 0},
	{regexp.MustCompile(`^(pub\s+)?(enum\s+(\w+))`), "type", 3, 0},
	{regexp.MustCompile(`^(pub\s+)?(trait\s+(\w+))`), "interface", 3, 0},
	{regexp.MustCompile(`^impl\s+(\w+)`), "type", 1, 0},
	{regexp.MustCompile(`^(pub\s+)?(async\s+)?(fn\s+(\w+)\s*\()`), "function", 4, 0},

	// Java/C#: class, interface, method patterns
	{regexp.MustCompile(`^(public|private|protected)?\s*(static\s+)?(class|interface)\s+(\w+)`), "class", 4, 0},

	// Ruby: def, class, module
	{regexp.MustCompile(`^(class\s+(\w+))`), "class", 2, 1},
	{regexp.MustCompile(`^(module\s+(\w+))`), "type", 2, 1},
	{regexp.MustCompile(`^(def\s+(\w+))`), "function", 2, 1},
}

func (p *HeuristicParser) Parse(filePath string, content []byte) (*FileInfo, error) {
	lines := strings.Split(string(content), "\n")
	info := &FileInfo{}

	var currentSymbol *Symbol
	var braceDepth int

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lineNum := i + 1

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			if currentSymbol != nil {
				braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			}
			continue
		}

		// Try each pattern
		for _, pat := range patterns {
			match := pat.re.FindStringSubmatch(trimmed)
			if match == nil {
				continue
			}

			// Close previous symbol
			if currentSymbol != nil {
				currentSymbol.LineEnd = lineNum - 1
				currentSymbol.Body = extractLines(lines, currentSymbol.LineStart, currentSymbol.LineEnd)
				info.Symbols = append(info.Symbols, *currentSymbol)
			}

			name := match[pat.nameIdx]
			sig := trimmed
			if pat.sigGroup > 0 && pat.sigGroup < len(match) {
				sig = match[pat.sigGroup]
			}

			currentSymbol = &Symbol{
				Name:      name,
				Type:      pat.symType,
				Signature: sig,
				LineStart: lineNum,
			}
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			break
		}

		if currentSymbol != nil {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		}
	}

	// Close last symbol
	if currentSymbol != nil {
		currentSymbol.LineEnd = len(lines)
		currentSymbol.Body = extractLines(lines, currentSymbol.LineStart, currentSymbol.LineEnd)
		info.Symbols = append(info.Symbols, *currentSymbol)
	}

	return info, nil
}
