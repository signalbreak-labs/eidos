package parser

import (
	"strings"
	"testing"
)

// TestYAMLScalarBlockBlankTrailing drives the EOF break of parseScalarBlock: a
// plain scalar followed by a blank line.
func TestYAMLScalarBlockBlankTrailing(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("plain\n\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	if s, ok := node.(*ScalarNode); !ok || s.Value != "plain" {
		t.Errorf("expected scalar plain, got %#v", node)
	}
}

// TestYAMLScalarBlockDocMarker drives the doc-marker break of parseScalarBlock.
func TestYAMLScalarBlockDocMarker(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("plain\n---\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	if s, ok := node.(*ScalarNode); !ok || s.Value != "plain" {
		t.Errorf("expected scalar plain, got %#v", node)
	}
}

// TestYAMLScalarBlockUnterminatedQuote drives the inferScalar error branch of
// parseScalarBlock: a plain scalar that is an unterminated single quote.
func TestYAMLScalarBlockUnterminatedQuote(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("'abc\n"))
	if err == nil {
		t.Fatal("expected an error for an unterminated single-quoted scalar")
	}
	if !strings.Contains(err.Error(), "unterminated single-quoted") {
		t.Errorf("error %q does not mention the unterminated quote", err)
	}
}

// TestYAMLVersionScalarBadEscape drives the unescape-failure branch of
// parseVersionScalar: a quoted version with an invalid escape keeps its raw
// (still-quoted) value rather than failing.
func TestYAMLVersionScalarBadEscape(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("openapi: \"3.0\\q\"\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "\"3.0\\q\"" {
		t.Errorf("expected raw quoted version value, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockScalarDuplicateChomping drives the duplicate-chomping rejection
// of parseBlockScalarHeader ("|+-"): the header falls back to a plain scalar
// that folds with the following line.
func TestYAMLBlockScalarDuplicateChomping(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: |+-\n  x\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "|+- x" {
		t.Errorf("expected the header to fall back to a plain scalar, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockScalarDuplicateIndent drives the duplicate-indent rejection of
// parseBlockScalarHeader ("|12").
func TestYAMLBlockScalarDuplicateIndent(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: |12\n  x\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "|12 x" {
		t.Errorf("expected the header to fall back to a plain scalar, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockScalarInvalidChar drives the invalid-character rejection of
// parseBlockScalarHeader ("|x").
func TestYAMLBlockScalarInvalidChar(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: |x\n  y\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "|x y" {
		t.Errorf("expected the header to fall back to a plain scalar, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockScalarAutoDetectBlank drives the blank-line skip in the
// auto-detect loop of parseBlockScalarContent: a leading blank line is content.
func TestYAMLBlockScalarAutoDetectBlank(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: |\n\n  content\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "\ncontent\n" {
		t.Errorf("expected block scalar content with leading blank, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockScalarAutoDetectDedent drives the dedent break in the
// auto-detect loop of parseBlockScalarContent: a line at the parent's indent
// means the block scalar is empty.
func TestYAMLBlockScalarAutoDetectDedent(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: |\nb: 1\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if len(m.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.Entries))
	}
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "" {
		t.Errorf("expected an empty block scalar for a, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLFoldedBlockTrailingBlank drives the trailing-blank branch of
// foldBlockLines.
func TestYAMLFoldedBlockTrailingBlank(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: >\n  line1\n\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "line1\n" {
		t.Errorf("expected folded value, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLQuotedScalarEscapedSingleQuote drives the escaped-quote branch of
// quotedScalarRaw (” inside a single-quoted scalar).
func TestYAMLQuotedScalarEscapedSingleQuote(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: 'it''s'\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "it's" {
		t.Errorf("expected it's, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLQuotedScalarBadEscape drives the decodeQuotedScalar error branch of
// parseQuotedScalarContinuation: a multi-line double-quoted scalar whose
// closing quote is found on a continuation line, then fails to unescape.
func TestYAMLQuotedScalarBadEscape(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: \"abc\\q\n  def\"\n"))
	if err == nil {
		t.Fatal("expected an error for an invalid escape in a quoted scalar")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLAnchorNoName drives the empty-name branch of splitIndicatorName: an
// anchor indicator with no name is not an anchor.
func TestYAMLAnchorNoName(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: & x\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "& x" {
		t.Errorf("expected a plain scalar, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLAliasTrailingContent drives the trailing-content rejection of
// splitAliasIndicator: an alias must be the entire value.
func TestYAMLAliasTrailingContent(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: *x y\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "*x y" {
		t.Errorf("expected a plain scalar, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLQuotedScalarTrailingBackslash drives the trailing-backslash branch of
// unescapeYAMLDoubleQuoted via isCompleteQuotedScalar.
func TestYAMLQuotedScalarTrailingBackslash(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: \"abc\\\"\n"))
	if err == nil {
		t.Fatal("expected an error for a quoted scalar ending in a backslash")
	}
}

// TestYAMLUnicodeEscapeTooLarge drives the code > 0x10FFFF branch of
// appendNumericEscape via a \U escape in a multi-line quoted scalar.
func TestYAMLUnicodeEscapeTooLarge(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: \"\\UFFFFFFFF\n  x\"\n"))
	if err == nil {
		t.Fatal("expected an error for a \\U escape above the Unicode range")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLQuotedScalarMultilineBlank drives the blank-line folding branches of
// foldQuotedInterior: a multi-line double-quoted scalar with a blank line.
func TestYAMLQuotedScalarMultilineBlank(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: \"line1\n\n  line2\"\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "line1\nline2" {
		t.Errorf("expected folded multi-line value, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLQuotedScalarTrailingBlank drives the trailing-blank branch of
// foldQuotedInterior.
func TestYAMLQuotedScalarTrailingBlank(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: \"line1\n\n\"\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "line1\n" {
		t.Errorf("expected folded value with trailing newline, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLBlockDocMarkerInFill drives the doc-marker break of parseBlock when
// reached from fillSequenceMappingFirstValue: a document marker indented under
// a sequence item's bare-colon mapping entry terminates the block (the marker
// is then over-indented for the sequence, so the document is rejected — but
// the parseBlock doc-marker branch has executed).
func TestYAMLBlockDocMarkerInFill(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a:\n    ---\n"))
	if err == nil {
		t.Fatal("expected an error for a doc marker indented under a sequence item")
	}
}

// TestYAMLExplicitKeyValueFillError drives the error-propagation branches of
// parseExplicitKeyValue and fillSequenceMappingFirstValue: the explicit-key
// value is a mapping entry whose block value fails to parse.
func TestYAMLExplicitKeyValueFillError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\n: a:\n    \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in an explicit-key value")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLExplicitKeyMergeSibling drives the sibling-merge branch of
// parseExplicitKeyValue: a mapping entry at the value's column merges into the
// explicit-key value.
func TestYAMLExplicitKeyMergeSibling(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\n: a:\n  b: 1\n"))
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

// TestYAMLExplicitKeyMergeError drives the parseBlock error branch of
// parseExplicitKeyValue's sibling merge: the merged block fails to parse.
func TestYAMLExplicitKeyMergeError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: 1\n? key\n: a:\n  b:\n    \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in a merged explicit-key block")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLNestedSequenceOverIndent2 drives the over-indentation error branch of
// parseNestedSequence's inner-item loop: a line more indented than the nested
// sequence's own base indentation.
func TestYAMLNestedSequenceOverIndent2(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n   b: 1\n"))
	if err == nil {
		t.Fatal("expected an error for an over-indented line in a nested sequence")
	}
	if !strings.Contains(err.Error(), "wrong indentation in sequence") {
		t.Errorf("error %q does not mention the indentation", err)
	}
}

// TestYAMLNestedSequenceItemError drives the error-propagation branch of
// parseNestedSequence's inner-item loop: an inner item that fails to parse.
func TestYAMLNestedSequenceItemError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- - a\n  - \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in a nested sequence item")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLSequenceBlockItemError drives the parseBlock error branch of
// parseSequenceBlockItem: a block value that fails to parse.
func TestYAMLSequenceBlockItemError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("-\n  \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in a sequence block item")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLSequenceMappingFillError drives the parseBlock error branch of
// fillSequenceMappingFirstValue: a sequence item's bare-colon mapping entry
// whose block value fails to parse.
func TestYAMLSequenceMappingFillError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a:\n    \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in a sequence mapping fill")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLSequenceMappingMergeError drives the parseBlock error branch of
// mergeSequenceMappingEntries: a sibling mapping entry whose block value fails
// to parse.
func TestYAMLSequenceMappingMergeError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a: 1\n  b:\n    \"bad\\q\": 1\n"))
	if err == nil {
		t.Fatal("expected an error for a bad quoted key in a merged sequence mapping")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestYAMLSequenceMappingValueError drives the parseValueAfterColon error branch
// of parseSingleMapping: a sequence item mapping whose value fails to parse.
func TestYAMLSequenceMappingValueError(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("- a: 'abc\n"))
	if err == nil {
		t.Fatal("expected an error for an unterminated quoted value in a sequence mapping")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("error %q does not mention the unterminated quote", err)
	}
}

// TestYAMLVersionScalarSingleQuoted drives the single-quote branch of
// parseVersionScalar: a single-quoted version string.
func TestYAMLVersionScalarSingleQuoted(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("openapi: '3.0.0'\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "3.0.0" {
		t.Errorf("expected version string 3.0.0, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLFoldedBlockBlankLine drives the blankPending branch of foldBlockLines:
// a folded block scalar with a blank line between content lines.
func TestYAMLFoldedBlockBlankLine(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: >\n  line1\n\n  line2\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "line1\nline2\n" {
		t.Errorf("expected folded value with paragraph break, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLFoldedBlockMoreIndented drives the moreIndented branch of
// foldBlockLines: a folded block scalar with a more-indented content line.
func TestYAMLFoldedBlockMoreIndented(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: >\n  line1\n    indented\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "line1\n  indented\n" {
		t.Errorf("expected folded value preserving the indented line, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLQuotedScalarSingleChar drives the len < 2 branch of
// isCompleteQuotedScalar: a value that is only a quote character.
func TestYAMLQuotedScalarSingleChar(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("a: \"\n"))
	if err == nil {
		t.Fatal("expected an error for a lone quote character")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("error %q does not mention the unterminated quote", err)
	}
}

// TestYAMLQuotedScalarMultilineSingle drives the escaped-quote branch of
// quotedScalarRaw: a multi-line single-quoted scalar containing an escaped
// quote (”).
func TestYAMLQuotedScalarMultilineSingle(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: 'it''s\n  fine'\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "it's fine" {
		t.Errorf("expected folded single-quoted value, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLExplicitKeyMergeDepth drives the enterBlock depth guard of
// parseExplicitKeyValue's sibling merge: explicit keys with sibling entries
// nested past maxNestingDepth.
func TestYAMLExplicitKeyMergeDepth(t *testing.T) {
	var sb strings.Builder
	// Level 0: the root mapping starts with a colon line so parseBlock routes to
	// parseMapping; the explicit key is a subsequent line.
	sb.WriteString("a: 1\n")
	sb.WriteString("? key\n")
	sb.WriteString(": a:\n")
	sb.WriteString("  b:\n")
	// Each deeper level starts with a colon line ("c: 1") so the block routes to
	// parseMapping, then carries the explicit key as a subsequent line. Each
	// level adds two enterBlock frames (the sibling merge and the block value),
	// so maxNestingDepth/2 levels reach the depth guard.
	for i := 1; i < maxNestingDepth/2+1; i++ {
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString("c: 1\n")
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString("? key\n")
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString(": a:\n")
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString("  b:\n")
	}
	_, err := LoadFileAsYAML("deep-explicit.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected a depth-limit error for deeply nested explicit-key merges")
	}
	if !strings.Contains(err.Error(), "nesting") {
		t.Errorf("error %q does not mention the nesting limit", err)
	}
}

// TestYAMLScalarBlockUnterminatedDouble drives the unterminated-double-quote
// branch of inferScalar via parseScalarBlock: a scalar block whose first line
// opens a double quote that never closes.
func TestYAMLScalarBlockUnterminatedDouble(t *testing.T) {
	_, err := LoadFileAsYAML("spec.yaml", []byte("\"abc\ndef\n"))
	if err == nil {
		t.Fatal("expected an error for an unterminated double-quoted scalar block")
	}
	if !strings.Contains(err.Error(), "unterminated double-quoted") {
		t.Errorf("error %q does not mention the unterminated quote", err)
	}
}

// TestYAMLDoubleQuotedTabEscape drives the literal-tab escape branch of
// unescapeYAMLDoubleQuoted: a backslash followed by a literal tab character.
func TestYAMLDoubleQuotedTabEscape(t *testing.T) {
	node, err := LoadFileAsYAML("spec.yaml", []byte("a: \"x\\\ty\"\n"))
	if err != nil {
		t.Fatalf("LoadFileAsYAML: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "x\ty" {
		t.Errorf("expected tab escape decoded, got %#v", m.Entries[0].Value)
	}
}

// TestYAMLSequenceBlockItemDepth drives the enterBlock depth guard of
// parseSequenceBlockItem: a sequence of block items nested past maxNestingDepth.
func TestYAMLSequenceBlockItemDepth(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < maxNestingDepth+2; i++ {
		sb.WriteString(strings.Repeat("  ", i))
		sb.WriteString("-\n")
	}
	_, err := LoadFileAsYAML("deep-seq.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected a depth-limit error for deeply nested sequence block items")
	}
	if !strings.Contains(err.Error(), "nesting") {
		t.Errorf("error %q does not mention the nesting limit", err)
	}
}

// TestYAMLSequenceMappingFillDepth drives the enterBlock depth guard of
// fillSequenceMappingFirstValue: sequence items with bare-colon mapping values
// nested past maxNestingDepth.
func TestYAMLSequenceMappingFillDepth(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < maxNestingDepth+2; i++ {
		sb.WriteString(strings.Repeat("  ", i))
		sb.WriteString("- a:\n")
	}
	_, err := LoadFileAsYAML("deep-fill.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected a depth-limit error for deeply nested sequence mapping fills")
	}
	if !strings.Contains(err.Error(), "nesting") {
		t.Errorf("error %q does not mention the nesting limit", err)
	}
}

// TestYAMLSequenceMappingMergeDepth drives the enterBlock depth guard of
// mergeSequenceMappingEntries: sibling mapping entries with block values nested
// past maxNestingDepth.
func TestYAMLSequenceMappingMergeDepth(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < maxNestingDepth/2+1; i++ {
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString("- a: 1\n")
		sb.WriteString(strings.Repeat("    ", i))
		sb.WriteString("  b:\n")
	}
	_, err := LoadFileAsYAML("deep-merge.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected a depth-limit error for deeply nested sequence mapping merges")
	}
	if !strings.Contains(err.Error(), "nesting") {
		t.Errorf("error %q does not mention the nesting limit", err)
	}
}
