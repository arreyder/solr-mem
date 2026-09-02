package solr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSolr serves /admin/ping and /admin/cores?action=STATUS for the core
// named "code", and records whether a CREATE was ever attempted.
type fakeSolr struct {
	pingStatus  int    // status code returned by /solr/code/admin/ping
	statusJSON  string // body returned by /solr/admin/cores?action=STATUS
	createCalls int
}

func (f *fakeSolr) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/solr/code/admin/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(f.pingStatus)
	})
	mux.HandleFunc("/solr/admin/cores", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("action") == "CREATE" {
			f.createCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.statusJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A core that is registered but failed to load answers every request with a
// 500. CREATEing over it deletes its core.properties and orphans the index on
// disk, so EnsureCollection must refuse.
func TestEnsureCollection_RefusesCreateOnInitFailure(t *testing.T) {
	f := &fakeSolr{
		pingStatus: http.StatusInternalServerError,
		statusJSON: `{"initFailures":{"code":"Error opening new searcher"},"status":{"code":{}}}`,
	}
	srv := f.start(t)

	err := NewClient(srv.URL + "/solr/code").EnsureCollection(context.Background(), "")
	if err == nil {
		t.Fatal("expected an error for a core that failed to initialize")
	}
	if !strings.Contains(err.Error(), "Error opening new searcher") {
		t.Errorf("error should surface Solr's init failure, got: %v", err)
	}
	if f.createCalls != 0 {
		t.Errorf("must not CREATE over a broken core, got %d CREATE calls", f.createCalls)
	}
}

// A core Solr has never heard of comes back as an empty status entry; that is
// the one case where CREATE is the right move.
func TestEnsureCollection_CreatesMissingCore(t *testing.T) {
	f := &fakeSolr{
		pingStatus: http.StatusInternalServerError,
		statusJSON: `{"initFailures":{},"status":{"code":{}}}`,
	}
	srv := f.start(t)

	if err := NewClient(srv.URL + "/solr/code").EnsureCollection(context.Background(), ""); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	if f.createCalls != 1 {
		t.Errorf("expected exactly 1 CREATE for a missing core, got %d", f.createCalls)
	}
}

// Registered but not yet answering ping (loading/warming) is not a reason to
// create anything.
func TestEnsureCollection_LeavesLoadingCoreAlone(t *testing.T) {
	f := &fakeSolr{
		pingStatus: http.StatusServiceUnavailable,
		statusJSON: `{"initFailures":{},"status":{"code":{"name":"code","instanceDir":"/var/solr/data/code"}}}`,
	}
	srv := f.start(t)

	if err := NewClient(srv.URL + "/solr/code").EnsureCollection(context.Background(), ""); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	if f.createCalls != 0 {
		t.Errorf("must not CREATE over a registered core, got %d CREATE calls", f.createCalls)
	}
}

// A healthy core short-circuits on ping without touching the admin API.
func TestEnsureCollection_HealthyCoreIsNoop(t *testing.T) {
	f := &fakeSolr{pingStatus: http.StatusOK}
	srv := f.start(t)

	if err := NewClient(srv.URL + "/solr/code").EnsureCollection(context.Background(), ""); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	if f.createCalls != 0 {
		t.Errorf("healthy core must not trigger CREATE, got %d", f.createCalls)
	}
}
