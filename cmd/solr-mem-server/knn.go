package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/arreyder/solr-mem/internal/solr"
)

// knnQueryString formats a Solr KNN local-params query for the embedding field.
// Produces: {!knn f=embedding topK=N}[v1,v2,...]
func knnQueryString(vec []float32, topK int) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return fmt.Sprintf("{!knn f=embedding topK=%d}[%s]", topK, strings.Join(parts, ","))
}

// runKNN performs a KNN search against the embedding field. Filters from the
// caller (agent_id, tags, etc.) are passed through as fq so semantic hits
// respect the same scoping as BM25 hits.
func runKNN(ctx context.Context, client *solr.Client, vec []float32, filters []string, topK int) (*solr.QueryResponse, error) {
	params := solr.QueryParams{
		Query:         knnQueryString(vec, topK),
		FilterQueries: filters,
		Rows:          topK,
	}
	return client.Query(ctx, params)
}
