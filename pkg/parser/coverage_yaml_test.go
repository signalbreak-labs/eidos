package parser

import (
	"strings"
	"testing"
)

// TestYAMLMappingNonColonLine drives the "line without a colon ends the
// mapping" branch of parseMapping: a plain scalar line at the same indentation
// as a mapping key terminates the mapping (and the trailing content is then
// rejected by loadYAML as extra content).
func TestYAMLMappingNonColonLine(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\nplain\n"))
	if err == nil {
		t.Fatal("expected an error for trailing non-mapping content")
	}
	if !strings.Contains(err.Error(), "unexpected extra content") {
		t.Errorf("error %q does not mention the extra content", err)
	}
}

// TestYAMLMappingInvalidEntry drives the splitMapping failure branch of
// parseMapping: a colon not followed by whitespace is not a mapping separator.
func TestYAMLMappingInvalidEntry(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a:b\n"))
	if err == nil {
		t.Fatal("expected an error for a colon without a following space")
	}
	if !strings.Contains(err.Error(), "invalid mapping entry") {
		t.Errorf("error %q does not mention the invalid entry", err)
	}
}

// TestYAMLMappingBadQuotedKey drives the unquoteYAMLKey failure branch of
// parseMapping: a double-quoted key with an invalid escape.
func TestYAMLMappingBadQuotedKey(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("\"abc\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid escape in a quoted key")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLExplicitKeyBadQuotedKey drives the unquoteYAMLKey failure branch of
// parseExplicitKey.
func TestYAMLExplicitKeyBadQuotedKey(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? \"abc\\q\"\n: value\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid escape in an explicit key")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLExplicitKeyValueWrongIndent drives the indentation-mismatch branch of
// parseExplicitKey: the value line must sit at the same indentation as the key.
func TestYAMLExplicitKeyValueWrongIndent(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\n  : value\n"))
	if err == nil {
		t.Fatal("expected an error for a misindented explicit-key value")
	}
	if !strings.Contains(err.Error(), "same indentation") {
		t.Errorf("error %q does not mention the indentation", err)
	}
}

// TestYAMLExplicitKeyBareColonValue drives the fill branch of
// parseExplicitKeyValue: an explicit-key value that is a mapping entry with a
// bare colon ("type:") must have its value filled from the following block.
func TestYAMLExplicitKeyBareColonValue(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\n: type:\n    properties: {}\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	val, ok := m.Entries[1].Value.(*MapNode)
	if !ok {
		t.Fatalf("expected explicit-key value to be a MapNode, got %T", m.Entries[1].Value)
	}
	if len(val.Entries) != 1 {
		t.Fatalf("expected 1 entry in the value mapping, got %d", len(val.Entries))
	}
	if val.Entries[0].Value == nil {
		t.Error("expected the bare-colon entry to have a filled value")
	}
}

// TestYAMLMappingBlockValueDedent drives the dedent branch of
// parseMappingBlockValue: a key with no inline value followed by a same-indent
// non-sequence line gets a null value and the line is a sibling key.
func TestYAMLMappingBlockValueDedent(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a:\nb: 1\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if len(m.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.Entries))
	}
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != nil {
		t.Errorf("expected a null value for a, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLMappingBlockValueDocMarker drives the doc-marker branch of
// parseMappingBlockValue: a document marker indented under a key means the key
// has a null value. The marker is then over-indented for the root mapping, so
// the document is rejected — but the null-value branch has executed.
func TestYAMLMappingBlockValueDocMarker(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a:\n  ---\n"))
	if err == nil {
		t.Fatal("expected an error for a doc marker indented under a key")
	}
}

// TestYAMLSequenceOverIndent drives the over-indentation error branch of
// parseSequence.
func TestYAMLSequenceOverIndent(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a\n  b: 1\n"))
	if err == nil {
		t.Fatal("expected an error for an over-indented line in a sequence")
	}
	if !strings.Contains(err.Error(), "wrong indentation in sequence") {
		t.Errorf("error %q does not mention the indentation", err)
	}
}

// TestYAMLSequenceNonDashLine drives the non-dash line break branch of
// parseSequence: a same-indent line that is not a sequence item ends the
// sequence (and the trailing content is rejected by loadYAML).
func TestYAMLSequenceNonDashLine(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a\nb: 1\n"))
	if err == nil {
		t.Fatal("expected an error for trailing non-sequence content")
	}
	if !strings.Contains(err.Error(), "unexpected extra content") {
		t.Errorf("error %q does not mention the extra content", err)
	}
}

// TestYAMLSequenceDashNoSpace drives the dash-followed-by-non-space break
// branch of parseSequence.
func TestYAMLSequenceDashNoSpace(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a\n-a\n"))
	if err == nil {
		t.Fatal("expected an error for a dash not followed by whitespace")
	}
	if !strings.Contains(err.Error(), "unexpected extra content") {
		t.Errorf("error %q does not mention the extra content", err)
	}
}

// TestYAMLNestedSequenceOverIndent drives the over-indentation error branch of
// parseNestedSequence (a compact "- - a" item followed by an over-indented
// line).
func TestYAMLNestedSequenceOverIndent(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n  b: 1\n"))
	if err == nil {
		t.Fatal("expected an error for an over-indented line in a nested sequence")
	}
	if !strings.Contains(err.Error(), "wrong indentation in sequence") {
		t.Errorf("error %q does not mention the indentation", err)
	}
}

// TestYAMLNestedSequenceDocMarker drives the doc-marker break branch of
// parseNestedSequence.
func TestYAMLNestedSequenceDocMarker(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n  ---\n"))
	if err == nil {
		t.Fatal("expected an error for a doc marker inside a nested sequence")
	}
}

// TestYAMLNestedSequenceNonDash drives the non-dash break branch of
// parseNestedSequence.
func TestYAMLNestedSequenceNonDash(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n  b: 1\n"))
	if err == nil {
		t.Fatal("expected an error for a non-dash line in a nested sequence")
	}
}

// TestYAMLNestedSequenceDashNoSpace drives the dash-followed-by-non-space break
// branch of parseNestedSequence.
func TestYAMLNestedSequenceDashNoSpace(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n  -a\n"))
	if err == nil {
		t.Fatal("expected an error for a dash not followed by whitespace")
	}
}

// TestYAMLSequenceBlockItemDocMarker drives the doc-marker branch of
// parseSequenceBlockItem: a document marker indented under a sequence item with
// no inline value means the item has a null value. The marker is then
// over-indented for the sequence, so the document is rejected — but the
// null-value branch has executed.
func TestYAMLSequenceBlockItemDocMarker(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("-\n  ---\n"))
	if err == nil {
		t.Fatal("expected an error for a doc marker indented under a sequence item")
	}
}

// TestYAMLSequenceCommentItem drives the empty-trimmed branch of isMappingLine:
// a sequence item that is only a comment is not a mapping line.
func TestYAMLSequenceCommentItem(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("- # comment\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	seq, ok := node.(*SequenceNode)
	if !ok || len(seq.Items) != 1 {
		t.Fatalf("expected a 1-item sequence, got %#v", node)
	}
}

// TestYAMLSequenceMappingBadQuotedKey drives the unquoteYAMLKey failure branch
// of parseSingleMapping (a sequence item that is a mapping with a bad quoted
// key).
func TestYAMLSequenceMappingBadQuotedKey(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- \"abc\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid escape in a sequence mapping key")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLSequenceMappingVersionScalar drives the version-scalar branch of
// parseSingleMapping: a top-level sequence item whose key is "openapi" keeps
// its value as a string.
func TestYAMLSequenceMappingVersionScalar(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("- openapi: 3.0.0\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	seq, ok := node.(*SequenceNode)
	if !ok || len(seq.Items) != 1 {
		t.Fatalf("expected a 1-item sequence, got %#v", node)
	}
	m, ok := seq.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("expected a mapping item, got %T", seq.Items[0])
	}
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "3.0.0" {
		t.Errorf("expected version string 3.0.0, got %#v", m.Entries[0].Value)
	}
}
