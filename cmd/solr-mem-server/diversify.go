package main

// diversifyBySession caps the number of items per session in a ranked slice.
// Items with an empty group key are passed through without counting — callers
// use this for hits that have no natural grouping (e.g. code docs without a
// session_id).
//
// cap <= 0 disables diversification and returns the input unchanged.
func diversifyBySession[T any](items []T, key func(T) string, cap int) []T {
	if cap <= 0 || len(items) == 0 {
		return items
	}
	counts := make(map[string]int, len(items))
	out := make([]T, 0, len(items))
	for _, item := range items {
		k := key(item)
		if k == "" {
			out = append(out, item)
			continue
		}
		if counts[k] >= cap {
			continue
		}
		counts[k]++
		out = append(out, item)
	}
	return out
}
