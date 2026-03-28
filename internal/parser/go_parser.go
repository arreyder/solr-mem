package parser

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strings"
)

// GoParser extracts symbols from Go source files using go/parser.
type GoParser struct{}

func (p *GoParser) Languages() []string {
	return []string{"go"}
}

func (p *GoParser) Parse(filePath string, content []byte) (*FileInfo, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, filePath, content, goparser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	lines := strings.Split(string(content), "\n")

	info := &FileInfo{
		PackageName:   file.Name.Name,
		ImportAliases: make(map[string]string),
		TotalLines:    len(lines),
	}

	// Extract imports with aliases
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		info.Imports = append(info.Imports, path)
		if imp.Name != nil {
			info.ImportAliases[imp.Name.Name] = path
		}
	}

	// Extract top-level declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := p.extractFunc(fset, d, lines)
			info.Symbols = append(info.Symbols, sym)

		case *ast.GenDecl:
			syms := p.extractGenDecl(fset, d, lines)
			info.Symbols = append(info.Symbols, syms...)

			// Check for interface satisfaction hints: var _ Interface = (*Type)(nil)
			if d.Tok == token.VAR {
				hints := p.extractInterfaceHints(d)
				info.InterfaceHints = append(info.InterfaceHints, hints...)
			}
		}
	}

	return info, nil
}

func (p *GoParser) extractFunc(fset *token.FileSet, fn *ast.FuncDecl, lines []string) Symbol {
	startLine := fset.Position(fn.Pos()).Line
	endLine := fset.Position(fn.End()).Line

	sym := Symbol{
		Name:      fn.Name.Name,
		Type:      "function",
		Signature: p.funcSignature(fn),
		Body:      extractLines(lines, startLine, endLine),
		LineStart: startLine,
		LineEnd:   endLine,
	}

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		sym.Type = "method"
		sym.Receiver = exprString(fn.Recv.List[0].Type)
	}

	if fn.Doc != nil {
		sym.DocString = fn.Doc.Text()
	}

	// Extract parameters
	sym.Params = extractParams(fn.Type.Params)

	// Extract return types
	sym.Returns = extractReturnTypes(fn.Type.Results)

	// Extract function calls from body
	if fn.Body != nil {
		sym.Calls = extractCalls(fn.Body)
	}

	// Extract types used in signature
	sym.TypesUsed = extractTypesFromSignature(fn.Type)

	return sym
}

func (p *GoParser) extractGenDecl(fset *token.FileSet, gd *ast.GenDecl, lines []string) []Symbol {
	var syms []Symbol

	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			startLine := fset.Position(gd.Pos()).Line
			endLine := fset.Position(gd.End()).Line

			sym := Symbol{
				Name:      s.Name.Name,
				LineStart: startLine,
				LineEnd:   endLine,
				Body:      extractLines(lines, startLine, endLine),
			}

			switch st := s.Type.(type) {
			case *ast.StructType:
				sym.Type = "struct"
				sym.Signature = fmt.Sprintf("type %s struct", s.Name.Name)
				sym.Fields = extractStructFields(st)
			case *ast.InterfaceType:
				sym.Type = "interface"
				sym.Signature = fmt.Sprintf("type %s interface", s.Name.Name)
				sym.InterfaceMethods = extractInterfaceMethods(st)
			default:
				sym.Type = "type"
				sym.Signature = fmt.Sprintf("type %s %s", s.Name.Name, exprString(s.Type))
			}

			if gd.Doc != nil {
				sym.DocString = gd.Doc.Text()
			}

			syms = append(syms, sym)

		case *ast.ValueSpec:
			startLine := fset.Position(s.Pos()).Line
			endLine := fset.Position(s.End()).Line

			for _, name := range s.Names {
				sym := Symbol{
					Name:      name.Name,
					LineStart: startLine,
					LineEnd:   endLine,
					Body:      extractLines(lines, startLine, endLine),
				}

				switch gd.Tok {
				case token.CONST:
					sym.Type = "const"
					sym.Signature = fmt.Sprintf("const %s", name.Name)
				case token.VAR:
					sym.Type = "var"
					sym.Signature = fmt.Sprintf("var %s", name.Name)
				}

				if gd.Doc != nil {
					sym.DocString = gd.Doc.Text()
				}

				syms = append(syms, sym)
			}
		}
	}

	return syms
}

// extractCalls walks a function body and collects all function/method calls.
func extractCalls(body *ast.BlockStmt) []string {
	seen := make(map[string]bool)
	var calls []string

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var name string
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			// receiver.Method() or pkg.Function()
			name = selectorCallName(fn)
		case *ast.Ident:
			// Local function or builtin
			if !isBuiltin(fn.Name) {
				name = fn.Name
			}
		}

		if name != "" && !seen[name] {
			seen[name] = true
			calls = append(calls, name)
		}
		return true
	})

	return calls
}

func selectorCallName(sel *ast.SelectorExpr) string {
	switch x := sel.X.(type) {
	case *ast.Ident:
		return x.Name + "." + sel.Sel.Name
	case *ast.SelectorExpr:
		// a.b.Method() — take last two segments
		if ident, ok := x.X.(*ast.Ident); ok {
			return ident.Name + "." + x.Sel.Name + "." + sel.Sel.Name
		}
		return x.Sel.Name + "." + sel.Sel.Name
	case *ast.CallExpr:
		// chained: foo().Bar() — just record .Bar
		return sel.Sel.Name
	}
	return sel.Sel.Name
}

// extractParams parses function parameters.
func extractParams(fields *ast.FieldList) []Param {
	if fields == nil {
		return nil
	}
	var params []Param
	for _, field := range fields.List {
		typeStr := exprString(field.Type)
		if len(field.Names) == 0 {
			params = append(params, Param{Type: typeStr})
		} else {
			for _, name := range field.Names {
				params = append(params, Param{Name: name.Name, Type: typeStr})
			}
		}
	}
	return params
}

// extractReturnTypes gets return type strings from a function signature.
func extractReturnTypes(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var types []string
	for _, field := range fields.List {
		typeStr := exprString(field.Type)
		if len(field.Names) == 0 {
			types = append(types, typeStr)
		} else {
			for range field.Names {
				types = append(types, typeStr)
			}
		}
	}
	return types
}

// extractTypesFromSignature collects non-builtin type references from a function signature.
func extractTypesFromSignature(ft *ast.FuncType) []string {
	seen := make(map[string]bool)
	var types []string

	collectTypes := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, t := range extractTypeRefs(field.Type) {
				if !seen[t] && !isBuiltinType(t) {
					seen[t] = true
					types = append(types, t)
				}
			}
		}
	}

	collectTypes(ft.Params)
	collectTypes(ft.Results)
	return types
}

// extractTypeRefs pulls type names from an AST type expression.
func extractTypeRefs(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		return []string{exprString(e)}
	case *ast.StarExpr:
		return extractTypeRefs(e.X)
	case *ast.ArrayType:
		return extractTypeRefs(e.Elt)
	case *ast.MapType:
		var refs []string
		refs = append(refs, extractTypeRefs(e.Key)...)
		refs = append(refs, extractTypeRefs(e.Value)...)
		return refs
	case *ast.Ellipsis:
		return extractTypeRefs(e.Elt)
	case *ast.FuncType:
		var refs []string
		if e.Params != nil {
			for _, f := range e.Params.List {
				refs = append(refs, extractTypeRefs(f.Type)...)
			}
		}
		if e.Results != nil {
			for _, f := range e.Results.List {
				refs = append(refs, extractTypeRefs(f.Type)...)
			}
		}
		return refs
	case *ast.ChanType:
		return extractTypeRefs(e.Value)
	}
	return nil
}

// extractStructFields gets field definitions from a struct type.
func extractStructFields(st *ast.StructType) []Field {
	if st.Fields == nil {
		return nil
	}
	var fields []Field
	for _, f := range st.Fields.List {
		typeStr := exprString(f.Type)
		tag := ""
		if f.Tag != nil {
			tag = strings.Trim(f.Tag.Value, "`")
		}

		if len(f.Names) == 0 {
			// Embedded field
			fields = append(fields, Field{
				Name:     typeStr,
				Type:     typeStr,
				Tag:      tag,
				Exported: isExportedName(typeStr),
			})
		} else {
			for _, name := range f.Names {
				fields = append(fields, Field{
					Name:     name.Name,
					Type:     typeStr,
					Tag:      tag,
					Exported: name.IsExported(),
				})
			}
		}
	}
	return fields
}

// extractInterfaceMethods gets method signatures from an interface type.
func extractInterfaceMethods(it *ast.InterfaceType) []string {
	if it.Methods == nil {
		return nil
	}
	var methods []string
	for _, m := range it.Methods.List {
		switch t := m.Type.(type) {
		case *ast.FuncType:
			if len(m.Names) > 0 {
				sig := m.Names[0].Name + "(" + fieldListString(t.Params) + ")"
				if t.Results != nil && len(t.Results.List) > 0 {
					sig += " " + fieldListString(t.Results)
				}
				methods = append(methods, sig)
			}
		case *ast.Ident:
			// Embedded interface
			methods = append(methods, t.Name)
		case *ast.SelectorExpr:
			methods = append(methods, exprString(t))
		}
	}
	return methods
}

func fieldListString(fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		parts = append(parts, exprString(f.Type))
	}
	return strings.Join(parts, ", ")
}

// extractInterfaceHints finds var _ Interface = (*Type)(nil) patterns.
func (p *GoParser) extractInterfaceHints(gd *ast.GenDecl) []InterfaceHint {
	var hints []InterfaceHint
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		// Check for blank identifier
		if len(vs.Names) != 1 || vs.Names[0].Name != "_" {
			continue
		}
		// Get the interface type
		ifaceName := ""
		if vs.Type != nil {
			ifaceName = exprString(vs.Type)
		}
		if ifaceName == "" {
			continue
		}
		// Get the implementing type from the value: (*Type)(nil)
		if len(vs.Values) != 1 {
			continue
		}
		typeName := extractTypeFromNilCast(vs.Values[0])
		if typeName != "" {
			hints = append(hints, InterfaceHint{
				Interface: ifaceName,
				Type:      typeName,
			})
		}
	}
	return hints
}

// extractTypeFromNilCast extracts the type from (*Type)(nil) or Type(nil) expressions.
func extractTypeFromNilCast(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	// Check that the argument is nil
	if len(call.Args) != 1 {
		return ""
	}
	if ident, ok := call.Args[0].(*ast.Ident); !ok || ident.Name != "nil" {
		return ""
	}
	// Extract the type being cast
	switch fn := call.Fun.(type) {
	case *ast.ParenExpr:
		return exprString(fn.X)
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return exprString(fn)
	}
	return ""
}

var builtins = map[string]bool{
	"make": true, "append": true, "len": true, "cap": true,
	"new": true, "delete": true, "close": true, "copy": true,
	"panic": true, "recover": true, "print": true, "println": true,
	"complex": true, "real": true, "imag": true, "clear": true,
	"min": true, "max": true,
}

func isBuiltin(name string) bool {
	return builtins[name]
}

var builtinTypes = map[string]bool{
	"string": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
	"bool": true, "byte": true, "rune": true, "uintptr": true, "error": true, "any": true,
}

func isBuiltinType(name string) bool {
	return builtinTypes[name]
}

func isExportedName(name string) bool {
	// Handle pointer and package-qualified names
	name = strings.TrimPrefix(name, "*")
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func (p *GoParser) funcSignature(fn *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		sb.WriteString("(")
		recv := fn.Recv.List[0]
		if len(recv.Names) > 0 {
			sb.WriteString(recv.Names[0].Name)
			sb.WriteString(" ")
		}
		sb.WriteString(exprString(recv.Type))
		sb.WriteString(") ")
	}

	sb.WriteString(fn.Name.Name)
	sb.WriteString("(")

	if fn.Type.Params != nil {
		var params []string
		for _, field := range fn.Type.Params.List {
			typeStr := exprString(field.Type)
			if len(field.Names) == 0 {
				params = append(params, typeStr)
			} else {
				for _, name := range field.Names {
					params = append(params, name.Name+" "+typeStr)
				}
			}
		}
		sb.WriteString(strings.Join(params, ", "))
	}
	sb.WriteString(")")

	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		results := fn.Type.Results.List
		if len(results) == 1 && len(results[0].Names) == 0 {
			sb.WriteString(" ")
			sb.WriteString(exprString(results[0].Type))
		} else {
			sb.WriteString(" (")
			var rets []string
			for _, field := range results {
				typeStr := exprString(field.Type)
				if len(field.Names) == 0 {
					rets = append(rets, typeStr)
				} else {
					for _, name := range field.Names {
						rets = append(rets, name.Name+" "+typeStr)
					}
				}
			}
			sb.WriteString(strings.Join(rets, ", "))
			sb.WriteString(")")
		}
	}

	return sb.String()
}

func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(e.Elt)
	case *ast.MapType:
		return "map[" + exprString(e.Key) + "]" + exprString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprString(e.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + exprString(e.Value)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func extractLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
