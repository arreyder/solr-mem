package retrieval

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestFuseSingleStreamPreservesOrder(t *testing.T) {
	streams := []Stream{
		{Name: "bm25", IDs: []string{"a", "b", "c"}},
	}
	got := FuseIDs(streams)
	want := []string{"a", "b", "c"}
	if !equal(got, want) {
		t.Errorf("single stream order: got %v, want %v", got, want)
	}
}

func TestFuseBothStreamsAgreeTopGoesFirst(t *testing.T) {
	streams := []Stream{
		{Name: "bm25", IDs: []string{"a", "b", "c"}},
		{Name: "vec", IDs: []string{"a", "b", "c"}},
	}
	got := FuseIDs(streams)
	if len(got) != 3 || got[0] != "a" {
		t.Errorf("full agreement should put a first, got %v", got)
	}
}

func TestFuseCrossStreamBoostsHitsInBoth(t *testing.T) {
	// c appears in both but never at the top; a appears once; b appears once.
	// Canonical RRF with k=60: score(c) = 1/62 + 1/62 = ~0.0323
	//                          score(a) = 1/61          = ~0.0164
	//                          score(b) = 1/61          = ~0.0164
	// So c should rank first.
	streams := []Stream{
		{Name: "bm25", IDs: []string{"a", "c"}},
		{Name: "vec", IDs: []string{"b", "c"}},
	}
	got := FuseIDs(streams)
	if got[0] != "c" {
		t.Errorf("cross-stream hit should win: got %v", got)
	}
}

func TestFuseKnownScores(t *testing.T) {
	// Verify exact RRF formula with k=10 for cleaner numbers.
	streams := []Stream{
		{Name: "s1", IDs: []string{"x"}},
		{Name: "s2", IDs: []string{"x"}},
	}
	got := Fuse(streams, RRFWithK(10))
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	// score(x) = 1/(10+1) + 1/(10+1) = 2/11
	want := 2.0 / 11.0
	if !approx(got[0].Score, want) {
		t.Errorf("expected score %f, got %f", want, got[0].Score)
	}
}

func TestFuseWeights(t *testing.T) {
	// Equal-rank hits should split by weight.
	streams := []Stream{
		{Name: "high", IDs: []string{"a"}},
		{Name: "low", IDs: []string{"b"}},
	}
	got := Fuse(streams, RRFWithK(10), RRFWithWeight(map[string]float64{"high": 2.0, "low": 0.5}))
	var scoreA, scoreB float64
	for _, s := range got {
		if s.ID == "a" {
			scoreA = s.Score
		}
		if s.ID == "b" {
			scoreB = s.Score
		}
	}
	// score(a) = 2.0/11 = 0.1818..., score(b) = 0.5/11 = 0.0454...
	if scoreA <= scoreB {
		t.Errorf("higher weight should produce higher score: a=%f b=%f", scoreA, scoreB)
	}
	if !approx(scoreA, 2.0/11.0) {
		t.Errorf("expected score(a)=2/11, got %f", scoreA)
	}
}

func TestFuseTopK(t *testing.T) {
	streams := []Stream{
		{Name: "s", IDs: []string{"a", "b", "c", "d", "e"}},
	}
	got := FuseIDs(streams, RRFWithTopK(3))
	if len(got) != 3 {
		t.Errorf("expected 3, got %d", len(got))
	}
}

func TestFuseEmptyInput(t *testing.T) {
	if got := FuseIDs(nil); len(got) != 0 {
		t.Errorf("nil input should produce empty, got %v", got)
	}
	if got := FuseIDs([]Stream{{Name: "s"}}); len(got) != 0 {
		t.Errorf("empty stream should produce empty, got %v", got)
	}
}

func TestFuseIgnoresEmptyIDs(t *testing.T) {
	streams := []Stream{
		{Name: "s", IDs: []string{"a", "", "b"}},
	}
	got := FuseIDs(streams)
	// Empty IDs are skipped; remaining ranks keep their original positions.
	// So "a" is rank 0, "b" is rank 2 (even though "a" is next to it).
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("empty IDs should be skipped: got %v", got)
	}
}

func TestFuseDeterministicTieBreak(t *testing.T) {
	// Two IDs with identical scores should sort by ID ascending for stable output.
	streams := []Stream{
		{Name: "s1", IDs: []string{"zebra"}},
		{Name: "s2", IDs: []string{"apple"}},
	}
	got := FuseIDs(streams)
	if len(got) != 2 || got[0] != "apple" {
		t.Errorf("tie break should favor lexicographically-smaller ID: got %v", got)
	}
}

func TestFuseMissingFromOneStream(t *testing.T) {
	// Asymmetric streams: "only-a" appears only in s1. Its score should
	// come only from that stream's rank.
	streams := []Stream{
		{Name: "s1", IDs: []string{"only-a", "shared"}},
		{Name: "s2", IDs: []string{"shared", "only-b"}},
	}
	got := Fuse(streams, RRFWithK(10))
	// shared is rank 1 in both: 1/11 + 1/12 = ~0.174
	// only-a is rank 0 in s1: 1/11 = 0.0909
	// only-b is rank 1 in s2: 1/12 = 0.0833
	if got[0].ID != "shared" {
		t.Errorf("shared should rank first: got %v", got)
	}
}

func equal(a, b []string) bool {
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
