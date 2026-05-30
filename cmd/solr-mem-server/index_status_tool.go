package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

func indexStatusTool(ctx context.Context, args map[string]any) (any, error) {
	params := solr.QueryParams{
		Query:         "*:*",
		FilterQueries: []string{`doc_level:"status"`},
		Fields:        []string{"id", "repo_url", "repo_id", "commit_sha", "content", "tags", "updated_at"},
		Sort:          "updated_at desc",
		Rows:          50,
	}

	if fq := repoFilterQuery(getString(args, "repo_url")); fq != "" {
		params.FilterQueries = append(params.FilterQueries, fq)
	}

	resp, err := codeClient.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("index status query failed: %w", err)
	}

	return ToolOutput{
		Text:       formatIndexStatus(resp),
		Structured: resp,
	}, nil
}

func formatIndexStatus(resp *solr.QueryResponse) string {
	if len(resp.Docs) == 0 {
		return "No indexing status found. The indexer may not have run yet."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Indexed repositories: %d\n\n", resp.NumFound))

	for _, doc := range resp.Docs {
		repoURL, _ := doc["repo_url"].(string)
		content, _ := doc["content"].(string)
		updatedAt, _ := doc["updated_at"].(string)

		sb.WriteString(fmt.Sprintf("## %s\n", repoURL))
		sb.WriteString(content)
		sb.WriteString(fmt.Sprintf("\nupdated_at: %s\n\n", updatedAt))
	}

	return sb.String()
}
