// Package retrieval contains the search-side primitives used by the
// solr-mem server to combine multiple retrieval signals.
package retrieval

// Stream is one ranked list of document IDs from a single retrieval source
// (e.g. BM25 keyword match, dense vector KNN, graph walk). Order matters:
// position 0 is the top-ranked hit.
type Stream struct {
	Name string
	IDs  []string
}

// RRFOption configures the fuser.
type RRFOption func(*rrfConfig)

type rrfConfig struct {
	k      int
	topK   int
	weight map[string]float64
}

// RRFWithK sets the rank-damping constant. Larger k compresses scores across
// streams, making them count more evenly. The canonical default is 60 (per
// the original Cormack et al. paper).
func RRFWithK(k int) RRFOption {
	return func(c *rrfConfig) {
		if k > 0 {
			c.k = k
		}
	}
}

// RRFWithTopK truncates the returned fused list. 0 means "return everything
// that appeared in any stream".
func RRFWithTopK(n int) RRFOption {
	return func(c *rrfConfig) {
		c.topK = n
	}
}

// RRFWithWeight overrides the default 1.0 weight for a named stream. Useful
// if one signal is known to be higher-quality for a given query class.
// Streams not in the map keep weight 1.0.
func RRFWithWeight(weights map[string]float64) RRFOption {
	return func(c *rrfConfig) {
		c.weight = weights
	}
}

// RRFScored pairs an ID with its fused score. Sorted high-to-low on return
// from Fuse.
type RRFScored struct {
	ID    string
	Score float64
}

// Fuse combines multiple ranked streams with Reciprocal Rank Fusion:
//
//	score(d) = sum over streams i of  weight_i / (k + rank_i(d))
//
// rank is 1-indexed. Streams with fewer results than another don't penalize
// the missing IDs — they simply don't contribute. The returned list is
// sorted by fused score descending; ties break on lexicographic ID for
// deterministic output.
func Fuse(streams []Stream, opts ...RRFOption) []RRFScored {
	cfg := rrfConfig{k: 60}
	for _, opt := range opts {
		opt(&cfg)
	}

	scores := make(map[string]float64)
	for _, s := range streams {
		w := 1.0
		if cfg.weight != nil {
			if v, ok := cfg.weight[s.Name]; ok {
				w = v
			}
		}
		for rank, id := range s.IDs {
			if id == "" {
				continue
			}
			scores[id] += w / float64(cfg.k+rank+1)
		}
	}

	out := make([]RRFScored, 0, len(scores))
	for id, sc := range scores {
		out = append(out, RRFScored{ID: id, Score: sc})
	}
	// Sort by score desc, then ID asc for determinism.
	sortFused(out)

	if cfg.topK > 0 && len(out) > cfg.topK {
		out = out[:cfg.topK]
	}
	return out
}

// FuseIDs is a thin wrapper around Fuse that returns just the ranked IDs.
func FuseIDs(streams []Stream, opts ...RRFOption) []string {
	scored := Fuse(streams, opts...)
	ids := make([]string, len(scored))
	for i, s := range scored {
		ids[i] = s.ID
	}
	return ids
}

// sortFused sorts in-place: descending score, ascending ID for ties.
func sortFused(xs []RRFScored) {
	// Simple selection sort is fine for small N; most memory queries return
	// <= 50 hits. Using sort.Slice would pull in sort; inlined for zero-dep.
	for i := 0; i < len(xs); i++ {
		best := i
		for j := i + 1; j < len(xs); j++ {
			if xs[j].Score > xs[best].Score ||
				(xs[j].Score == xs[best].Score && xs[j].ID < xs[best].ID) {
				best = j
			}
		}
		if best != i {
			xs[i], xs[best] = xs[best], xs[i]
		}
	}
}
