package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func codeContextTool(ctx context.Context, args map[string]any) (any, error) {
	filePath := getString(args, "file_path")
	if filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	line := getInt(args, "line", 0)
	depth := getInt(args, "depth", 1)
	if depth > 3 {
		depth = 3
	}

	var sb strings.Builder
	var allDocs []map[string]any

	// Build filter queries
	var fqs []string
	if v := getString(args, "repo_url"); v != "" {
		fqs = append(fqs, fmt.Sprintf("repo_url:%q", v))
	}

	// 1. Get the file document
	fileParams := solr.QueryParams{
		Query:         "*:*",
		FilterQueries: append(fqs, fmt.Sprintf("file_path:%q", filePath), `doc_level:"file"`),
		Rows:          1,
		Highlight:     false,
	}

	fileResp, err := codeClient.Query(ctx, fileParams)
	if err != nil {
		return nil, fmt.Errorf("file lookup failed: %w", err)
	}

	if len(fileResp.Docs) == 0 {
		return fmt.Sprintf("No indexed file found at path %q", filePath), nil
	}

	fileDoc := fileResp.Docs[0]
	fileID, _ := fileDoc["id"].(string)
	fileTitle, _ := fileDoc["title"].(string)
	fileContent, _ := fileDoc["content"].(string)

	sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", fileTitle, fileContent))
	allDocs = append(allDocs, fileDoc)

	// 2. Get symbols in this file
	symbolParams := solr.QueryParams{
		Query:         "*:*",
		FilterQueries: append(fqs, fmt.Sprintf("parent_id:%q", fileID), `doc_level:"symbol"`),
		Rows:          100,
		Sort:          "line_start asc",
		Highlight:     false,
	}

	symbolResp, err := codeClient.Query(ctx, symbolParams)
	if err != nil {
		return nil, fmt.Errorf("symbol lookup failed: %w", err)
	}

	if line > 0 && len(symbolResp.Docs) > 0 {
		// Find the enclosing symbol and its neighbors
		sb.WriteString("=== Symbols at/near line ===\n")
		var enclosingIdx int = -1
		for i, doc := range symbolResp.Docs {
			lineStart, _ := doc["line_start"].(float64)
			lineEnd, _ := doc["line_end"].(float64)
			if int(lineStart) <= line && line <= int(lineEnd) {
				enclosingIdx = i
				break
			}
		}

		// Show enclosing symbol + neighbors
		startIdx := enclosingIdx - 1
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := enclosingIdx + 2
		if endIdx > len(symbolResp.Docs) {
			endIdx = len(symbolResp.Docs)
		}
		if enclosingIdx < 0 {
			// No enclosing symbol found, show all
			startIdx = 0
			endIdx = len(symbolResp.Docs)
			if endIdx > 5 {
				endIdx = 5
			}
		}

		for i := startIdx; i < endIdx; i++ {
			doc := symbolResp.Docs[i]
			title, _ := doc["title"].(string)
			content, _ := doc["content"].(string)
			symbolType, _ := doc["symbol_type"].(string)
			lineStart, _ := doc["line_start"].(float64)

			marker := ""
			if i == enclosingIdx {
				marker = " <<< enclosing"
			}

			sb.WriteString(fmt.Sprintf("\n--- [%s] %s (L%d)%s ---\n", symbolType, title, int(lineStart), marker))
			sb.WriteString(content)
			sb.WriteString("\n")
			allDocs = append(allDocs, doc)
		}
	} else if len(symbolResp.Docs) > 0 {
		// No line specified, show symbol index
		sb.WriteString("=== Symbols ===\n")
		for _, doc := range symbolResp.Docs {
			title, _ := doc["title"].(string)
			symbolType, _ := doc["symbol_type"].(string)
			lineStart, _ := doc["line_start"].(float64)
			sb.WriteString(fmt.Sprintf("  [%s] %s  L%d\n", symbolType, title, int(lineStart)))
			allDocs = append(allDocs, doc)
		}
	}

	// 3. Include package context if depth > 0
	if depth >= 1 {
		parentID, _ := fileDoc["parent_id"].(string)
		if parentID != "" {
			pkgParams := solr.QueryParams{
				Query:         fmt.Sprintf("id:%q", parentID),
				Rows:          1,
				Highlight:     false,
			}
			pkgResp, err := codeClient.Query(ctx, pkgParams)
			if err == nil && len(pkgResp.Docs) > 0 {
				pkgDoc := pkgResp.Docs[0]
				pkgTitle, _ := pkgDoc["title"].(string)
				pkgContent, _ := pkgDoc["content"].(string)
				sb.WriteString(fmt.Sprintf("\n=== %s ===\n%s\n", pkgTitle, pkgContent))
				allDocs = append(allDocs, pkgDoc)
			}
		}
	}

	return ToolOutput{
		Text: sb.String(),
		Structured: map[string]any{
			"documents": allDocs,
		},
	}, nil
}
