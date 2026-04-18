// Package main is the solr-mem-backfill tool: it computes embeddings for
// existing memories that don't have one yet and writes them back via atomic
// update. Safe to run incrementally — it always queries "missing embedding"
// first, so a partial run can be resumed just by running it again.
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/arreyder/solr-mem/internal/embed"
	"github.com/arreyder/solr-mem/internal/solr"
)

// Options controls a backfill run.
type Options struct {
	BatchSize   int           // docs per Solr page (default 50)
	Concurrency int           // parallel embed calls (default 4)
	DryRun      bool          // compute but don't write
	MaxDocs     int           // cap total processed (0 = unlimited)
	Force       bool          // re-embed docs even if they already have an embedding
	Pause       time.Duration // sleep between batches (rate-limit headroom)
}

// Stats summarize a run.
type Stats struct {
	Scanned  int // docs returned from Solr
	Skipped  int // already had an embedding (and !Force)
	Embedded int // docs we computed vectors for
	Written  int // docs we actually wrote back (0 when DryRun)
	Errors   int // embed failures; these get logged but don't abort
}

// Run scans the memories collection for docs needing embeddings, computes
// them, and writes back. It returns when the backlog is empty, MaxDocs is
// hit, or ctx is canceled.
func Run(ctx context.Context, client *solr.Client, provider embed.Provider, opts Options) (Stats, error) {
	if provider == nil {
		return Stats{}, fmt.Errorf("backfill: no embedding provider configured")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	// Track IDs we've already attempted this run. Without this, a doc that
	// fails to embed (and therefore never gets its field written) would keep
	// showing up in every "-_exists_:embedding" query and loop forever.
	seen := make(map[string]bool)

	var stats Stats
	for {
		if opts.MaxDocs > 0 && stats.Embedded >= opts.MaxDocs {
			break
		}
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}

		batch, err := fetchBatch(ctx, client, opts.Force, opts.BatchSize)
		if err != nil {
			return stats, fmt.Errorf("fetch batch: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		stats.Scanned += len(batch)

		// Build the work list: skip docs already attempted this run, and
		// skip docs with existing embeddings unless Force.
		work := make([]memoryDoc, 0, len(batch))
		for _, d := range batch {
			if seen[d.ID] {
				continue
			}
			seen[d.ID] = true
			if !opts.Force && d.HasEmbedding {
				stats.Skipped++
				continue
			}
			work = append(work, d)
		}
		if opts.MaxDocs > 0 && stats.Embedded+len(work) > opts.MaxDocs {
			work = work[:opts.MaxDocs-stats.Embedded]
		}

		// If every doc in this page was already seen, we've exhausted the
		// backlog that this run can make progress on.
		if len(work) == 0 {
			break
		}

		updates := embedBatch(ctx, provider, work, opts.Concurrency, &stats)

		if len(updates) > 0 && !opts.DryRun {
			if err := client.BulkUpdate(ctx, updates); err != nil {
				return stats, fmt.Errorf("bulk update: %w", err)
			}
			stats.Written += len(updates)
		}

		log.Printf("backfill: scanned=%d skipped=%d embedded=%d written=%d errors=%d",
			stats.Scanned, stats.Skipped, stats.Embedded, stats.Written, stats.Errors)

		if opts.Pause > 0 {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			case <-time.After(opts.Pause):
			}
		}
	}
	return stats, nil
}

type memoryDoc struct {
	ID           string
	Title        string
	Content      string
	HasEmbedding bool
}

// fetchBatch returns up to batchSize memory docs. When !force, scopes to
// docs missing the embedding field so subsequent calls make progress as we
// write back.
func fetchBatch(ctx context.Context, client *solr.Client, force bool, batchSize int) ([]memoryDoc, error) {
	params := solr.QueryParams{
		Query:  "*:*",
		Rows:   batchSize,
		Fields: []string{"id", "title", "content", "embedding"},
		Sort:   "created_at asc",
	}
	if !force {
		// "-_exists_:embedding" is Solr's canonical missing-field filter
		// and works for any field type, including DenseVectorField.
		params.FilterQueries = append(params.FilterQueries, "-_exists_:embedding")
	}
	resp, err := client.Query(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]memoryDoc, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		id, _ := d["id"].(string)
		title, _ := d["title"].(string)
		content, _ := d["content"].(string)
		has := false
		if v, ok := d["embedding"]; ok {
			if arr, ok := v.([]any); ok && len(arr) > 0 {
				has = true
			}
		}
		out = append(out, memoryDoc{ID: id, Title: title, Content: content, HasEmbedding: has})
	}
	return out, nil
}

// embedBatch runs opts.Concurrency embed calls in parallel and returns the
// atomic-update payloads for docs that succeeded. Failures are logged and
// counted but don't stop the run.
func embedBatch(ctx context.Context, provider embed.Provider, docs []memoryDoc, concurrency int, stats *Stats) []map[string]any {
	if len(docs) == 0 {
		return nil
	}

	type result struct {
		ID        string
		Embedding []float32
		Err       error
	}

	in := make(chan memoryDoc)
	out := make(chan result)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range in {
				text := buildEmbedText(d.Title, d.Content)
				if text == "" {
					out <- result{ID: d.ID, Err: fmt.Errorf("empty text")}
					continue
				}
				cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				v, err := provider.Embed(cctx, text)
				cancel()
				out <- result{ID: d.ID, Embedding: v, Err: err}
			}
		}()
	}

	go func() {
		for _, d := range docs {
			select {
			case <-ctx.Done():
				break
			case in <- d:
			}
		}
		close(in)
		wg.Wait()
		close(out)
	}()

	updates := make([]map[string]any, 0, len(docs))
	for r := range out {
		if r.Err != nil {
			stats.Errors++
			log.Printf("backfill: embed %s failed: %v", r.ID, r.Err)
			continue
		}
		stats.Embedded++
		updates = append(updates, map[string]any{
			"id":        r.ID,
			"embedding": map[string]any{"set": r.Embedding},
		})
	}
	return updates
}

// buildEmbedText joins title and content with a blank line. Mirrors the
// shape used at write time in the server (embed_helper.go) so backfilled
// vectors live in the same space as live writes.
func buildEmbedText(title, content string) string {
	t := title
	if t != "" && content != "" {
		return t + "\n\n" + content
	}
	if t != "" {
		return t
	}
	return content
}
