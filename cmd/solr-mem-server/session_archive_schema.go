package main

import "github.com/modelcontextprotocol/go-sdk/mcp"

func sessionArchiveToolSchemas() []ToolDefinition {
	return []ToolDefinition{{
		Tool: &mcp.Tool{
			Name: "archive_omp_session",
			Description: "Archive redacted raw OMP session chunks into the isolated omp_sessions core. This archive is not embedded and is excluded from normal memory recall. Raw chunks expire after 90 days.",
			InputSchema: NewObjectSchema(map[string]any{
				"archive_id": prop("string", "Deterministic immutable archive revision ID"),
				"session_id": prop("string", "OMP session ID"),
				"event_type": prop("string", "session_exit or compaction"),
				"host": prop("string", "Origin host"),
				"cwd": prop("string", "Session working directory"),
				"repo_origin": prop("string", "Repository origin"),
				"git_head": prop("string", "Repository commit"),
				"metadata": prop("string", "JSON metadata"),
				"chunks": arrayPropSchema(NewObjectSchema(map[string]any{"content": prop("string", "Raw transcript chunk")}, "content"), "Ordered raw transcript chunks, max 256 x 64KiB"),
			}, "archive_id", "session_id", "chunks"),
		},
		Handler: archiveOMPSessionTool,
	}, {
		Tool: &mcp.Tool{
			Name: "search_omp_session_archive",
			Description: "Search the isolated OMP raw-session archive for offline analysis only. Archive chunks are never part of normal memory recall.",
			InputSchema: NewObjectSchema(map[string]any{
				"query": prop("string", "Lexical archive query"),
				"session_id": prop("string", "Optional exact OMP session ID"),
				"event_type": prop("string", "Optional event type"),
				"limit": integerProp("Maximum chunks (default 20, max 100)", intPtr(1), intPtr(100)),
			}, "query"),
		},
		Handler: searchOMPSessionArchiveTool,
	}}
}
