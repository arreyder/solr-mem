// Command solr-mem-backfill embeds existing memories so semantic search can
// find them. It re-embeds every memory (idempotent) — title + content via the
// configured embedder — and atomically sets the `embedding` field.
//
// Env: SOLR_URL (default http://localhost:8983/solr/memories), EMBED_URL,
// EMBED_MODEL, EMBED_DIM (same as the server).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/embed"
	"github.com/arreyder/solr-mem/internal/solr"
)

func main() {
	batch := flag.Int("batch", 100, "docs per page / update batch")
	flag.Parse()

	solrURL := os.Getenv("SOLR_URL")
	if solrURL == "" {
		solrURL = "http://localhost:8983/solr/memories"
	}
	client := solr.NewClient(solrURL)

	emb := embed.FromEnv()
	if !emb.Enabled() {
		log.Fatal("EMBED_URL not set — nothing to backfill (embeddings disabled)")
	}

	ctx := context.Background()
	start := 0
	total, embedded, failed := 0, 0, 0

	for {
		resp, err := client.Query(ctx, solr.QueryParams{
			Query:     "*:*",
			Rows:      *batch,
			Start:     start,
			Sort:      "id asc", // stable paging
			Fields:    []string{"id", "title", "content"},
			Highlight: false,
		})
		if err != nil {
			log.Fatalf("query at start=%d: %v", start, err)
		}
		if len(resp.Docs) == 0 {
			break
		}

		var updates []map[string]any
		for _, d := range resp.Docs {
			total++
			id, _ := d["id"].(string)
			title, _ := d["title"].(string)
			content, _ := d["content"].(string)
			text := strings.TrimSpace(title + "\n\n" + content)
			if id == "" || text == "" {
				continue
			}
			vec, err := emb.Embed(ctx, text)
			if err != nil || len(vec) == 0 {
				log.Printf("embed failed id=%s: %v", id, err)
				failed++
				continue
			}
			updates = append(updates, map[string]any{
				"id":        id,
				"embedding": map[string]any{"set": vec},
			})
			embedded++
		}

		if err := client.BulkUpdate(ctx, updates); err != nil {
			log.Fatalf("bulk update at start=%d: %v", start, err)
		}
		log.Printf("progress: %d seen, %d embedded, %d failed", total, embedded, failed)
		start += *batch
		time.Sleep(50 * time.Millisecond) // be gentle on the embed service
	}

	log.Printf("DONE: %d memories, %d embedded, %d failed", total, embedded, failed)
}
