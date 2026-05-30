package main

import (
	"context"
	"fmt"

	"github.com/arreyder/solr-mem/internal/solr"
)

// defaultDedupWindowSec is the default window for content-hash dedup on writes.
// Callers can override per-call with dedup_window_seconds; 0 disables.
const defaultDedupWindowSec = 300

// findRecentByHash returns the ID of the most recent memory with content_hash
// equal to hash and created_at within the last windowSec seconds. Empty string
// means no match (or the lookup errored — callers treat it as "not a dup").
func findRecentByHash(ctx context.Context, client *solr.Client, hash string, windowSec int) (string, error) {
	if hash == "" || windowSec <= 0 || client == nil {
		return "", nil
	}
	params := solr.QueryParams{
		Query: "*:*",
		FilterQueries: []string{
			fmt.Sprintf("content_hash:%s", hash),
			fmt.Sprintf("created_at:[NOW-%dSECONDS TO *]", windowSec),
		},
		Fields: []string{"id"},
		Sort:   "created_at desc",
		Rows:   1,
	}
	resp, err := client.Query(ctx, params)
	if err != nil {
		return "", err
	}
	if len(resp.Docs) == 0 {
		return "", nil
	}
	id, _ := resp.Docs[0]["id"].(string)
	return id, nil
}

// normalizeOnDuplicate canonicalizes the on_duplicate argument.
// "" / unrecognized → "skip".
func normalizeOnDuplicate(s string) string {
	switch s {
	case "skip", "merge", "force":
		return s
	default:
		return "skip"
	}
}
