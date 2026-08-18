package parser

import (
	"reflect"
	"testing"
)

func TestNodeNativeSlice(t *testing.T) {
	seq := &SequenceNode{
		Items: []Node{
			&ScalarNode{Value: "a"},
			&ScalarNode{Value: 42},
			&MapNode{
				Entries: []MapEntry{
					{Key: &ScalarNode{Value: "k"}, Value: &ScalarNode{Value: "v"}},
				},
			},
		},
	}

	got := nodeNativeSlice(seq)
	want := []any{"a", 42, map[string]any{"k": "v"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nodeNativeSlice() = %v, want %v", got, want)
	}
}

func TestNodeNativeSlice_NonSequence(t *testing.T) {
	if got := nodeNativeSlice(&ScalarNode{Value: "x"}); got != nil {
		t.Errorf("expected nil for non-sequence, got %v", got)
	}
}

// TestScalarStringMap exercises the shipped v30Converter.scalarStringMap
// helper (which forEachEntry + nodeString underpin) rather than a test-only
// duplicate. The prior test defined its own nodeStringMap and asserted nothing
// about shipped code (L-92).
func TestScalarStringMap(t *testing.T) {
	c := &v30Converter{version: Version3_0, budget: NewBudget(DefaultLimits())}
	m := &MapNode{
		Entries: []MapEntry{
			{Key: &ScalarNode{Value: "a"}, Value: &ScalarNode{Value: "one"}},
			// A non-string value must be omitted from the map and emit a warning.
			{Key: &ScalarNode{Value: "b"}, Value: &ScalarNode{Value: 2}},
			// A non-string key (integer) is passed through via its raw source
			// text, matching the v2 keyString behavior (C-1).
			{Key: &ScalarNode{Value: 3, Raw: "3"}, Value: &ScalarNode{Value: "three"}},
			// A non-string key with no raw text is skipped (L-74).
			{Key: &ScalarNode{Value: 4}, Value: &ScalarNode{Value: "four"}},
		},
	}
	got := c.scalarStringMap(m, "test")
	want := map[string]string{"a": "one", "3": "three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scalarStringMap() = %v, want %v", got, want)
	}
	if len(c.diags) == 0 {
		t.Errorf("expected a type-mismatch warning for the non-string value, got none")
	}
}
