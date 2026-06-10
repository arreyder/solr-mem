package main

import (
	"reflect"
	"testing"
)

func TestCollectRetrievableIDs(t *testing.T) {
	docs := []map[string]any{
		{"id": "a", "title": "x"},
		{"title": "no id"}, // skipped
		{"id": ""},         // skipped (empty)
		{"id": "b"},
	}

	got := collectRetrievableIDs(docs, true)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("track=true: got %v, want %v", got, want)
	}

	if got := collectRetrievableIDs(docs, false); got != nil {
		t.Errorf("track=false must return nil, got %v", got)
	}
	if got := collectRetrievableIDs(nil, true); got != nil {
		t.Errorf("empty docs must return nil, got %v", got)
	}
}
