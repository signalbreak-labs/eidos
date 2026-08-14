package parser

import (
	"testing"
)

// TestNodeFloat exercises every numeric branch of nodeFloat: each signed and
// unsigned integer kind, float64, string-parsing, and the failure paths.
func TestNodeFloat(t *testing.T) {
	ints := []any{
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
	}
	for i, v := range ints {
		got, ok := nodeFloat(&ScalarNode{Value: v})
		if !ok || got != float64(i+1) {
			t.Errorf("nodeFloat(%T(%v)) = (%v,%v), want (%v,true)", v, v, got, ok, float64(i+1))
		}
	}

	if got, ok := nodeFloat(&ScalarNode{Value: 3.5}); !ok || got != 3.5 {
		t.Errorf("nodeFloat(3.5) = (%v,%v), want (3.5,true)", got, ok)
	}
	if got, ok := nodeFloat(&ScalarNode{Value: "12.5"}); !ok || got != 12.5 {
		t.Errorf("nodeFloat(string) = (%v,%v), want (12.5,true)", got, ok)
	}
	if _, ok := nodeFloat(&ScalarNode{Value: "not-a-number"}); ok {
		t.Error("nodeFloat should fail for malformed numeric string")
	}
	if _, ok := nodeFloat(&ScalarNode{Value: struct{}{}}); ok {
		t.Error("nodeFloat should fail for unsupported value type")
	}
	if _, ok := nodeFloat(&MapNode{}); ok {
		t.Error("nodeFloat should fail for non-scalar node")
	}
	if _, ok := nodeFloat(nil); ok {
		t.Error("nodeFloat should fail for nil node")
	}
}

// TestNodeInt asserts nodeInt truncates floats toward zero (best-effort policy).
func TestNodeInt(t *testing.T) {
	if got, ok := nodeInt(&ScalarNode{Value: 3.9}); !ok || got != 3 {
		t.Errorf("nodeInt(3.9) = (%v,%v), want (3,true)", got, ok)
	}
	if _, ok := nodeInt(&ScalarNode{Value: "x"}); ok {
		t.Error("nodeInt should fail for non-numeric value")
	}
}

// TestUnescapeYAMLDoubleQuoted covers the escape subset and the error paths.
func TestUnescapeYAMLDoubleQuoted(t *testing.T) {
	good := []struct {
		in   string
		want string
	}{
		{in: `plain`, want: `plain`},
		{in: `a\nb`, want: "a\nb"},
		{in: `a\tb`, want: "a\tb"},
		{in: `a\rb`, want: "a\rb"},
		{in: `a\\b`, want: `a\b`},
		{in: `a\"b`, want: `a"b`},
		// Full YAML 1.2 section 5.7 escape set beyond the original subset.
		{in: `a\0b`, want: "a\x00b"},
		{in: `a\ab`, want: "a\x07b"},
		{in: `a\bb`, want: "a\x08b"},
		{in: `a\vb`, want: "a\x0Bb"},
		{in: `a\fb`, want: "a\x0Cb"},
		{in: `a\eb`, want: "a\x1Bb"},
		{in: `a\ b`, want: "a b"},
		{in: `a\/b`, want: "a/b"},
		{in: `a\Nb`, want: "a\u0085b"},
		{in: `a\_b`, want: "a\u00A0b"},
		{in: `a\Lb`, want: "a\u2028b"},
		{in: `a\Pb`, want: "a\u2029b"},
		{in: `\x41`, want: "A"},
		{in: `\U00000041`, want: "A"},
		{in: `\U0001F600`, want: "\U0001F600"},
		// `\ ` drops the backslash and keeps the space, matching yaml.v3. Real
		// specs (e.g. GitHub rest-api-description) embed "\ " inside
		// double-quoted description strings.
		{in: `a \  b`, want: "a   b"},
		// Unicode escapes: BMP code point, combined surrogate pair, lone
		// surrogates, and a high surrogate not followed by a low surrogate.
		{in: "\\u00e9", want: "é"},
		{in: "\\uD83D\\uDE00", want: "\U0001F600"},
		{in: "\\uD83D", want: "�"},
		{in: "\\uDE00", want: "�"},
		{in: "\\uD83Dx", want: "�x"},
		{in: "\\uD83D\\u00e9", want: "�é"},
	}
	for _, tt := range good {
		got, err := unescapeYAMLDoubleQuoted(tt.in)
		if err != nil {
			t.Errorf("unescapeYAMLDoubleQuoted(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("unescapeYAMLDoubleQuoted(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	bad := []string{
		"trailing\\",     // escape at end
		"\\u00",          // short unicode escape
		"\\uZZZZ",        // invalid hex
		"\\q",            // unknown escape
		"A\\u",           // unicode escape truncated at end
		"\\uD83D\\uZZZZ", // invalid low surrogate hex
	}
	for _, in := range bad {
		if _, err := unescapeYAMLDoubleQuoted(in); err == nil {
			t.Errorf("unescapeYAMLDoubleQuoted(%q) expected error, got nil", in)
		}
	}
}

// TestJSONStringEscapeCoverage drives the jsonParser.parseString escape cases
// and unicode-error paths that the existing lexer tests do not reach.
func TestJSONStringEscapeCoverage(t *testing.T) {
	node, err := LoadFile("esc.json", []byte(`{"a": "\"\\\/\b\f\n\r\tA😀"}`))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	a := findKey(node.(*MapNode), "a").(*ScalarNode)
	want := `"\/` + "\b\f\n\r\t" + "A😀"
	if a.Value != want {
		t.Errorf("escaped string = %q, want %q", a.Value, want)
	}

	bad := []string{
		`{"a": "\x"}`,       // invalid escape
		`{"a": "\u00"}`,     // truncated unicode escape
		`{"a": "\uZZZZ"}`,   // invalid unicode hex
		`{"a": "bad\`,       // unterminated escape
		"{\"a\": \"\x01\"}", // control character
	}
	for _, in := range bad {
		if _, err := LoadFile("bad.json", []byte(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

// TestYAMLBlockScalars exercises parseScalarBlock: a mapping key with no
// inline value whose following indented lines are bare text (this parser's
// analog of block-scalar content). The lines are joined with newlines and the
// whole block inferred as a single string scalar.
func TestYAMLBlockScalars(t *testing.T) {
	data := []byte("description:\n  line one\n  line two\nother: value\n")
	node, err := LoadFile("block.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile scalar block: %v", err)
	}
	root := node.(*MapNode)
	desc := findKey(root, "description").(*ScalarNode)
	if desc.Value != "line one\nline two" {
		t.Errorf("scalar block = %q, want %q", desc.Value, "line one\nline two")
	}
}

// TestParseSequenceBlockItem covers the sequence item whose value is an
// indented block on the following line (the empty-inline-value path).
func TestParseSequenceBlockItem(t *testing.T) {
	data := []byte("-\n  key: value\n  nested:\n    - 1\n")
	node, err := LoadFile("seqblock.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	seq, ok := node.(*SequenceNode)
	if !ok {
		t.Fatalf("expected *SequenceNode, got %T", node)
	}
	if len(seq.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(seq.Items))
	}
	m, ok := seq.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("expected item to be a *MapNode, got %T", seq.Items[0])
	}
	if v := findKey(m, "key"); v == nil {
		t.Fatal("missing key")
	}
}

// TestParseSequenceBlockItem_NullValue covers the null-value returns of
// parseSequenceBlockItem: end of input and a following line at the same or a
// lower indent.
func TestParseSequenceBlockItem_NullValue(t *testing.T) {
	// Item with no following indented block and EOF.
	node, err := LoadFile("eof.yaml", []byte("-\n"))
	if err != nil {
		t.Fatalf("EOF case: %v", err)
	}
	if seq := node.(*SequenceNode); len(seq.Items) != 1 {
		t.Fatalf("expected 1 item at EOF, got %d", len(seq.Items))
	}

	// A bare `-` in the middle of a nested sequence is followed by a line at the
	// same indent, hitting the next.indent <= baseIndent null return.
	node, err = LoadFile("nested.yaml", []byte("items:\n  - a\n  - \n  - b\n"))
	if err != nil {
		t.Fatalf("same-indent case: %v", err)
	}
	root := node.(*MapNode)
	items := findKey(root, "items").(*SequenceNode)
	if len(items.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items.Items))
	}
	if s, ok := items.Items[1].(*ScalarNode); !ok || s.Value != nil {
		t.Errorf("expected middle null scalar item, got %#v", items.Items[1])
	}

	// Item followed by a document start marker is treated as null and the
	// multi-document stream is rejected (M-35).
	if _, err := LoadFile("marker.yaml", []byte("-\n---\nfoo: bar\n")); err == nil {
		t.Fatal("expected multi-document stream error after sequence null item")
	}
}

// TestYAMLScalarBlockWrongIndent asserts an over-indented scalar block line is
// rejected (parseScalarBlock wrong-indentation branch).
func TestYAMLScalarBlockWrongIndent(t *testing.T) {
	data := []byte("description:\n  line one\n    over-indented\n")
	if _, err := LoadFile("wrongindent.yaml", data); err == nil {
		t.Fatal("expected error for wrong indentation in scalar block")
	}
}

// TestJSONLexerPrimitives exercises the jsonParser byte-level helpers' guard
// branches: peek/advance at end of input and consume on a mismatch.
func TestJSONLexerPrimitives(t *testing.T) {
	p := &jsonParser{data: []byte("ab")}
	if p.peek() != 'a' {
		t.Error("peek should return the current byte")
	}
	p.advance()
	if p.peek() != 'b' {
		t.Error("peek should return the second byte")
	}
	p.advance()
	if p.peek() != 0 {
		t.Error("peek past end should return 0")
	}
	p.advance() // advance past end must not panic
	if err := p.consume('x'); err == nil {
		t.Error("consume at end of input should error")
	}
}

// TestKeyString covers keyString's three branches: nil key, string-valued key,
// and the Raw fallback for non-string scalar keys (e.g. numeric YAML keys).
func TestKeyString(t *testing.T) {
	if got := keyString(nil); got != "" {
		t.Errorf("keyString(nil) = %q, want empty", got)
	}
	if got := keyString(&ScalarNode{Value: "name"}); got != "name" {
		t.Errorf("keyString(string) = %q, want name", got)
	}
	if got := keyString(&ScalarNode{Value: int64(200), Raw: "200"}); got != "200" {
		t.Errorf("keyString(numeric) = %q, want 200", got)
	}
}

// unsupportedTestNode is a Node implementation that is not MapNode/SequenceNode/
// ScalarNode, exercising the defensive error branch of shiftSingleNodeLoc.
type unsupportedTestNode struct{ loc SourceLocation }

func (u *unsupportedTestNode) GetSourceLocation() SourceLocation { return u.loc }

// TestShiftNodeLineAndColumn exercises the location-shifting walker used after
// inline flow collections are parsed: MapNode key/value recursion, SequenceNode
// items, nil nodes, and the unsupported-type error branch. Each node appears in
// the tree exactly once so it is shifted exactly once: a node's column becomes
// column + baseLoc.Column - 1.
func TestShiftNodeLineAndColumn(t *testing.T) {
	base := SourceLocation{File: "flow.yaml", Line: 7, Column: 3}
	at := func(col int) SourceLocation { return SourceLocation{File: "flow.yaml", Line: 1, Column: col} }

	// Map whose value is a scalar (column 4 → 4+3-1 = 6) and whose key is a
	// scalar at column 1 (→ 3).
	key := &ScalarNode{Value: "k", SourceLocation: at(1)}
	scalar := &ScalarNode{Value: "x", SourceLocation: at(4)}
	child := &MapNode{SourceLocation: at(1)}
	child.Entries = []MapEntry{{Key: key, Value: scalar}}
	seqItem := &ScalarNode{Value: "s", SourceLocation: at(2)} // → 4
	seq := &SequenceNode{Items: []Node{seqItem}, SourceLocation: at(1)}
	root := &MapNode{SourceLocation: at(1)}
	root.Entries = []MapEntry{{Key: &ScalarNode{Value: "outer", SourceLocation: at(1)}, Value: child}}
	root.Entries = append(root.Entries, MapEntry{Key: &ScalarNode{Value: "seq", SourceLocation: at(1)}, Value: seq})

	if err := shiftNodeLineAndColumn(root, base); err != nil {
		t.Fatalf("shift: %v", err)
	}
	check := func(what string, n Node, wantCol int) {
		t.Helper()
		loc := n.GetSourceLocation()
		if loc.Line != 7 || loc.Column != wantCol {
			t.Errorf("%s loc = %s:%d:%d, want line 7 col %d", what, loc.File, loc.Line, loc.Column, wantCol)
		}
	}
	check("root", root, 3)
	check("child map", child, 3)
	check("scalar value", scalar, 6)
	check("map key", key, 3)
	check("sequence", seq, 3)
	check("seq item", seqItem, 4)

	// nil is a no-op.
	if err := shiftNodeLineAndColumn(nil, base); err != nil {
		t.Errorf("shift nil: %v", err)
	}
	// Unsupported node type surfaces an error.
	if err := shiftNodeLineAndColumn(&unsupportedTestNode{}, base); err == nil {
		t.Error("expected error for unsupported node type")
	}
}

// TestJSONUnicodeEscapeBranches drives appendUnicodeEscape through the JSON
// parser: a full surrogate pair (combine branch), a high surrogate followed by
// a \u escape that is not a low surrogate, a lone low surrogate, and a BMP
// code point. All emoji/accents are written as JSON \u escapes so the
// appendUnicodeEscape code path (not the raw-rune path) executes.
func TestJSONUnicodeEscapeBranches(t *testing.T) {
	data := []byte(`{"a": "\uD83D\uDE00", "b": "\uD83Dé", "c": "\uDE00", "d": "é"}`)
	node, err := LoadFile("uni.json", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := map[string]string{
		"a": "\U0001F600", // surrogate pair combined (M-34)
		"b": "�é",         // high surrogate + non-low \u escape
		"c": "�",          // lone low surrogate
		"d": "é",          // BMP code point
	}
	for k, w := range want {
		v := findKey(node.(*MapNode), k).(*ScalarNode)
		if v.Value != w {
			t.Errorf("%s = %q, want %q", k, v.Value, w)
		}
	}
}

// TestIsMappingLine exercises the mapping-line discriminator used by the
// sequence item parser.
func TestIsMappingLine(t *testing.T) {
	if !isMappingLine("foo: bar") {
		t.Error("expected 'foo: bar' to be a mapping line")
	}
	if isMappingLine("just a scalar") {
		t.Error("expected plain text not to be a mapping line")
	}
	if !isMappingLine("foo:") {
		t.Error("expected 'foo:' with empty value to be a mapping line")
	}
}
