package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/arreyder/solr-mem/internal/privacy"
	"github.com/arreyder/solr-mem/internal/solr"
)

const sessionArchiveRetention = 90 * 24 * time.Hour

func archiveOMPSessionTool(ctx context.Context, args map[string]any) (any, error) {
	archiveID, sessionID := getString(args, "archive_id"), getString(args, "session_id")
	if archiveID == "" || sessionID == "" { return nil, fmt.Errorf("archive_id and session_id are required") }
	chunks, ok := args["chunks"].([]any)
	if !ok || len(chunks) == 0 || len(chunks) > 256 { return nil, fmt.Errorf("chunks must contain 1-256 items") }
	now := time.Now().UTC()
	docs := make([]solr.SessionArchiveDocument, 0, len(chunks))
	for index, raw := range chunks {
		chunk, ok := raw.(map[string]any); if !ok { return nil, fmt.Errorf("chunk %d must be an object", index) }
		content := getString(chunk, "content"); if content == "" { return nil, fmt.Errorf("chunk %d content is required", index) }
		if len(content) > 64*1024 { return nil, fmt.Errorf("chunk %d exceeds 64KiB", index) }
		scrubbed, hits := scrubString(content)
		sum := sha256.Sum256([]byte(scrubbed))
		metadata := privacy.MergeMetadata(getString(args, "metadata"), hits)
		docs = append(docs, solr.SessionArchiveDocument{
			ID: fmt.Sprintf("%s:%06d", archiveID, index), ArchiveID: archiveID, SessionID: sessionID,
			EventType: getString(args, "event_type"), ChunkIndex: index, ChunkCount: len(chunks),
			ContentSHA256: hex.EncodeToString(sum[:]), Content: scrubbed, Host: getString(args, "host"),
			CWD: getString(args, "cwd"), RepoOrigin: getString(args, "repo_origin"), GitHead: getString(args, "git_head"),
			EventAt: now, CreatedAt: now, ExpiresAt: now.Add(sessionArchiveRetention), Metadata: metadata,
		})
	}
	if err := sessionArchiveClient.AddJSON(ctx, docs); err != nil { return nil, fmt.Errorf("archive session: %w", err) }
	return ToolOutput{Text: fmt.Sprintf("Archived %d session chunks; expires %s", len(docs), docs[0].ExpiresAt.Format(time.RFC3339)), Structured: map[string]any{"archive_id": archiveID, "chunks": len(docs), "expires_at": docs[0].ExpiresAt.Format(time.RFC3339)}}, nil
}
