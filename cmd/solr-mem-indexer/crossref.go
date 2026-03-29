package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/parser"
	"github.com/arreyder/solr-mem/internal/solr"
)

// CrossRefIndex holds in-memory cross-reference data built during pass 1.
type CrossRefIndex struct {
	// qualified name → doc ID
	SymbolIDs map[string]string

	// qualified name → list of caller qualified names
	CalledBy map[string][]string

	// source file (relPath) → test file (relPath)
	TestFiles map[string]string

	// type name → interfaces it implements (from var _ hints)
	Implements map[string][]string

	// interface name → types implementing it
	ImplementedBy map[string][]string

	// receiver type → method names
	MethodSets map[string][]string

	// doc ID → qualified name (reverse lookup)
	IDToQualName map[string]string

	// qualified name → symbol calls list
	SymbolCalls map[string][]string
}

// NewCrossRefIndex creates an empty cross-reference index.
func NewCrossRefIndex() *CrossRefIndex {
	return &CrossRefIndex{
		SymbolIDs:     make(map[string]string),
		CalledBy:      make(map[string][]string),
		TestFiles:     make(map[string]string),
		Implements:    make(map[string][]string),
		ImplementedBy: make(map[string][]string),
		MethodSets:    make(map[string][]string),
		IDToQualName:  make(map[string]string),
		SymbolCalls:   make(map[string][]string),
	}
}

// RegisterSymbol records a symbol in the cross-reference index during pass 1.
func (cr *CrossRefIndex) RegisterSymbol(docID, qualifiedName string, sym parser.Symbol) {
	cr.SymbolIDs[qualifiedName] = docID
	cr.IDToQualName[docID] = qualifiedName

	if len(sym.Calls) > 0 {
		cr.SymbolCalls[qualifiedName] = sym.Calls
	}

	// Track method sets
	if sym.Receiver != "" {
		recvType := strings.TrimPrefix(sym.Receiver, "*")
		cr.MethodSets[recvType] = append(cr.MethodSets[recvType], sym.Name)
	}
}

// RegisterInterfaceHints records var _ Interface = (*Type)(nil) patterns.
func (cr *CrossRefIndex) RegisterInterfaceHints(hints []parser.InterfaceHint) {
	for _, h := range hints {
		typeName := strings.TrimPrefix(h.Type, "*")
		cr.Implements[typeName] = append(cr.Implements[typeName], h.Interface)
		cr.ImplementedBy[h.Interface] = append(cr.ImplementedBy[h.Interface], typeName)
	}
}

// RegisterTestFile links a source file to its test file.
func (cr *CrossRefIndex) RegisterTestFile(sourceFile, testFile string) {
	cr.TestFiles[sourceFile] = testFile
}

// Resolve performs pass 2: resolves called_by relationships by inverting the calls graph.
func (cr *CrossRefIndex) Resolve() {
	for callerQN, calls := range cr.SymbolCalls {
		for _, callTarget := range calls {
			// Try to match the call target to a registered symbol.
			// Calls come in forms like:
			//   "c.Get" (receiver method)
			//   "fmt.Errorf" (package function)
			//   "localFunc" (same-package function)
			//
			// We try to match against qualified names in the index.
			// This is heuristic — we try multiple resolution strategies.

			matched := cr.resolveCallTarget(callTarget, callerQN)
			if matched != "" {
				cr.CalledBy[matched] = append(cr.CalledBy[matched], callerQN)
			}
		}
	}
}

// resolveCallTarget tries to find the qualified name of a call target.
func (cr *CrossRefIndex) resolveCallTarget(callTarget, callerQN string) string {
	// Direct match (unlikely but possible for same-package calls)
	if _, ok := cr.SymbolIDs[callTarget]; ok {
		return callTarget
	}

	// Extract caller's package for context
	callerPkg := ""
	parts := strings.Split(callerQN, ".")
	if len(parts) >= 2 {
		callerPkg = parts[0]
	}

	// For "receiver.Method" calls, try matching against method sets
	callParts := strings.Split(callTarget, ".")
	if len(callParts) == 2 {
		methodName := callParts[1]
		receiverVar := callParts[0]

		// Strategy 1: The receiver variable matches a known type (e.g., "c" is often the receiver)
		// Check if callerPkg.ReceiverType.MethodName exists for any receiver type
		for recvType, methods := range cr.MethodSets {
			for _, m := range methods {
				if m == methodName {
					// Found a matching method — check if it's in the same package
					candidate := callerPkg + "." + recvType + "." + methodName
					if _, ok := cr.SymbolIDs[candidate]; ok {
						return candidate
					}
					// Also try without package prefix
					candidate = recvType + "." + methodName
					if _, ok := cr.SymbolIDs[candidate]; ok {
						return candidate
					}
				}
			}
		}

		// Strategy 2: receiverVar might be a package alias (e.g., "fmt.Errorf")
		// Try callerPkg.receiverVar.methodName and receiverVar.methodName
		candidate := receiverVar + "." + methodName
		if _, ok := cr.SymbolIDs[candidate]; ok {
			return candidate
		}
		candidate = callerPkg + "." + receiverVar + "." + methodName
		if _, ok := cr.SymbolIDs[candidate]; ok {
			return candidate
		}
	}

	// For simple names, try callerPkg.Name
	if len(callParts) == 1 && callerPkg != "" {
		candidate := callerPkg + "." + callTarget
		if _, ok := cr.SymbolIDs[candidate]; ok {
			return candidate
		}
	}

	return "" // unresolved
}

const bulkBatchSize = 100

// ApplyUpdates performs batched Solr updates to populate cross-reference fields.
func (cr *CrossRefIndex) ApplyUpdates(ctx context.Context, client *solr.Client) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var batch []map[string]any
	updates := 0
	errors := 0

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := client.BulkUpdate(ctx, batch); err != nil {
			log.Printf("Warning: bulk update failed (%d docs): %v", len(batch), err)
			errors += len(batch)
		} else {
			updates += len(batch)
		}
		batch = batch[:0]
	}

	enqueue := func(id string, fields map[string]any) {
		doc := map[string]any{"id": id}
		for k, v := range fields {
			doc[k] = map[string]any{"set": v}
		}
		batch = append(batch, doc)
		if len(batch) >= bulkBatchSize {
			flush()
		}
	}

	// Update called_by on target symbols
	for targetQN, callers := range cr.CalledBy {
		docID, ok := cr.SymbolIDs[targetQN]
		if !ok {
			continue
		}
		seen := make(map[string]bool)
		var unique []string
		for _, c := range callers {
			if !seen[c] {
				seen[c] = true
				unique = append(unique, c)
			}
		}
		enqueue(docID, map[string]any{"called_by": unique, "updated_at": now})
	}

	// Update implements/implemented_by
	for typeName, ifaces := range cr.Implements {
		for qn, docID := range cr.SymbolIDs {
			if strings.HasSuffix(qn, "."+typeName) {
				enqueue(docID, map[string]any{"implements": ifaces, "updated_at": now})
				break
			}
		}
	}

	for ifaceName, types := range cr.ImplementedBy {
		for qn, docID := range cr.SymbolIDs {
			if strings.HasSuffix(qn, "."+ifaceName) {
				enqueue(docID, map[string]any{"implemented_by": types, "updated_at": now})
				break
			}
		}
	}

	// Update method sets on struct docs
	for recvType, methods := range cr.MethodSets {
		for qn, docID := range cr.SymbolIDs {
			if strings.HasSuffix(qn, "."+recvType) {
				enqueue(docID, map[string]any{"methods": methods, "updated_at": now})
				break
			}
		}
	}

	flush()

	log.Printf("Cross-reference pass: %d updates applied, %d errors (%d called_by, %d implements, %d method_sets)",
		updates, errors, len(cr.CalledBy), len(cr.Implements), len(cr.MethodSets))

	return nil
}

// DetectTestFiles scans the files list to link source files with test files.
func (cr *CrossRefIndex) DetectTestFiles(files []string, repoPath string) {
	testSet := make(map[string]string) // dir+base -> full relpath

	for _, f := range files {
		relPath, _ := filepath.Rel(repoPath, f)
		base := filepath.Base(relPath)
		dir := filepath.Dir(relPath)
		if strings.HasSuffix(base, "_test.go") {
			// Store without _test.go suffix for matching
			srcBase := strings.TrimSuffix(base, "_test.go") + ".go"
			testSet[filepath.Join(dir, srcBase)] = relPath
		}
	}

	for srcFile, testFile := range testSet {
		cr.TestFiles[srcFile] = testFile
	}
}

// Stats returns a summary of the cross-reference index.
func (cr *CrossRefIndex) Stats() string {
	totalCalledBy := 0
	for _, v := range cr.CalledBy {
		totalCalledBy += len(v)
	}
	return fmt.Sprintf("symbols=%d, called_by_edges=%d, implements=%d, method_sets=%d, test_links=%d",
		len(cr.SymbolIDs), totalCalledBy, len(cr.Implements), len(cr.MethodSets), len(cr.TestFiles))
}
