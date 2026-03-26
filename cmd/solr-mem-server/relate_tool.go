package main

import (
	"context"
	"fmt"
)

func relateMemoriesTool(ctx context.Context, args map[string]any) (any, error) {
	ids := getStringSlice(args, "ids")
	if len(ids) < 2 {
		return nil, fmt.Errorf("at least 2 memory IDs are required")
	}

	// For each memory, add all other IDs to its related_ids
	for _, id := range ids {
		others := make([]string, 0, len(ids)-1)
		for _, other := range ids {
			if other != id {
				others = append(others, other)
			}
		}

		fields := map[string]any{
			"related_ids": others,
		}

		if err := solrClient.Update(ctx, id, fields); err != nil {
			return nil, fmt.Errorf("failed to update related_ids for %s: %w", id, err)
		}
	}

	return fmt.Sprintf("Successfully linked %d memories together.", len(ids)), nil
}
