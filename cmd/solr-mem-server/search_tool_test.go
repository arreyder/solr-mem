package main

import (
	"reflect"
	"testing"
)

func TestMatchToMM(t *testing.T) {
	cases := map[string]string{
		"":      "",
		"most":  "",
		"MOST":  "",
		" any ": "1",
		"any":   "1",
		"or":    "1",
		"all":   "100%",
		"AND":   "100%",
		"bogus": "", // unknown falls back to the default
	}
	for in, want := range cases {
		if got := matchToMM(in); got != want {
			t.Errorf("matchToMM(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureFields(t *testing.T) {
	// Required fields are appended when missing.
	got := ensureFields([]string{"title", "importance"}, "id", "session_id")
	want := []string{"title", "importance", "id", "session_id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Already-present required fields aren't duplicated; blanks dropped; order kept.
	got = ensureFields([]string{"id", "", "title", "id"}, "id")
	want = []string{"id", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup: got %v, want %v", got, want)
	}
}
