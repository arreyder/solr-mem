// solr-mem-bench is a small harness for measuring memory retrieval quality.
//
// Usage:
//
//	solr-mem-bench -solr-url http://localhost:8983/solr -collection memories_bench \
//	  -corpus testdata/corpus.jsonl -queries testdata/queries.jsonl -seed
//
// The -seed flag clears the target collection and writes the corpus before
// running queries. Without -seed, the harness assumes the collection is
// already populated with the gold IDs referenced by queries.
//
// Gold format (JSONL, one per line):
//
//	corpus:  {"id":"mem-001","title":"...","content":"...","tags":["..."]}
//	queries: {"id":"q-001","text":"database N+1 fix","gold":["mem-007"]}
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

type corpusItem struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

type queryItem struct {
	ID   string   `json:"id"`
	Text string   `json:"text"`
	Gold []string `json:"gold"`
}

func main() {
	var (
		solrURL    = flag.String("solr-url", "http://localhost:8983/solr/memories", "Base URL of the memories collection")
		corpusPath = flag.String("corpus", "cmd/solr-mem-bench/testdata/corpus.jsonl", "Path to corpus JSONL")
		queryPath  = flag.String("queries", "cmd/solr-mem-bench/testdata/queries.jsonl", "Path to queries JSONL")
		seed       = flag.Bool("seed", false, "Clear the collection and write the corpus before running queries")
		topK       = flag.Int("topk", 10, "Number of hits to retrieve per query")
		variant    = flag.String("variant", "bm25", "Retrieval variant name (for labeling output)")
	)
	flag.Parse()

	corpus, err := loadCorpus(*corpusPath)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}
	queries, err := loadQueries(*queryPath)
	if err != nil {
		log.Fatalf("load queries: %v", err)
	}
	log.Printf("loaded %d corpus items, %d queries", len(corpus), len(queries))

	client := solr.NewClient(*solrURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if *seed {
		log.Printf("seeding collection at %s (clearing first)", *solrURL)
		if err := seedCorpus(ctx, client, corpus); err != nil {
			log.Fatalf("seed: %v", err)
		}
		// Give Solr a moment to commit.
		time.Sleep(1 * time.Second)
	}

	agg := NewAggregate(1, 3, 5, 10)
	perQuery := make([]queryResult, 0, len(queries))

	start := time.Now()
	for _, q := range queries {
		hits, err := runQuery(ctx, client, q.Text, *topK)
		if err != nil {
			log.Printf("query %s failed: %v", q.ID, err)
			continue
		}
		agg.Add(q.Gold, hits)
		perQuery = append(perQuery, queryResult{ID: q.ID, Text: q.Text, Hits: hits, Gold: q.Gold})
	}
	elapsed := time.Since(start)

	agg.Finalize()
	printReport(*variant, agg, perQuery, elapsed)
}

type queryResult struct {
	ID   string
	Text string
	Hits []string
	Gold []string
}

func loadCorpus(path string) ([]corpusItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []corpusItem
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Bytes()
		if len(line) == 0 {
			continue
		}
		var item corpusItem
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("parse corpus line: %w", err)
		}
		out = append(out, item)
	}
	return out, s.Err()
}

func loadQueries(path string) ([]queryItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []queryItem
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Bytes()
		if len(line) == 0 {
			continue
		}
		var item queryItem
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("parse query line: %w", err)
		}
		out = append(out, item)
	}
	return out, s.Err()
}

// seedCorpus writes the bench corpus to Solr. It scopes its cleanup to
// IDs starting with "bench-" so it is safe to run against a live memories
// collection — it will only ever touch its own namespaced docs.
func seedCorpus(ctx context.Context, client *solr.Client, items []corpusItem) error {
	for _, it := range items {
		if !isBenchID(it.ID) {
			return fmt.Errorf("corpus id %q must start with 'bench-' for safe seeding", it.ID)
		}
	}
	if err := client.DeleteByQuery(ctx, "id:bench-*"); err != nil {
		return fmt.Errorf("clear bench docs: %w", err)
	}
	docs := make([]solr.Document, 0, len(items))
	now := time.Now().UTC()
	for _, it := range items {
		docs = append(docs, solr.Document{
			ID:        it.ID,
			Title:     it.Title,
			Content:   it.Content,
			Tags:      it.Tags,
			CreatedAt: now,
			UpdatedAt: now,
			Lifetime:  "permanent",
			Format:    "prose",
		})
	}
	return client.Add(ctx, docs...)
}

func isBenchID(s string) bool {
	return len(s) > 6 && s[:6] == "bench-"
}

func runQuery(ctx context.Context, client *solr.Client, text string, topK int) ([]string, error) {
	params := solr.QueryParams{
		Query:  text,
		Rows:   topK,
		Fields: []string{"id"},
	}
	resp, err := client.Query(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if id, ok := d["id"].(string); ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func printReport(variant string, agg *Aggregate, results []queryResult, elapsed time.Duration) {
	fmt.Printf("# solr-mem retrieval benchmark\n\n")
	fmt.Printf("Variant: `%s` | Queries: %d | Elapsed: %s\n\n", variant, agg.Queries, elapsed.Round(time.Millisecond))
	fmt.Printf("| Metric | Value |\n|---|---|\n")
	for _, k := range []int{1, 3, 5, 10} {
		if v, ok := agg.RecallAt[k]; ok {
			fmt.Printf("| R@%d | %.3f |\n", k, v)
		}
	}
	fmt.Printf("| MRR | %.3f |\n\n", agg.MRR)

	fmt.Printf("## Per-query breakdown\n\n")
	fmt.Printf("| Query | Gold | R@5 | MRR | Top hits |\n|---|---|---|---|---|\n")
	for _, r := range results {
		top := r.Hits
		if len(top) > 5 {
			top = top[:5]
		}
		fmt.Printf("| %s (%s) | %v | %.2f | %.2f | %v |\n",
			r.ID, truncate(r.Text, 40), r.Gold, RecallAtK(r.Gold, r.Hits, 5), MRR(r.Gold, r.Hits), top)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
