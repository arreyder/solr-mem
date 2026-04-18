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

	tags := getStringSlice(args, "tags")
	hash := contenthash.Compute(scrubbedTitle, scrubbedContent, tags)
	windowSec := getInt(args, "dedup_window_seconds", defaultDedupWindowSec)
	onDup := normalizeOnDuplicate(getString(args, "on_duplicate"))

	if hash != "" && windowSec > 0 && onDup != "force" {
		existingID, err := findRecentByHash(ctx, solrClient, hash, windowSec)
		if err != nil {
			log.Printf("store_memory: dedup lookup failed (continuing as new): %v", err)
		} else if existingID != "" {
			if onDup == "merge" {
				upd := map[string]any{"updated_at": now.Format(time.RFC3339)}
				if err := solrClient.Update(ctx, existingID, upd); err != nil {
					log.Printf("store_memory: merge update failed: %v", err)
				}
				return ToolOutput{
					Text: fmt.Sprintf("Memory already stored within the last %ds. Merged into existing.\nID: %s", windowSec, existingID),
					Structured: map[string]any{
						"id":        existingID,
						"duplicate": true,
						"action":    "merged",
					},
				}, nil
			}
			// skip (default)
			return ToolOutput{
				Text: fmt.Sprintf("Memory already stored within the last %ds. Skipped.\nID: %s", windowSec, existingID),
				Structured: map[string]any{
					"id":        existingID,
					"duplicate": true,
					"action":    "skipped",
				},
			}, nil
		}
	}

	doc := solr.Document{
		ID:          uuid.New().String(),
		AgentID:     getString(args, "agent_id"),
		MemoryType:  getString(args, "memory_type"),
		Content:     scrubbedContent,
		Title:       scrubbedTitle,
		Tags:        tags,
		Source:      getString(args, "source"),
		Importance:  getFloat(args, "importance", 0.5),
		Metadata:    metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   expiresAt,
		Lifetime:    lifetime,
		SessionID:   getString(args, "session_id"),
		RelatedIDs:  getStringSlice(args, "related_ids"),
		Format:      format,
		ContentHash: hash,
	}

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
