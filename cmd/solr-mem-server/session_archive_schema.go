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
	}}
}
