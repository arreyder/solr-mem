package main

import "testing"

type divItem struct {
	id      string
	session string
}

func ids(items []divItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.id
	}
	return out
}

func sessionKey(it divItem) string { return it.session }

func TestDiversifyBySessionCapsPerSession(t *testing.T) {
	in := []divItem{
		{"a", "s1"},
		{"b", "s1"},
		{"c", "s1"}, // should be dropped at cap=2
		{"d", "s2"},
		{"e", "s1"}, // dropped
		{"f", "s2"},
		{"g", "s3"},
	}
	got := diversifyBySession(in, sessionKey, 2)
	want := []string{"a", "b", "d", "f", "g"}
	if !equalStrings(ids(got), want) {
		t.Errorf("cap=2: got %v, want %v", ids(got), want)
	}
}

func TestDiversifyBySessionPreservesOrder(t *testing.T) {
	// Top-ranked items should be kept over later same-session items.
	in := []divItem{
		{"top", "s1"},
		{"mid", "s2"},
		{"also1", "s1"},
		{"low1", "s1"}, // dropped at cap=2
		{"low2", "s1"}, // dropped
	}
	got := diversifyBySession(in, sessionKey, 2)
	want := []string{"top", "mid", "also1"}
	if !equalStrings(ids(got), want) {
		t.Errorf("order: got %v, want %v", ids(got), want)
	}
}

func TestDiversifyBySessionEmptyKeyNotCapped(t *testing.T) {
	// Items with empty key (e.g. code docs) pass through without counting.
	in := []divItem{
		{"a", ""},
		{"b", ""},
		{"c", ""},
		{"d", "s1"},
		{"e", "s1"},
		{"f", "s1"}, // dropped
	}
	got := diversifyBySession(in, sessionKey, 2)
	want := []string{"a", "b", "c", "d", "e"}
	if !equalStrings(ids(got), want) {
		t.Errorf("empty key: got %v, want %v", ids(got), want)
	}
}

func TestDiversifyBySessionDisabled(t *testing.T) {
	in := []divItem{{"a", "s1"}, {"b", "s1"}, {"c", "s1"}}
	if got := diversifyBySession(in, sessionKey, 0); len(got) != 3 {
		t.Errorf("cap=0 should passthrough, got %d items", len(got))
	}
	if got := diversifyBySession(in, sessionKey, -1); len(got) != 3 {
		t.Errorf("cap<0 should passthrough, got %d items", len(got))
	}
}

func TestDiversifyBySessionEmpty(t *testing.T) {
	if got := diversifyBySession[divItem](nil, sessionKey, 2); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
