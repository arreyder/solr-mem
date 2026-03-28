package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/arreyder/solr-mem/internal/parser"
)

// buildSymbolYAML generates pre-analyzed YAML content for a symbol document.
func buildSymbolYAML(sym parser.Symbol, relPath, pkgName string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("kind: %s\n", sym.Type))
	sb.WriteString(fmt.Sprintf("name: %s\n", sym.Name))
	if sym.Receiver != "" {
		sb.WriteString(fmt.Sprintf("receiver: %q\n", sym.Receiver))
	}
	if pkgName != "" {
		sb.WriteString(fmt.Sprintf("package: %s\n", pkgName))
	}
	sb.WriteString(fmt.Sprintf("file: %s\n", relPath))
	sb.WriteString(fmt.Sprintf("lines: %d-%d\n", sym.LineStart, sym.LineEnd))
	sb.WriteString(fmt.Sprintf("signature: %q\n", sym.Signature))

	if sym.DocString != "" {
		doc := strings.TrimSpace(sym.DocString)
		if len(doc) > 200 {
			doc = doc[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("doc: %q\n", doc))
	}

	// Parameters
	if len(sym.Params) > 0 {
		sb.WriteString("params:\n")
		for _, p := range sym.Params {
			if p.Name != "" {
				sb.WriteString(fmt.Sprintf("  - %s: %s\n", p.Name, p.Type))
			} else {
				sb.WriteString(fmt.Sprintf("  - %s\n", p.Type))
			}
		}
	}

	// Returns
	if len(sym.Returns) > 0 {
		sb.WriteString(fmt.Sprintf("returns: [%s]\n", strings.Join(sym.Returns, ", ")))
	}

	// Calls
	if len(sym.Calls) > 0 {
		calls := sym.Calls
		if len(calls) > 30 {
			calls = calls[:30]
		}
		sb.WriteString("calls:\n")
		for _, c := range calls {
			sb.WriteString(fmt.Sprintf("  - %s\n", c))
		}
		if len(sym.Calls) > 30 {
			sb.WriteString(fmt.Sprintf("  # ... and %d more\n", len(sym.Calls)-30))
		}
	}

	// Types used
	if len(sym.TypesUsed) > 0 {
		sb.WriteString(fmt.Sprintf("types_used: [%s]\n", strings.Join(sym.TypesUsed, ", ")))
	}

	// Struct fields
	if len(sym.Fields) > 0 {
		sb.WriteString("fields:\n")
		for _, f := range sym.Fields {
			exported := ""
			if f.Exported {
				exported = " (exported)"
			}
			sb.WriteString(fmt.Sprintf("  - %s: %s%s\n", f.Name, f.Type, exported))
		}
	}

	// Interface methods
	if len(sym.InterfaceMethods) > 0 {
		sb.WriteString("interface_methods:\n")
		for _, m := range sym.InterfaceMethods {
			sb.WriteString(fmt.Sprintf("  - %s\n", m))
		}
	}

	// Placeholders for cross-ref data (populated in pass 2)
	sb.WriteString("called_by: []\n")
	sb.WriteString("tested_by: []\n")

	return sb.String()
}

// buildFileYAML generates a richer file-level document with grouped symbols.
func buildFileYAML(relPath, lang string, info *parser.FileInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("file: %s\n", relPath))
	sb.WriteString(fmt.Sprintf("language: %s\n", lang))
	if info.PackageName != "" {
		sb.WriteString(fmt.Sprintf("package: %s\n", info.PackageName))
	}
	sb.WriteString(fmt.Sprintf("lines: %d\n", info.TotalLines))

	// Categorize imports
	if len(info.Imports) > 0 {
		var stdlib, external, internal []string
		for _, imp := range info.Imports {
			if !strings.Contains(imp, ".") {
				stdlib = append(stdlib, imp)
			} else if strings.Contains(imp, "gitlab.com/ductone") || strings.Contains(imp, "github.com/arreyder") {
				// Heuristic: known internal import paths
				// Take the last meaningful segment
				parts := strings.Split(imp, "/")
				if len(parts) > 2 {
					internal = append(internal, strings.Join(parts[len(parts)-2:], "/"))
				} else {
					internal = append(internal, imp)
				}
			} else {
				parts := strings.Split(imp, "/")
				if len(parts) > 2 {
					external = append(external, strings.Join(parts[len(parts)-2:], "/"))
				} else {
					external = append(external, imp)
				}
			}
		}
		sb.WriteString("imports:\n")
		if len(stdlib) > 0 {
			sb.WriteString(fmt.Sprintf("  stdlib: [%s]\n", strings.Join(truncateSlice(stdlib, 15), ", ")))
		}
		if len(internal) > 0 {
			sb.WriteString(fmt.Sprintf("  internal: [%s]\n", strings.Join(truncateSlice(internal, 15), ", ")))
		}
		if len(external) > 0 {
			sb.WriteString(fmt.Sprintf("  external: [%s]\n", strings.Join(truncateSlice(external, 10), ", ")))
		}
	}

	// Group symbols by role
	var types, exportedMethods, privateMethods, exportedFuncs, privateFuncs, consts, vars []string
	for _, sym := range info.Symbols {
		label := sym.Name
		if sym.Type == "method" || sym.Type == "function" {
			label = sym.Signature
			// Truncate very long signatures
			if len(label) > 100 {
				label = label[:100] + "..."
			}
		}

		switch sym.Type {
		case "struct", "interface", "type":
			detail := sym.Type
			if sym.Type == "struct" && len(sym.Fields) > 0 {
				detail = fmt.Sprintf("struct, %d fields", len(sym.Fields))
			}
			if sym.Type == "interface" && len(sym.InterfaceMethods) > 0 {
				detail = fmt.Sprintf("interface, %d methods", len(sym.InterfaceMethods))
			}
			types = append(types, fmt.Sprintf("%s (%s, L%d)", sym.Name, detail, sym.LineStart))
		case "method":
			if isExported(sym.Name) {
				exportedMethods = append(exportedMethods, fmt.Sprintf("%s (L%d)", sym.Name, sym.LineStart))
			} else {
				privateMethods = append(privateMethods, fmt.Sprintf("%s (L%d)", sym.Name, sym.LineStart))
			}
		case "function":
			if isExported(sym.Name) {
				exportedFuncs = append(exportedFuncs, fmt.Sprintf("%s (L%d)", sym.Name, sym.LineStart))
			} else {
				privateFuncs = append(privateFuncs, fmt.Sprintf("%s (L%d)", sym.Name, sym.LineStart))
			}
		case "const":
			consts = append(consts, sym.Name)
		case "var":
			vars = append(vars, sym.Name)
		}
	}

	sb.WriteString("symbols_by_role:\n")
	if len(types) > 0 {
		sb.WriteString("  types:\n")
		for _, t := range types {
			sb.WriteString(fmt.Sprintf("    - %s\n", t))
		}
	}
	if len(exportedFuncs) > 0 {
		sb.WriteString(fmt.Sprintf("  exported_functions: [%s]\n", strings.Join(truncateSlice(exportedFuncs, 20), ", ")))
	}
	if len(exportedMethods) > 0 {
		sb.WriteString(fmt.Sprintf("  exported_methods: [%s]\n", strings.Join(truncateSlice(exportedMethods, 20), ", ")))
	}
	if len(privateMethods) > 0 {
		sb.WriteString(fmt.Sprintf("  private_methods: [%s]\n", strings.Join(truncateSlice(privateMethods, 20), ", ")))
	}
	if len(privateFuncs) > 0 {
		sb.WriteString(fmt.Sprintf("  private_functions: [%s]\n", strings.Join(truncateSlice(privateFuncs, 15), ", ")))
	}
	if len(consts) > 0 {
		sb.WriteString(fmt.Sprintf("  constants: [%s]\n", strings.Join(truncateSlice(consts, 15), ", ")))
	}
	if len(vars) > 0 {
		sb.WriteString(fmt.Sprintf("  vars: [%s]\n", strings.Join(truncateSlice(vars, 10), ", ")))
	}

	// Interface hints
	if len(info.InterfaceHints) > 0 {
		sb.WriteString("implements:\n")
		for _, h := range info.InterfaceHints {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", h.Type, h.Interface))
		}
	}

	sb.WriteString("test_file: \"\"\n")

	return sb.String()
}

// buildPackageYAML generates an architecture-aware package document.
func buildPackageYAML(pkg *packageInfo, pkgDir string) string {
	var sb strings.Builder

	pkgName := pkg.name
	if pkgName == "" {
		pkgName = filepath.Base(pkgDir)
	}
	sb.WriteString(fmt.Sprintf("package: %s\n", pkgName))
	sb.WriteString(fmt.Sprintf("path: %s\n", pkgDir))
	sb.WriteString(fmt.Sprintf("files: [%s]\n", strings.Join(pkg.files, ", ")))

	// Group exported symbols by kind
	var types, functions, methods []string
	var interfaces []string
	for _, sym := range pkg.symbols {
		switch sym.Type {
		case "struct":
			detail := "struct"
			if len(sym.Fields) > 0 {
				detail = fmt.Sprintf("struct, %d fields", len(sym.Fields))
			}
			types = append(types, fmt.Sprintf("%s (%s)", sym.Name, detail))
		case "interface":
			detail := "interface"
			if len(sym.InterfaceMethods) > 0 {
				detail = fmt.Sprintf("interface, %d methods", len(sym.InterfaceMethods))
			}
			interfaces = append(interfaces, fmt.Sprintf("%s (%s)", sym.Name, detail))
		case "type":
			types = append(types, sym.Name)
		case "function":
			functions = append(functions, sym.Name)
		case "method":
			if sym.Receiver != "" {
				methods = append(methods, fmt.Sprintf("%s.%s", strings.TrimPrefix(sym.Receiver, "*"), sym.Name))
			} else {
				methods = append(methods, sym.Name)
			}
		}
	}

	if len(types) > 0 {
		sb.WriteString("key_types:\n")
		for _, t := range types {
			sb.WriteString(fmt.Sprintf("  - %s\n", t))
		}
	}
	if len(interfaces) > 0 {
		sb.WriteString("interfaces:\n")
		for _, i := range interfaces {
			sb.WriteString(fmt.Sprintf("  - %s\n", i))
		}
	}
	if len(functions) > 0 {
		sb.WriteString(fmt.Sprintf("entry_points: [%s]\n", strings.Join(truncateSlice(functions, 15), ", ")))
	}
	if len(methods) > 0 {
		sb.WriteString(fmt.Sprintf("exported_methods: [%s]\n", strings.Join(truncateSlice(methods, 20), ", ")))
	}

	// Interface satisfaction hints
	if len(pkg.interfaceHints) > 0 {
		sb.WriteString("implementations:\n")
		for _, h := range pkg.interfaceHints {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", h.typeName, h.ifaceName))
		}
	}

	if pkg.isVendor {
		sb.WriteString("source: vendor\n")
	}

	return sb.String()
}

// buildRepoYAML generates a repository-level overview document.
func buildRepoYAML(repoPath, commitSHA, defaultBranch string, packages map[string]*packageInfo, files []string) string {
	var sb strings.Builder

	langCounts := make(map[string]int)
	for _, f := range files {
		lang := parser.Language(f)
		langCounts[lang]++
	}
	var languages []string
	for lang := range langCounts {
		languages = append(languages, lang)
	}

	var dirs []string
	dirSet := make(map[string]bool)
	for pkgDir := range packages {
		topDir := strings.Split(pkgDir, string(filepath.Separator))[0]
		if !dirSet[topDir] {
			dirSet[topDir] = true
			dirs = append(dirs, topDir)
		}
	}

	sb.WriteString(fmt.Sprintf("repo: %s\n", repoPath))
	sb.WriteString(fmt.Sprintf("languages: [%s]\n", strings.Join(languages, ", ")))
	sb.WriteString(fmt.Sprintf("default_branch: %s\n", defaultBranch))
	sb.WriteString(fmt.Sprintf("last_indexed_commit: %s\n", commitSHA))
	sb.WriteString(fmt.Sprintf("top_level_dirs: [%s]\n", strings.Join(dirs, ", ")))
	sb.WriteString(fmt.Sprintf("total_files: %d\n", len(files)))
	sb.WriteString(fmt.Sprintf("total_packages: %d\n", len(packages)))

	// Count symbol types
	var totalStructs, totalInterfaces, totalFunctions, totalMethods int
	for _, pkg := range packages {
		for _, sym := range pkg.symbols {
			switch sym.Type {
			case "struct":
				totalStructs++
			case "interface":
				totalInterfaces++
			case "function":
				totalFunctions++
			case "method":
				totalMethods++
			}
		}
	}
	sb.WriteString("stats:\n")
	sb.WriteString(fmt.Sprintf("  structs: %d\n", totalStructs))
	sb.WriteString(fmt.Sprintf("  interfaces: %d\n", totalInterfaces))
	sb.WriteString(fmt.Sprintf("  functions: %d\n", totalFunctions))
	sb.WriteString(fmt.Sprintf("  methods: %d\n", totalMethods))

	return sb.String()
}

func truncateSlice(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	result := make([]string, max)
	copy(result, s[:max])
	result = append(result, fmt.Sprintf("...+%d more", len(s)-max))
	return result
}
