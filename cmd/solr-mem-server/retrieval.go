package main

import (
	"context"
	"log"
	"time"
)

// collectRetrievableIDs extracts the ids to credit with a retrieval. Returns
// nil when tracking is disabled (track=false) so maintenance/bulk scans — e.g.
// the sleep-pass consolidation passes — don't pollute the usage signal.
func collectRetrievableIDs(docs []map[string]any, track bool) []string {
	if !track || len(docs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		if id, ok := d["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// recordRetrievalsAsync credits a retrieval for the surfaced docs without
// blocking the read path. Fire-and-forget: errors are logged, not returned, and
// it uses its own background context so it survives the request returning.
func recordRetrievalsAsync(docs []map[string]any, track bool) {
	ids := collectRetrievableIDs(docs, track)
	if len(ids) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := solrClient.RecordRetrievals(ctx, ids, time.Now()); err != nil {
			log.Printf("retrieval tracking failed for %d memories: %v", len(ids), err)
		}
	}()
}
