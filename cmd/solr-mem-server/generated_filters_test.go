package main

import (
	"strings"
	"testing"
)

func TestGeneratedExclusionFilter(t *testing.T) {
	if got := generatedExclusionFilter(false); got != "" {
		t.Errorf("disabled should return empty, got %q", got)
	}

	fq := generatedExclusionFilter(true)
	if !strings.HasPrefix(fq, "*:* ") {
		t.Errorf("filter must start with *:* base, got %q", fq)
	}
	if !strings.Contains(fq, `-tags:"vendor"`) {
		t.Error("filter must exclude the vendor tag")
	}
	for _, pat := range generatedFilePatterns {
		if !strings.Contains(fq, "-file_path:"+pat) {
			t.Errorf("filter missing exclusion for %q", pat)
		}
	}
	// Every clause beyond the base must be a negative (exclusion) clause.
	for _, clause := range strings.Fields(fq)[1:] {
		if !strings.HasPrefix(clause, "-") {
			t.Errorf("non-negative clause in exclusion filter: %q", clause)
		}
	}
}
