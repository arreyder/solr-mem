package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/arreyder/solr-mem/internal/solr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var solrClient *solr.Client
var codeClient *solr.Client

// indexerControlURL is the base URL of the indexer's force-reindex control
// endpoint. Defaults to the co-located indexer on localhost.
var indexerControlURL = envOrDefault("INDEXER_CONTROL_URL", "http://127.0.0.1:7071")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	solrURL := os.Getenv("SOLR_URL")
	if solrURL == "" {
		solrURL = "http://pax89.local:8983/solr/memories"
	}
	solrClient = solr.NewClient(solrURL)

	codeURL := os.Getenv("SOLR_URL_CODE")
	if codeURL == "" {
		codeURL = "http://pax89.local:8983/solr/code"
	}
	codeClient = solr.NewClient(codeURL)

	// Start expiration sweeper
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startSweeper(ctx, solrClient)

	// Start memory broker
	broker := NewBroker(solrClient, codeClient)
	broker.StartSweeper(ctx)

	s := newServer(broker)

	// Determine transport mode
	httpAddr := getHTTPAddr()
	if httpAddr != "" {
		runHTTP(s, httpAddr, solrURL)
	} else {
		runStdio(s, solrURL)
	}
}

func newServer(broker *Broker) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "solr-mem",
		Title:   "Solr Memory Store",
		Version: "0.4.0",
	}, &mcp.ServerOptions{
		Instructions: `AI agent memory store backed by Apache Solr. Provides tools to store, search, update, delete, and list memories with full-text search, highlighting, faceting, and similarity matching.

Memories default to permanent lifetime. Use lifetime parameter to control persistence:
- permanent: never expires (default)
- session: tied to a session, cleaned up when session ends
- ephemeral: auto-expires after 1 hour
- temporary: auto-expires after 7 days

## Memory Broker

The memory broker helps worker agents stay informed without interrupting their flow:
1. Call observe_work periodically to report what you're doing
2. The broker asynchronously searches memories and code for relevant context
3. Call get_memory_packet at checkpoints to retrieve a compact, ranked bundle
4. Call ack_memory_packet after processing to allow fresh packet generation

## Content Format Guidance

Store memories in compact structured formats to maximize token efficiency for both storage and retrieval. Prefer YAML or key-value pairs over prose paragraphs.

Good (structured, scannable, compact):
` + "```" + `yaml
issue: GetOneGSI1 uses strong consistency (all GSI scans already use weak)
file: pkg/db/db.go:685
fix: add WithWeakConsistency()
effort: trivial
volume: 1852_calls/hr
savings: 50pct_RCU
risk: very_low
tags: [bug, consistency, gsi]
` + "```" + `

Bad (verbose prose, wastes tokens):
"The GetOneGSI1 function in the database package uses strong consistency reads, which is inconsistent with all other GSI scan operations that already use weak consistency. This is a bug that should be fixed by adding the WithWeakConsistency option. The effort required is trivial and the risk is very low."

Guidelines:
- Use YAML, tables, or key-value format for structured findings
- Put the most important info in title (searched with 2x boost)
- Use tags liberally (searched with 1.5x boost, filterable)
- Use metadata field for machine-readable JSON (e.g. file paths, line numbers, metrics)
- Reserve prose for context that cannot be expressed as key-value pairs
- One memory per discrete finding; use relate_memories to link them
- Use bulk_store_memories when storing multiple findings at once`,
	})

	// Register standard tools.
	for _, def := range ToolSchemas() {
		def := def
		tool := *def.Tool
		mcp.AddTool(s, &tool, func(ctx context.Context, req *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
			return invokeTool(ctx, def.Handler, args)
		})
	}

	// Register broker tools.
	for _, def := range BrokerToolSchemas(broker) {
		def := def
		tool := *def.Tool
		mcp.AddTool(s, &tool, func(ctx context.Context, req *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
			return invokeTool(ctx, def.Handler, args)
		})
	}

	return s
}

func runStdio(s *mcp.Server, solrURL string) {
	log.Printf("Starting solr-mem MCP server over stdio (solr: %s)", solrURL)
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Error serving MCP: %v", err)
	}
}

func runHTTP(s *mcp.Server, addr, solrURL string) {
	log.Printf("Starting solr-mem MCP server on %s (solr: %s)", addr, solrURL)

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := solrClient.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// MCP handler for all other paths
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s
	}, nil)
	mux.Handle("/", mcpHandler)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

func getHTTPAddr() string {
	for i, arg := range os.Args[1:] {
		if arg == "--http" {
			if i+1 < len(os.Args[1:]) {
				return os.Args[i+2]
			}
			return ":8080"
		}
	}
	return ""
}

func invokeTool(ctx context.Context, handler ToolHandler, args map[string]any) (*mcp.CallToolResult, any, error) {
	result, err := handler(ctx, args)
	if err != nil {
		return ErrorResult(err), nil, nil
	}

	switch v := result.(type) {
	case ToolOutput:
		res := TextResult(v.Text)
		if v.Structured != nil {
			return res, v.Structured, nil
		}
		return res, nil, nil
	case *ToolOutput:
		res := TextResult(v.Text)
		if v.Structured != nil {
			return res, v.Structured, nil
		}
		return res, nil, nil
	case string:
		return TextResult(v), nil, nil
	case []mcp.Content:
		return &mcp.CallToolResult{Content: v}, nil, nil
	case mcp.Content:
		return &mcp.CallToolResult{Content: []mcp.Content{v}}, nil, nil
	default:
		return ErrorResult(fmt.Errorf("unexpected tool result type %T", v)), nil, nil
	}
}
