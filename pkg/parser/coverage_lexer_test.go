package parser

import (
	"strings"
	"testing"
)

// TestLoadFileAsJSON_Valid verifies LoadFileAsJSON parses a JSON document
// directly (the explicit-JSON entry point used by the API layer).
func TestLoadFileAsJSON_Valid(t *testing.T) {
	node, err := LoadFileAsJSON("spec.json", []byte(`{"openapi":"3.0.1","paths":{}}`))
	if err != nil {
		t.Fatalf("LoadFileAsJSON: %v", err)
	}
	m, ok := node.(*MapNode)
	if !ok {
		t.Fatalf("expected *MapNode, got %T", node)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.Entries))
	}
}

// TestLoadFileAsJSON_Invalid verifies LoadFileAsJSON surfaces a parse error
// attributed to the file name.
func TestLoadFileAsJSON_Invalid(t *testing.T) {
	_, err := LoadFileAsJSON("spec.json", []byte(`{"openapi": `))
	if err == nil {
		t.Fatal("expected a parse error for truncated JSON")
	}
	if !strings.Contains(err.Error(), "spec.json") {
		t.Errorf("error %q does not attribute the failure to the file name", err)
	}
}

// TestYAMLExplicitKey verifies the `? key` / `: value` explicit-key syntax
// parses into a mapping entry. The mapping's first line must carry a colon for
// parseBlock to route to parseMapping, which is where explicit keys are
// recognized (M-36).
func TestYAMLExplicitKey(t *testing.T) {
	data := []byte("a: 1\n? explicit\n: value\n")
	node, err := LoadFileAsYAML("spec.yaml", data)
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m, ok := node.(*MapNode)
	if !ok {
		t.Fatalf("expected *MapNode, got %T", node)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.Entries))
	}
	if m.Entries[1].Key.Value != "explicit" {
		t.Errorf("expected key explicit, got %q", m.Entries[1].Key.Value)
	}
	if v, ok := m.Entries[1].Value.(*ScalarNode); !ok || v.Value != "value" {
		t.Errorf("expected scalar value, got %+v", m.Entries[1].Value)
	}
}

// TestYAMLExplicitKeyNoValue verifies an explicit key with no following value
// is rejected fail-loud.
func TestYAMLExplicitKeyNoValue(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? orphan\n"))
	if err == nil {
		t.Fatal("expected an error for an explicit key with no value")
	}
	if !strings.Contains(err.Error(), "explicit key has no value") {
		t.Errorf("error %q does not describe the missing value", err)
	}
}

// TestYAMLExplicitKeyValueNotColon verifies an explicit-key value line that
// does not start with ':' is rejected.
func TestYAMLExplicitKeyValueNotColon(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\nvalue\n"))
	if err == nil {
		t.Fatal("expected an error for an explicit-key value not starting with ':'")
	}
	if !strings.Contains(err.Error(), "must start with ':'") {
		t.Errorf("error %q does not describe the missing colon", err)
	}
}

// TestYAMLExplicitKeyMappingValue verifies an explicit key whose value is a
// mapping entry (e.g. "type: object") parses into a nested MapNode.
func TestYAMLExplicitKeyMappingValue(t *testing.T) {
	data := []byte("a: 1\n? key\n: type: object\n  properties: {}\n")
	node, err := LoadFileAsYAML("spec.yaml", data)
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	val, ok := m.Entries[1].Value.(*MapNode)
	if !ok {
		t.Fatalf("expected explicit-key value to be a MapNode, got %T", m.Entries[1].Value)
	}
	if len(val.Entries) != 2 {
		t.Fatalf("expected 2 entries in the value mapping, got %d", len(val.Entries))
	}
}

// TestYAMLExplicitKeySiblingMerge verifies an explicit-key value mapping merges
// additional sibling entries at the same column (e.g. "get:" followed by
// "put:").
func TestYAMLExplicitKeySiblingMerge(t *testing.T) {
	data := []byte("a: 1\n? key\n: get: x\n  put: y\n")
	node, err := LoadFileAsYAML("spec.yaml", data)
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	val, ok := m.Entries[1].Value.(*MapNode)
	if !ok {
		t.Fatalf("expected explicit-key value to be a MapNode, got %T", m.Entries[1].Value)
	}
	if len(val.Entries) != 2 {
		t.Fatalf("expected 2 merged entries, got %d", len(val.Entries))
	}
}
