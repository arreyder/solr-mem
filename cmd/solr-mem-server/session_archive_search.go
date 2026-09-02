package main

import (
 "context"
 "fmt"
 "github.com/arreyder/solr-mem/internal/solr"
)

func searchOMPSessionArchiveTool(ctx context.Context, args map[string]any) (any, error) {
 query := getString(args, "query")
 if query == "" { return nil, fmt.Errorf("query is required") }
 filters := []string{}
 if id := getString(args, "session_id"); id != "" { filters = append(filters, "session_id:"+id) }
 if kind := getString(args, "event_type"); kind != "" { filters = append(filters, "event_type:"+kind) }
 result, err := sessionArchiveClient.Query(ctx, solr.QueryParams{Query: query, FilterQueries: filters, Rows: getInt(args, "limit", 20), Highlight: true})
 if err != nil { return nil, fmt.Errorf("search session archive: %w", err) }
 return ToolOutput{Text: fmt.Sprintf("Found %d archive chunks", result.NumFound), Structured: map[string]any{"count": result.NumFound, "chunks": result.Docs}}, nil
}
