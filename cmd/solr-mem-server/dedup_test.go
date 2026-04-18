package main

import (
	"context"
	"testing"
)

func TestNormalizeOnDuplicate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "skip"},
		{"skip", "skip"},
		{"merge", "merge"},
		{"force", "force"},
		{"garbage", "skip"},
		{"SKIP", "skip"}, // case-sensitive — only lowercase forms are accepted
	}
	for _, c := range cases {
		if got := normalizeOnDuplicate(c.in); got != c.want {
			t.Errorf("normalizeOnDuplicate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFindRecentByHashNoopPaths(t *testing.T) {
	// Empty hash, zero window, or nil client should all short-circuit without error.
	if id, err := findRecentByHash(context.Background(), nil, "", 0); id != "" || err != nil {
		t.Errorf("nil inputs should noop, got id=%q err=%v", id, err)
	}
	if id, err := findRecentByHash(context.Background(), nil, "abc", 300); id != "" || err != nil {
		t.Errorf("nil client should noop, got id=%q err=%v", id, err)
	}
}
