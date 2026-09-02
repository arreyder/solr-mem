package main

import (
	"context"
	"fmt"
	"time"

	"github.com/arreyder/solr-mem/internal/privacy"
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
	format := getString(args, "format")
	if format == "" {
		format = "prose"
	}

	title := getString(args, "title")
	metadata := getString(args, "metadata")
	scrubbedContent, contentHits := scrubString(content)
	scrubbedTitle, titleHits := scrubString(title)
	allHits := privacy.MergeHits(contentHits, titleHits)
	metadata = privacy.MergeMetadata(metadata, allHits)

	doc := solr.Document{
		ID:         uuid.New().String(),
		AgentID:    getString(args, "agent_id"),
		MemoryType: getString(args, "memory_type"),
		Content:    scrubbedContent,
		Title:      scrubbedTitle,
		Tags:       getStringSlice(args, "tags"),
		Source:     getString(args, "source"),
		Importance: getFloat(args, "importance", 0.5),
		Metadata:   metadata,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  expiresAt,
		Lifetime:   lifetime,
		SessionID:  getString(args, "session_id"),
		RelatedIDs: getStringSlice(args, "related_ids"),
		Format:     format,
	}
	doc.Embedding = embedMemoryText(ctx, scrubbedTitle, scrubbedContent)

	if err := solrClient.Add(ctx, doc); err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	result := fmt.Sprintf("Memory stored successfully.\nID: %s\nType: %s\nLifetime: %s\nTitle: %s\nCreated: %s",
		doc.ID, doc.MemoryType, doc.Lifetime, doc.Title, doc.CreatedAt.Format(time.RFC3339))
	if expiresAt != "" {
		result += fmt.Sprintf("\nExpires: %s", expiresAt)
	}

	structured := map[string]any{
		"id":         doc.ID,
		"lifetime":   doc.Lifetime,
		"expires_at": expiresAt,
		"created_at": doc.CreatedAt.Format(time.RFC3339),
	}
	if n := len(allHits); n > 0 {
		total := 0
		for _, v := range allHits {
			total += v
		}
		structured["scrub_count"] = total
		result += fmt.Sprintf("\nPrivacy: redacted %d secret(s) across %d kind(s)", total, n)
	}

	return ToolOutput{
		Text:       result,
		Structured: structured,
	}, nil
}
