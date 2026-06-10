package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

// embedMemoryText embeds a memory's semantic text (title + content). Returns
// nil when embeddings are disabled or on error, so store/update degrade to
// lexical-only instead of failing the write.
func embedMemoryText(ctx context.Context, title, content string) []float32 {
	if !embedder.Enabled() {
		return nil
	}
	text := strings.TrimSpace(title + "\n\n" + content)
	if text == "" {
		return nil
	}
	vec, err := embedder.Embed(ctx, text)
	if err != nil {
		log.Printf("embedding failed (proceeding without vector): %v", err)
		return nil
	}
	return vec
}

// currentTitleContent fetches a memory's stored title and content. Used when
// re-embedding on update where only one of the two was supplied, so the vector
// still reflects both fields.
func currentTitleContent(ctx context.Context, id string) (title, content string) {
	resp, err := solrClient.Query(ctx, solr.QueryParams{
		Query:     fmt.Sprintf("id:%q", id),
		Rows:      1,
		Fields:    []string{"title", "content"},
		Highlight: false,
	})
	if err != nil || resp == nil || len(resp.Docs) == 0 {
		return "", ""
	}
	title, _ = resp.Docs[0]["title"].(string)
	content, _ = resp.Docs[0]["content"].(string)
	return title, content
}
