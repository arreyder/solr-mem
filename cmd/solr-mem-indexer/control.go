package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// reindexRequester is the subset of Watcher the control server needs.
type reindexRequester interface {
	RequestForceReindex(repoPath string) bool
}

type reindexRequest struct {
	Repo string `json:"repo"`
}

type reindexResponse struct {
	Status string `json:"status"`
	Repo   string `json:"repo,omitempty"`
	Error  string `json:"error,omitempty"`
}

// startControlServer runs a small localhost HTTP server exposing POST /reindex,
// which enqueues a force-reindex for a repo onto the watcher. It is intended to
// be reachable only by the co-located MCP server (default bind 127.0.0.1).
func startControlServer(ctx context.Context, addr string, w reindexRequester) {
	mux := http.NewServeMux()
	mux.HandleFunc("/reindex", reindexHandler(w))
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, http.StatusOK, reindexResponse{Status: "ok"})
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	go func() {
		log.Printf("Control endpoint listening on %s (POST /reindex)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Control endpoint error: %v", err)
		}
	}()
}

func reindexHandler(w reindexRequester) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(rw, http.StatusMethodNotAllowed, reindexResponse{Status: "error", Error: "use POST"})
			return
		}
		var req reindexRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(rw, http.StatusBadRequest, reindexResponse{Status: "error", Error: "invalid JSON body"})
			return
		}
		req.Repo = strings.TrimSpace(req.Repo)
		if req.Repo == "" {
			writeJSON(rw, http.StatusBadRequest, reindexResponse{Status: "error", Error: "repo is required"})
			return
		}
		if !w.RequestForceReindex(req.Repo) {
			writeJSON(rw, http.StatusServiceUnavailable, reindexResponse{Status: "error", Repo: req.Repo, Error: "reindex queue full, try again shortly"})
			return
		}
		writeJSON(rw, http.StatusAccepted, reindexResponse{Status: "queued", Repo: req.Repo})
	}
}

func writeJSON(rw http.ResponseWriter, code int, body any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(body)
}
