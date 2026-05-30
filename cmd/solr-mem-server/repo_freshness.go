package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

// indexStaleAfter is how old a repo's last successful index can be before we
// flag it as stale in result envelopes. The indexer's clones are pulled on
// their own schedule and can drift from the caller's local checkout, so a
// caller seeing few/no results needs to know whether the index is fresh before
// concluding "not indexed". See memory badaa3e9 (root cause #2).
const indexStaleAfter = 7 * 24 * time.Hour

// repoFreshness describes how current the index is for a single repository.
type repoFreshness struct {
	RepoID        string `json:"repo_id"`
	RepoURL       string `json:"repo_url"`
	IndexedCommit string `json:"indexed_commit"`
	IndexedAt     string `json:"indexed_at"`
	AgeSeconds    int64  `json:"age_seconds"`
	IsStale       bool   `json:"is_stale"`
}

// codeEnvelope embeds a query response and adds repo_index freshness metadata.
// Embedding (not nesting) keeps the existing structured shape — NumFound, Docs,
// Highlighting, Facets — and merely adds a repo_index field alongside them.
type codeEnvelope struct {
	*solr.QueryResponse
	RepoIndex []repoFreshness `json:"repo_index,omitempty"`
}

// repoFreshnessFromDocs looks up the indexer status doc for every repository
// present in the given result docs and returns freshness metadata for each.
//
// It is best-effort: any lookup error (or absence of repo_ids) yields nil so a
// freshness lookup can never break or fail the underlying search.
func repoFreshnessFromDocs(ctx context.Context, docs []map[string]any) []repoFreshness {
	ids := distinctRepoIDs(docs)
	if len(ids) == 0 {
		return nil
	}

	clauses := make([]string, 0, len(ids))
	for _, id := range ids {
		clauses = append(clauses, fmt.Sprintf("repo_id:%q", id))
	}
	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query: "*:*",
		FilterQueries: []string{
			`doc_level:"status"`,
			"(" + strings.Join(clauses, " OR ") + ")",
		},
		Fields: []string{"repo_id", "repo_url", "commit_sha", "updated_at"},
		Rows:   len(ids),
	})
	if err != nil {
		return nil
	}

	now := time.Now()
	out := make([]repoFreshness, 0, len(resp.Docs))
	for _, doc := range resp.Docs {
		out = append(out, freshnessFromStatusDoc(doc, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoURL < out[j].RepoURL })
	return out
}

// freshnessFromStatusDoc derives freshness for one status doc relative to now.
// Pure (no Solr/clock dependency) so it can be unit-tested.
func freshnessFromStatusDoc(doc map[string]any, now time.Time) repoFreshness {
	f := repoFreshness{
		RepoID:        docString(doc, "repo_id"),
		RepoURL:       docString(doc, "repo_url"),
		IndexedCommit: docString(doc, "commit_sha"),
		IndexedAt:     docString(doc, "updated_at"),
	}
	if t, err := time.Parse(time.RFC3339, f.IndexedAt); err == nil {
		age := now.Sub(t)
		f.AgeSeconds = int64(age.Seconds())
		f.IsStale = age > indexStaleAfter
	}
	return f
}

// freshnessText renders a compact, human-readable index-freshness block for the
// text envelope. Returns "" when there is nothing to report.
func freshnessText(fr []repoFreshness) string {
	if len(fr) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n--- Index freshness ---\n")
	for _, f := range fr {
		commit := f.IndexedCommit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		stale := ""
		if f.IsStale {
			stale = " STALE — index may be behind the repo; verify against a local checkout if results look incomplete"
		}
		sb.WriteString(fmt.Sprintf("%s: indexed %s ago at commit %s (%s)%s\n",
			f.RepoURL, humanizeAge(f.AgeSeconds), commit, f.IndexedAt, stale))
	}
	return sb.String()
}

func distinctRepoIDs(docs []map[string]any) []string {
	seen := map[string]bool{}
	var ids []string
	for _, doc := range docs {
		if id := docString(doc, "repo_id"); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func docString(doc map[string]any, key string) string {
	s, _ := doc[key].(string)
	return s
}

func humanizeAge(seconds int64) string {
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dd", seconds/86400)
	}
}
