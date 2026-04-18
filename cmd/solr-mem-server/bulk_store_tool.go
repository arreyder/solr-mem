package main

import (
	"context"
	"fmt"
	"time"

	"github.com/arreyder/solr-mem/internal/privacy"
	"github.com/arreyder/solr-mem/internal/solr"
	"github.com/google/uuid"
)

func bulkStoreMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
	memoriesRaw, ok := args["memories"]
	if !ok || memoriesRaw == nil {
		return nil, fmt.Errorf("memories array is required")
	}

	memories, ok := memoriesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("memories must be an array")
	}
	if len(memories) == 0 {
		return nil, fmt.Errorf("memories array is empty")
	}

	now := time.Now().UTC()
	var docs []solr.Document
	var ids []string
	totalScrubbed := 0

	for i, raw := range memories {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("memory at index %d is not an object", i)
		}

		content := getString(m, "content")
		if content == "" {
			return nil, fmt.Errorf("memory at index %d is missing content", i)
		}

		lifetime := normalizeLifetime(getString(m, "lifetime"))
		expiresAt := resolveExpiration(lifetime, getString(m, "expires_at"))

		id := uuid.New().String()
		ids = append(ids, id)

		format := getString(m, "format")
		if format == "" {
			format = "prose"
		}

		title := getString(m, "title")
		metadata := getString(m, "metadata")
		scrubbedContent, contentHits := scrubString(content)
		scrubbedTitle, titleHits := scrubString(title)
		allHits := privacy.MergeHits(contentHits, titleHits)
		metadata = privacy.MergeMetadata(metadata, allHits)
		for _, v := range allHits {
			totalScrubbed += v
		}

		docs = append(docs, solr.Document{
			ID:         id,
			AgentID:    getString(m, "agent_id"),
			MemoryType: getString(m, "memory_type"),
			Content:    scrubbedContent,
			Title:      scrubbedTitle,
			Tags:       getStringSlice(m, "tags"),
			Source:     getString(m, "source"),
			Importance: getFloat(m, "importance", 0.5),
			Metadata:   metadata,
			CreatedAt:  now,
			UpdatedAt:  now,
			ExpiresAt:  expiresAt,
			Lifetime:   lifetime,
			SessionID:  getString(m, "session_id"),
			RelatedIDs: getStringSlice(m, "related_ids"),
			Format:     format,
		})
	}

	if err := solrClient.Add(ctx, docs...); err != nil {
		return nil, fmt.Errorf("failed to bulk store memories: %w", err)
	}

	text := fmt.Sprintf("Successfully stored %d memories.\nIDs: %v", len(docs), ids)
	structured := map[string]any{
		"count": len(docs),
		"ids":   ids,
	}
	if totalScrubbed > 0 {
		text += fmt.Sprintf("\nPrivacy: redacted %d secret(s) across all memories", totalScrubbed)
		structured["scrub_count"] = totalScrubbed
	}

	return ToolOutput{
		Text:       text,
		Structured: structured,
	}, nil
}
