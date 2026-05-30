package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

// forceReindexTool resolves a repo from a (possibly short) name and asks the
// indexer's control endpoint to re-pull and fully re-index it. The actual
// indexing is performed by the indexer process (which owns the clones and the
// index lock); this tool only resolves the target and forwards the request.
func forceReindexTool(ctx context.Context, args map[string]any) (any, error) {
	repoArg := getString(args, "repo_url")
	if repoArg == "" {
		return nil, fmt.Errorf("repo_url is required (a short name like \"ductone/c1\" or a full path)")
	}

	// Resolve the target against indexed status docs using the same normalized
	// matching as the search tools.
	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:         "*:*",
		FilterQueries: []string{`doc_level:"status"`, repoFilterQuery(repoArg)},
		Fields:        []string{"repo_id", "repo_url", "commit_sha", "updated_at"},
		Rows:          10,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve repo failed: %w", err)
	}

	switch len(resp.Docs) {
	case 0:
		return ToolOutput{Text: fmt.Sprintf("No indexed repo matched %q.\n%s", repoArg, knownReposHint(ctx))}, nil
	case 1:
		// proceed
	default:
		var matches []string
		for _, d := range resp.Docs {
			matches = append(matches, docString(d, "repo_url"))
		}
		return ToolOutput{Text: fmt.Sprintf("%q matched %d repos; be more specific:\n  %s",
			repoArg, len(resp.Docs), strings.Join(matches, "\n  "))}, nil
	}

	doc := resp.Docs[0]
	repoURL := docString(doc, "repo_url")
	prevCommit := docString(doc, "commit_sha")
	if len(prevCommit) > 12 {
		prevCommit = prevCommit[:12]
	}

	ack, err := postReindex(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("could not reach indexer control endpoint at %s: %w (is the indexer running with INDEXER_CONTROL_ADDR enabled?)", indexerControlURL, err)
	}
	if ack.Status != "queued" {
		return nil, fmt.Errorf("indexer rejected reindex for %s: %s", repoURL, ack.Error)
	}

	text := fmt.Sprintf(
		"Queued full re-pull + reindex of %s (was at commit %s).\n"+
			"The indexer will git-fetch, then rebuild the repo from scratch "+
			"(also refreshes cross-references and package/repo docs the incremental path skips).\n"+
			"Re-run index_status or a search shortly and check repo_index.indexed_commit / is_stale.",
		repoURL, prevCommit)
	return ToolOutput{Text: text, Structured: map[string]any{
		"status":        "queued",
		"repo_url":      repoURL,
		"prev_commit":   prevCommit,
		"control_url":   indexerControlURL,
		"requested_arg": repoArg,
	}}, nil
}

type reindexAck struct {
	Status string `json:"status"`
	Repo   string `json:"repo"`
	Error  string `json:"error"`
}

func postReindex(ctx context.Context, repoURL string) (*reindexAck, error) {
	body, _ := json.Marshal(map[string]string{"repo": repoURL})
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		strings.TrimRight(indexerControlURL, "/")+"/reindex", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	var ack reindexAck
	if err := json.NewDecoder(httpResp.Body).Decode(&ack); err != nil {
		return nil, fmt.Errorf("bad response (HTTP %d): %w", httpResp.StatusCode, err)
	}
	return &ack, nil
}

// knownReposHint lists currently indexed repo URLs to help a caller correct a
// mistyped name. Best-effort: returns "" on error.
func knownReposHint(ctx context.Context) string {
	resp, err := codeClient.Query(ctx, solr.QueryParams{
		Query:         "*:*",
		FilterQueries: []string{`doc_level:"status"`},
		Fields:        []string{"repo_url"},
		Rows:          50,
	})
	if err != nil || len(resp.Docs) == 0 {
		return ""
	}
	var urls []string
	for _, d := range resp.Docs {
		if u := docString(d, "repo_url"); u != "" {
			urls = append(urls, u)
		}
	}
	return "Indexed repos:\n  " + strings.Join(urls, "\n  ")
}
