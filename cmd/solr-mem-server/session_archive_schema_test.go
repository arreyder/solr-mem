package main

import "testing"

func TestSessionArchiveToolsAreRegistered(t *testing.T) {
	found := map[string]bool{}
	for _, definition := range ToolSchemas() { found[definition.Tool.Name] = true }
	for _, name := range []string{"archive_omp_session", "search_omp_session_archive"} {
		if !found[name] { t.Fatalf("missing %s", name) }
	}
}
