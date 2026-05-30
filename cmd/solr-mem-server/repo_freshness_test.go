package main

import (
	"strings"
	"testing"
	"time"
)

func TestFreshnessFromStatusDoc(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-05-30T12:00:00Z")

	tests := []struct {
		name       string
		updatedAt  string
		wantAgeSec int64
		wantStale  bool
	}{
		{"fresh, minutes old", "2026-05-30T11:30:00Z", 1800, false},
		{"just under threshold", "2026-05-23T12:00:01Z", 7*86400 - 1, false},
		{"just over threshold", "2026-05-23T11:59:59Z", 7*86400 + 1, true},
		{"very stale", "2026-04-13T12:00:00Z", 47 * 86400, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := map[string]any{
				"repo_id":    "abc123",
				"repo_url":   "/Users/x/solr-mem-repos/ductone_c1",
				"commit_sha": "915ea7970805b93ba8424c6e7d06848d3dd7d729",
				"updated_at": tt.updatedAt,
			}
			f := freshnessFromStatusDoc(doc, now)
			if f.AgeSeconds != tt.wantAgeSec {
				t.Errorf("AgeSeconds = %d, want %d", f.AgeSeconds, tt.wantAgeSec)
			}
			if f.IsStale != tt.wantStale {
				t.Errorf("IsStale = %v, want %v", f.IsStale, tt.wantStale)
			}
			if f.IndexedCommit != doc["commit_sha"] {
				t.Errorf("IndexedCommit not carried through")
			}
		})
	}
}

func TestFreshnessFromStatusDocUnparseableTime(t *testing.T) {
	now := time.Now()
	f := freshnessFromStatusDoc(map[string]any{"updated_at": "not-a-time"}, now)
	if f.AgeSeconds != 0 || f.IsStale {
		t.Errorf("unparseable time should yield zero age and not-stale, got %+v", f)
	}
}

func TestDistinctRepoIDs(t *testing.T) {
	docs := []map[string]any{
		{"repo_id": "a"}, {"repo_id": "b"}, {"repo_id": "a"}, {"repo_id": ""}, {},
	}
	got := distinctRepoIDs(docs)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("distinctRepoIDs = %v, want [a b]", got)
	}
}

func TestFreshnessTextStaleMarker(t *testing.T) {
	if freshnessText(nil) != "" {
		t.Error("empty freshness should render empty string")
	}
	out := freshnessText([]repoFreshness{
		{RepoURL: "r1", IndexedCommit: "915ea7970805deadbeef", IndexedAt: "2026-05-30T11:30:00Z", AgeSeconds: 1800, IsStale: false},
		{RepoURL: "r2", IndexedCommit: "abc", IndexedAt: "2026-04-13T12:00:00Z", AgeSeconds: 47 * 86400, IsStale: true},
	})
	if !strings.Contains(out, "Index freshness") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "915ea7970805") || strings.Contains(out, "915ea7970805deadbeef") {
		t.Error("commit should be truncated to 12 chars")
	}
	if !strings.Contains(out, "STALE") {
		t.Error("stale repo should be marked STALE")
	}
	if strings.Count(out, "STALE") != 1 {
		t.Error("only the stale repo should be marked")
	}
}

func TestHumanizeAge(t *testing.T) {
	cases := map[int64]string{30: "30s", 90: "1m", 7200: "2h", 47 * 86400: "47d"}
	for sec, want := range cases {
		if got := humanizeAge(sec); got != want {
			t.Errorf("humanizeAge(%d) = %q, want %q", sec, got, want)
		}
	}
}
