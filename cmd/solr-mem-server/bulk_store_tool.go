package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/arreyder/solr-mem/internal/contenthash"
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
	var duplicates []map[string]any
	totalScrubbed := 0

	windowSec := getInt(args, "dedup_window_seconds", defaultDedupWindowSec)
	onDup := normalizeOnDuplicate(getString(args, "on_duplicate"))

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

		tags := getStringSlice(m, "tags")
		hash := contenthash.Compute(scrubbedTitle, scrubbedContent, tags)

		if hash != "" && windowSec > 0 && onDup != "force" {
			existingID, err := findRecentByHash(ctx, solrClient, hash, windowSec)
			if err != nil {
				log.Printf("bulk_store: dedup lookup failed at index %d (continuing as new): %v", i, err)
			} else if existingID != "" {
				action := "skipped"
				if onDup == "merge" {
					upd := map[string]any{"updated_at": now.Format(time.RFC3339)}
					if err := solrClient.Update(ctx, existingID, upd); err != nil {
						log.Printf("bulk_store: merge update failed at index %d: %v", i, err)
					} else {
						action = "merged"
					}
				}
				duplicates = append(duplicates, map[string]any{
					"index":  i,
					"id":     existingID,
					"action": action,
				})
				continue
			}
		}

		id := uuid.New().String()
		ids = append(ids, id)

		docs = append(docs, solr.Document{
			ID:          id,
			AgentID:     getString(m, "agent_id"),
			MemoryType:  getString(m, "memory_type"),
			Content:     scrubbedContent,
			Title:       scrubbedTitle,
			Tags:        tags,
			Source:      getString(m, "source"),
			Importance:  getFloat(m, "importance", 0.5),
			Metadata:    metadata,
			CreatedAt:   now,
			UpdatedAt:   now,
			ExpiresAt:   expiresAt,
			Lifetime:    lifetime,
			SessionID:   getString(m, "session_id"),
			RelatedIDs:  getStringSlice(m, "related_ids"),
			Format:      format,
			ContentHash: hash,
			Embedding:   embedForStore(ctx, scrubbedTitle, scrubbedContent),
		})
	}

	if len(docs) > 0 {
		if err := solrClient.Add(ctx, docs...); err != nil {
			return nil, fmt.Errorf("failed to bulk store memories: %w", err)
		}
	}

	text := fmt.Sprintf("Stored %d memories.\nIDs: %v", len(docs), ids)
	structured := map[string]any{
		"count": len(docs),
		"ids":   ids,
	}
	if len(duplicates) > 0 {
		text += fmt.Sprintf("\nDedup: %d duplicates within %ds window", len(duplicates), windowSec)
		structured["duplicates"] = duplicates
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
