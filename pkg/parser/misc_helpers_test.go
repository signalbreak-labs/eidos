package parser

import "testing"

// TestPathItemIsRefOnly covers every branch of pathItemIsRefOnly: nil, empty
// ref, each non-empty sibling field, and the all-empty (true) case.
func TestPathItemIsRefOnly(t *testing.T) {
	if pathItemIsRefOnly(nil) {
		t.Error("nil path item must not be ref-only")
	}
	if pathItemIsRefOnly(&PathItem{}) {
		t.Error("empty-ref path item must not be ref-only")
	}
	cases := []struct {
		name string
		pi   *PathItem
	}{
		{name: "summary", pi: &PathItem{Ref: "#/x", Summary: "s"}},
		{name: "description", pi: &PathItem{Ref: "#/x", Description: "d"}},
		{name: "get", pi: &PathItem{Ref: "#/x", Get: &Operation{}}},
		{name: "post", pi: &PathItem{Ref: "#/x", Post: &Operation{}}},
		{name: "servers", pi: &PathItem{Ref: "#/x", Servers: []Server{{URL: "https://x"}}}},
		{name: "parameters", pi: &PathItem{Ref: "#/x", Parameters: []Parameter{{Name: "p"}}}},
	}
	for _, tc := range cases {
		if pathItemIsRefOnly(tc.pi) {
			t.Errorf("%s path item must not be ref-only", tc.name)
		}
	}
	if !pathItemIsRefOnly(&PathItem{Ref: "#/x"}) {
		t.Error("pure-ref path item must be ref-only")
	}
}

// TestIsHTTPMethod asserts all eight verbs return true and non-verbs false.
func TestIsHTTPMethod(t *testing.T) {
	for _, m := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
		if !isHTTPMethod(m) {
			t.Errorf("isHTTPMethod(%q) = false", m)
		}
		if !isHTTPMethod(m) {
			t.Errorf("isHTTPMethod(%q) uppercase = false", m)
		}
	}
	for _, m := range []string{"", "connect", "purge", "getter"} {
		if isHTTPMethod(m) {
			t.Errorf("isHTTPMethod(%q) = true, want false", m)
		}
	}
}

// TestScalarValue covers nil, non-scalar, and scalar inputs.
func TestScalarValue(t *testing.T) {
	if v := scalarValue(nil); v != nil {
		t.Errorf("scalarValue(nil) = %v, want nil", v)
	}
	if v := scalarValue(&MapNode{}); v != nil {
		t.Errorf("scalarValue(MapNode) = %v, want nil", v)
	}
	if v := scalarValue(&ScalarNode{Value: "x"}); v != "x" {
		t.Errorf("scalarValue(scalar) = %v, want x", v)
	}
}

// TestNodeLoc covers the nil early-return and the pass-through.
func TestNodeLoc(t *testing.T) {
	if loc := nodeLoc(nil); loc != (SourceLocation{}) {
		t.Errorf("nodeLoc(nil) = %+v, want empty", loc)
	}
	want := SourceLocation{File: "f.yaml", Line: 3, Column: 5}
	n := &ScalarNode{SourceLocation: want}
	if got := nodeLoc(n); got != want {
		t.Errorf("nodeLoc = %+v, want %+v", got, want)
	}
}
