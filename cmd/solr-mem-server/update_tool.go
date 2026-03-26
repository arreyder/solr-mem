package main

import (
	"context"
	"fmt"
	"time"
)

func updateMemoryTool(ctx context.Context, args map[string]any) (any, error) {
	id := getString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	fields := make(map[string]any)

	if v := getString(args, "content"); v != "" {
		fields["content"] = v
	}
	if v := getString(args, "title"); v != "" {
		fields["title"] = v
	}
	if v := getString(args, "memory_type"); v != "" {
		fields["memory_type"] = v
	}
	if v := getStringSlice(args, "tags"); len(v) > 0 {
		fields["tags"] = v
	}
	if v := getString(args, "source"); v != "" {
		fields["source"] = v
	}
	if _, ok := args["importance"]; ok {
		fields["importance"] = getFloat(args, "importance", 0.5)
	}
	if v := getString(args, "metadata"); v != "" {
		fields["metadata"] = v
	}
	if v := getString(args, "session_id"); v != "" {
		fields["session_id"] = v
	}
	if v := getStringSlice(args, "related_ids"); len(v) > 0 {
		fields["related_ids"] = v
	}

	// Handle lifetime changes — recalculate expires_at
	if v := getString(args, "lifetime"); v != "" {
		lifetime := normalizeLifetime(v)
		fields["lifetime"] = lifetime
		expiresAt := resolveExpiration(lifetime, getString(args, "expires_at"))
		if expiresAt != "" {
			fields["expires_at"] = expiresAt
		}
	} else if v := getString(args, "expires_at"); v != "" {
		fields["expires_at"] = v
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	fields["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	if err := solrClient.Update(ctx, id, fields); err != nil {
		return nil, fmt.Errorf("failed to update memory: %w", err)
	}

	return fmt.Sprintf("Memory %s updated successfully. Updated %d field(s).", id, len(fields)-1), nil
}
