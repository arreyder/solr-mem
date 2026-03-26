package main

import (
	"context"
	"fmt"
)

func deleteMemoryTool(ctx context.Context, args map[string]any) (any, error) {
	id := getString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	if err := solrClient.Delete(ctx, id); err != nil {
		return nil, fmt.Errorf("failed to delete memory: %w", err)
	}

	return fmt.Sprintf("Memory %s deleted successfully.", id), nil
}
