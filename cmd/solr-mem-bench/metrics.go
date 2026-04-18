package main

// RecallAtK returns the fraction of gold IDs that appear in the top-K hits.
// gold is the set of relevant IDs for a query; hits is the ranked result list.
func RecallAtK(gold, hits []string, k int) float64 {
	if len(gold) == 0 {
		return 0
	}
	if k <= 0 || k > len(hits) {
		k = len(hits)
	}
	goldSet := make(map[string]struct{}, len(gold))
	for _, g := range gold {
		goldSet[g] = struct{}{}
	}
	found := 0
	for i := 0; i < k; i++ {
		if _, ok := goldSet[hits[i]]; ok {
			found++
		}
	}
	return float64(found) / float64(len(gold))
}

// MRR returns the reciprocal rank of the first gold hit in the ranked list,
// or 0 if no gold item was retrieved. Ranks are 1-indexed.
func MRR(gold, hits []string) float64 {
	if len(gold) == 0 || len(hits) == 0 {
		return 0
	}
	goldSet := make(map[string]struct{}, len(gold))
	for _, g := range gold {
		goldSet[g] = struct{}{}
	}
	for i, h := range hits {
		if _, ok := goldSet[h]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// Aggregate holds per-run averaged metrics.
type Aggregate struct {
	Queries  int
	RecallAt map[int]float64 // k -> mean recall
	MRR      float64
}

// NewAggregate accumulates per-query metrics over a set of queries.
func NewAggregate(ks ...int) *Aggregate {
	m := make(map[int]float64, len(ks))
	for _, k := range ks {
		m[k] = 0
	}
	return &Aggregate{RecallAt: m}
}

// Add records one query's hits against its gold.
func (a *Aggregate) Add(gold, hits []string) {
	a.Queries++
	for k := range a.RecallAt {
		a.RecallAt[k] += RecallAtK(gold, hits, k)
	}
	a.MRR += MRR(gold, hits)
}

// Finalize divides sums by query count, producing mean metrics.
func (a *Aggregate) Finalize() {
	if a.Queries == 0 {
		return
	}
	n := float64(a.Queries)
	for k, v := range a.RecallAt {
		a.RecallAt[k] = v / n
	}
	a.MRR /= n
}
