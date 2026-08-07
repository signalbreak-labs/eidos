package parser

import (
	"strconv"
	"strings"
)

// ---------- generic node helpers ----------

// asString extracts a string value from a scalar node. It returns the typed
// string when available and reports failure for non-string scalars so callers
// can emit a type-mismatch diagnostic.
func asString(n Node) (string, bool) {
	s, ok := n.(*ScalarNode)
	if !ok {
		return "", false
	}
	v, ok := s.Value.(string)
	if !ok {
		return "", false
	}
	return v, true
}

func nodeString(n Node) (string, bool) {
	return asString(n)
}

func nodeBool(n Node) (bool, bool) {
	s, ok := n.(*ScalarNode)
	if !ok {
		return false, false
	}
	v, ok := s.Value.(bool)
	return v, ok
}

// nodeFloat converts a numeric node to float64. It handles int64/uint64 in
// addition to float64 so that large integer values from YAML/JSON do not wrap,
// and parses string values via strconv.ParseFloat as a best-effort coercion.
// Malformed numeric strings fall back to (0, false) without emitting a
// diagnostic.
func nodeFloat(n Node) (float64, bool) {
	s, ok := n.(*ScalarNode)
	if !ok {
		return 0, false
	}
	switch v := s.Value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// nodeInt converts a numeric node to int. It truncates non-integer floats, so
// a YAML scalar such as 3.9 becomes 3. This matches the current best-effort
// policy because the lexer represents YAML integers as float64.
func nodeInt(n Node) (int, bool) {
	f, ok := nodeFloat(n)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func nodeToNative(n Node) any {
	if n == nil {
		return nil
	}
	switch v := n.(type) {
	case *ScalarNode:
		return v.Value
	case *MapNode:
		m := make(map[string]any, len(v.Entries))
		forEachEntry(v, func(key string, value Node) {
			m[key] = nodeToNative(value)
		})
		return m
	case *SequenceNode:
		out := make([]any, 0, len(v.Items))
		for _, item := range v.Items {
			out = append(out, nodeToNative(item))
		}
		return out
	}
	return nil
}

// nodeToNativeMap converts a MapNode into map[string]any.
func nodeToNativeMap(n Node) map[string]any {
	m, ok := n.(*MapNode)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(m.Entries))
	forEachEntry(m, func(key string, value Node) {
		out[key] = nodeToNative(value)
	})
	return out
}

func nodeNativeSlice(n Node) []any {
	s, ok := n.(*SequenceNode)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(s.Items))
	for _, item := range s.Items {
		out = append(out, nodeToNative(item))
	}
	return out
}

// nodeExtensions extracts all `x-*` keys from a MapNode into a map suitable
// for populating the Extensions field on model structs.
func nodeExtensions(n Node) map[string]any {
	m, ok := n.(*MapNode)
	if !ok {
		return nil
	}
	ext := make(map[string]any)
	forEachEntry(m, func(key string, value Node) {
		if strings.HasPrefix(key, "x-") {
			ext[key] = nodeToNative(value)
		}
	})
	if len(ext) == 0 {
		return nil
	}
	return ext
}

// forEachEntry iterates over the entries of a MapNode, skipping nil keys and
// invoking fn for each key/value pair. It centralizes the boilerplate repeated
// across the converter methods.
//
// Non-string scalar keys (e.g. a YAML integer key like `1: foo`) are skipped
// rather than passed to fn: there is no string field name to bind to, and
// calling fn with an empty key would silently overwrite any other keyless
// entry under "" (L-74). forEachEntry has no diagnostics channel, so such
// entries are dropped without a diagnostic; callers that need to surface them
// should iterate m.Entries directly.
func forEachEntry(m *MapNode, fn func(key string, value Node)) {
	for _, e := range m.Entries {
		if e.Key == nil {
			continue
		}
		key, ok := asString(e.Key)
		if !ok {
			continue
		}
		fn(key, e.Value)
	}
}
