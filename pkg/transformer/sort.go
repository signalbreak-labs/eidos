package transformer

import "sort"

// sortedKeys returns the keys of m in lexicographic order. It is the
// deterministic replacement for unguarded `for k := range m` iteration, which
// yields keys in random order per run and makes downstream "sorted by name"
// output depend on map iteration seed (M-39).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupByName returns items with later entries whose name (extracted by nameOf)
// duplicates an earlier entry dropped, preserving the first occurrence. Combined
// with a stable name sort over deterministically-ordered input, the result is
// fully deterministic and free of the type-name collisions that arise when two
// paths map to the same name (e.g. /v1/pets and /v2/pets both -> "pets") (M-39).
func dedupByName[T any](items []T, nameOf func(T) string) []T {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := items[:0]
	for _, it := range items {
		name := nameOf(it)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, it)
	}
	return out
}
