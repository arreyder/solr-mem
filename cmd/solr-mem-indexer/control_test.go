package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRequester struct {
	got      string
	queueOK  bool
	requests int
}

func (f *fakeRequester) RequestForceReindex(repo string) bool {
	f.requests++
	f.got = repo
	return f.queueOK
}

func doReindex(t *testing.T, h http.HandlerFunc, method, body string) (int, reindexResponse) {
	t.Helper()
	req := httptest.NewRequest(method, "/reindex", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	var resp reindexResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp
}

func TestReindexHandlerQueues(t *testing.T) {
	fake := &fakeRequester{queueOK: true}
	code, resp := doReindex(t, reindexHandler(fake), http.MethodPost, `{"repo":"/repos/ductone_c1"}`)
	if code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", code)
	}
	if resp.Status != "queued" || resp.Repo != "/repos/ductone_c1" {
		t.Errorf("resp = %+v", resp)
	}
	if fake.got != "/repos/ductone_c1" || fake.requests != 1 {
		t.Errorf("requester got %q (%d calls)", fake.got, fake.requests)
	}
}

func TestReindexHandlerValidation(t *testing.T) {
	fake := &fakeRequester{queueOK: true}
	h := reindexHandler(fake)

	if code, _ := doReindex(t, h, http.MethodGet, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("GET should be 405, got %d", code)
	}
	if code, _ := doReindex(t, h, http.MethodPost, "not json"); code != http.StatusBadRequest {
		t.Errorf("bad JSON should be 400, got %d", code)
	}
	if code, _ := doReindex(t, h, http.MethodPost, `{"repo":"  "}`); code != http.StatusBadRequest {
		t.Errorf("empty repo should be 400, got %d", code)
	}
	if fake.requests != 0 {
		t.Errorf("no valid request should have reached the requester, got %d", fake.requests)
	}
}

func TestReindexHandlerQueueFull(t *testing.T) {
	fake := &fakeRequester{queueOK: false}
	code, resp := doReindex(t, reindexHandler(fake), http.MethodPost, `{"repo":"/repos/x"}`)
	if code != http.StatusServiceUnavailable {
		t.Errorf("queue full should be 503, got %d", code)
	}
	if resp.Status != "error" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestConfigDisablesControlAddr(t *testing.T) {
	t.Setenv("INDEXER_CONTROL_ADDR", "off")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlAddr != "" {
		t.Errorf("ControlAddr = %q, want empty (disabled)", cfg.ControlAddr)
	}
}
