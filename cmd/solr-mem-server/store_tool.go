package main

import (
	"context"
	"fmt"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
	"github.com/google/uuid"
)

func storeMemoryTool(ctx context.Context, args map[string]any) (any, error) {
	content := getString(args, "content")
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	lifetime := normalizeLifetime(getString(args, "lifetime"))
	expiresAt := resolveExpiration(lifetime, getString(args, "expires_at"))

	now := time.Now().UTC()
	doc := solr.Document{
		ID:         uuid.New().String(),
		AgentID:    getString(args, "agent_id"),
		MemoryType: getString(args, "memory_type"),
		Content:    content,
		Title:      getString(args, "title"),
		Tags:       getStringSlice(args, "tags"),
		Source:     getString(args, "source"),
		Importance: getFloat(args, "importance", 0.5),
		Metadata:   getString(args, "metadata"),
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  expiresAt,
		Lifetime:   lifetime,
		SessionID:  getString(args, "session_id"),
		RelatedIDs: getStringSlice(args, "related_ids"),
	}

	if err := solrClient.Add(ctx, doc); err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	result := fmt.Sprintf("Memory stored successfully.\nID: %s\nType: %s\nLifetime: %s\nTitle: %s\nCreated: %s",
		doc.ID, doc.MemoryType, doc.Lifetime, doc.Title, doc.CreatedAt.Format(time.RFC3339))
	if expiresAt != "" {
		result += fmt.Sprintf("\nExpires: %s", expiresAt)
	}

	return ToolOutput{
		Text: result,
		Structured: map[string]any{
			"id":         doc.ID,
			"lifetime":   doc.Lifetime,
			"expires_at": expiresAt,
			"created_at": doc.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}
