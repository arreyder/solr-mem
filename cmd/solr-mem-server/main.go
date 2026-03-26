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

func main() {
	solrURL := os.Getenv("SOLR_URL")
	if solrURL == "" {
		solrURL = "http://pax89.local:8983/solr/memories"
	}
	solrClient = solr.NewClient(solrURL)

	// Start expiration sweeper
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startSweeper(ctx, solrClient)

	s := newServer()

	// Determine transport mode
	httpAddr := getHTTPAddr()
	if httpAddr != "" {
		runHTTP(s, httpAddr, solrURL)
	} else {
		runStdio(s, solrURL)
	}
}

func newServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "solr-mem",
		Title:   "Solr Memory Store",
		Version: "0.2.0",
	}, &mcp.ServerOptions{
		Instructions: `AI agent memory store backed by Apache Solr. Provides tools to store, search, update, delete, and list memories with full-text search, highlighting, faceting, and similarity matching.

Memories default to permanent lifetime. Use lifetime parameter to control persistence:
- permanent: never expires (default)
- session: tied to a session, cleaned up when session ends
- ephemeral: auto-expires after 1 hour
- temporary: auto-expires after 7 days`,
	})

	for _, def := range ToolSchemas() {
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
