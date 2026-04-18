package main

import (
	"context"
	"log"
	"strings"
	"time"
)

// embedForStore produces an embedding for a memory write. It combines title
// and content (title first, it's usually the most informative signal) and
// calls the configured provider with a short timeout. On error or when no
// provider is configured, returns nil so callers can still store the doc.
func embedForStore(ctx context.Context, title, content string) []float32 {
	if embedProvider == nil {
		return nil
	}
	text := strings.TrimSpace(title)
	if content != "" {
		if text != "" {
			text += "\n\n"
		}
		text += content
	}
	if text == "" {
		return nil
	}

	// Short timeout: a slow provider shouldn't block a memory write.
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	v, err := embedProvider.Embed(cctx, text)
	if err != nil {
		log.Printf("embed: failed (continuing without vector): %v", err)
		return nil
	}
	return v
}
