package transformer

import (
	"fmt"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
)

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
// A dropped construct is never silent: when diags is non-nil and a name
// collision drops a later entry, a Warning naming the duplicate is appended
// (AGENTS.md "fail loud, never silently").
func dedupByName[T any](items []T, nameOf func(T) string, diags *diagnostics.Diagnostics) []T {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := items[:0]
	for _, it := range items {
		name := nameOf(it)
		if _, ok := seen[name]; ok {
			if diags != nil {
				*diags = append(*diags, diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  fmt.Sprintf("duplicate %T name %q dropped", it, name),
					Detail:   fmt.Sprintf("A construct named %q collides with an earlier construct of the same name; the later one is dropped so the two cannot emit colliding Terraform type names.", name),
				})
			}
			continue
		}
		seen[name] = struct{}{}
		out = append(out, it)
	}
	return out
}
