package main

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestRecallAtKPerfect(t *testing.T) {
	gold := []string{"a", "b"}
	hits := []string{"a", "b", "c", "d", "e"}
	if got := RecallAtK(gold, hits, 5); !approx(got, 1.0) {
		t.Errorf("R@5 should be 1.0, got %f", got)
	}
}

func TestRecallAtKPartial(t *testing.T) {
	gold := []string{"a", "b", "c"}
	hits := []string{"a", "x", "y", "z", "b"}
	// 2 of 3 gold found in top 5.
	if got := RecallAtK(gold, hits, 5); !approx(got, 2.0/3.0) {
		t.Errorf("expected 2/3, got %f", got)
	}
	// Only 1 in top 2.
	if got := RecallAtK(gold, hits, 2); !approx(got, 1.0/3.0) {
		t.Errorf("expected 1/3, got %f", got)
	}
}

func TestRecallAtKNoHits(t *testing.T) {
	gold := []string{"a"}
	hits := []string{"x", "y", "z"}
	if got := RecallAtK(gold, hits, 3); !approx(got, 0) {
		t.Errorf("expected 0, got %f", got)
	}
}

func TestRecallAtKKLargerThanHits(t *testing.T) {
	gold := []string{"a"}
	hits := []string{"a", "b"}
	if got := RecallAtK(gold, hits, 10); !approx(got, 1.0) {
		t.Errorf("k > len(hits) should not error, got %f", got)
	}
}

func TestRecallAtKEmptyGold(t *testing.T) {
	if got := RecallAtK(nil, []string{"a"}, 5); got != 0 {
		t.Errorf("no gold → 0, got %f", got)
	}
}

func TestMRRFirstPosition(t *testing.T) {
	gold := []string{"a"}
	hits := []string{"a", "b", "c"}
	if got := MRR(gold, hits); !approx(got, 1.0) {
		t.Errorf("first-rank gold → 1.0, got %f", got)
	}
}

func TestMRRThirdPosition(t *testing.T) {
	gold := []string{"c"}
	hits := []string{"a", "b", "c"}
	if got := MRR(gold, hits); !approx(got, 1.0/3.0) {
		t.Errorf("rank 3 → 1/3, got %f", got)
	}
}

func TestMRRNoMatch(t *testing.T) {
	gold := []string{"z"}
	hits := []string{"a", "b", "c"}
	if got := MRR(gold, hits); got != 0 {
		t.Errorf("no match → 0, got %f", got)
	}
}

func TestMRRMultipleGoldTakesBest(t *testing.T) {
	// Two gold items; MRR uses the best (earliest) rank.
	gold := []string{"c", "a"}
	hits := []string{"a", "b", "c"}
	if got := MRR(gold, hits); !approx(got, 1.0) {
		t.Errorf("best rank should win: expected 1.0, got %f", got)
	}
}

func TestAggregateEndToEnd(t *testing.T) {
	agg := NewAggregate(5, 10)
	agg.Add([]string{"a"}, []string{"a", "b", "c"})      // R@5=1, R@10=1, MRR=1
	agg.Add([]string{"x"}, []string{"a", "b", "c"})      // all zero
	agg.Add([]string{"c"}, []string{"a", "b", "c"})      // R@5=1, R@10=1, MRR=1/3
	agg.Finalize()

	// Means across 3 queries: R@5 = 2/3, R@10 = 2/3, MRR = (1 + 0 + 1/3)/3 = 4/9
	if !approx(agg.RecallAt[5], 2.0/3.0) {
		t.Errorf("R@5 mean expected 2/3, got %f", agg.RecallAt[5])
	}
	if !approx(agg.RecallAt[10], 2.0/3.0) {
		t.Errorf("R@10 mean expected 2/3, got %f", agg.RecallAt[10])
	}
	if !approx(agg.MRR, 4.0/9.0) {
		t.Errorf("MRR mean expected 4/9, got %f", agg.MRR)
	}
}

func TestAggregateEmpty(t *testing.T) {
	agg := NewAggregate(5)
	agg.Finalize() // should not panic on zero queries
	if agg.MRR != 0 || agg.RecallAt[5] != 0 {
		t.Errorf("empty aggregate should stay zero")
	}
}
