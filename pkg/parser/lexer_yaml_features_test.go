package parser

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// scalarOf returns the Value of the scalar node found under key k in root.
func scalarOf(t *testing.T, root Node, k string) any {
	t.Helper()
	v := findKey(root.(*MapNode), k)
	s, ok := v.(*ScalarNode)
	if !ok {
		t.Fatalf("key %q value is %T, want *ScalarNode", k, v)
	}
	return s.Value
}

// seqOf returns the sequence node found under key k in root.
func seqOf(t *testing.T, root Node, k string) *SequenceNode {
	t.Helper()
	v := findKey(root.(*MapNode), k)
	s, ok := v.(*SequenceNode)
	if !ok {
		t.Fatalf("key %q value is %T, want *SequenceNode", k, v)
	}
	return s
}

// TestMultiLinePlainScalar exercises the folding of a plain scalar that
// continues across more-indented lines: single line breaks fold to spaces, and
// a colon inside a URL does not end the scalar.
func TestMultiLinePlainScalar(t *testing.T) {
	data := []byte(`description: The REST API. Please see
  https://platform.example.com/docs for details.
title: Done
`)
	root, err := LoadFile("plain.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := "The REST API. Please see https://platform.example.com/docs for details."
	if got := scalarOf(t, root, "description"); got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
	// The following sibling key is not absorbed into the scalar.
	if got := scalarOf(t, root, "title"); got != "Done" {
		t.Errorf("title = %q, want Done", got)
	}
}

// TestMultiLinePlainScalarParagraphs folds blank-line-separated paragraphs of a
// multi-line plain scalar into newlines.
func TestMultiLinePlainScalarParagraphs(t *testing.T) {
	data := []byte(`description: first line
  second line

  third line
title: Done
`)
	root, err := LoadFile("para.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := scalarOf(t, root, "description"); got != "first line second line\nthird line" {
		t.Errorf("description = %q", got)
	}
}

// TestPlainScalarFoldsNestedDashLine asserts that a continuation line beginning
// with "- " is folded into the plain scalar rather than treated as a block
// sequence indicator. Once an inline plain scalar value has started, the key's
// value is a scalar and cannot have block children, so a "-" at the start of a
// more-indented continuation line is literal content (YAML 1.2 §7.3.3).
// yaml.v3/PyYAML fold such lines the same way. A following sibling key is not
// absorbed. This pattern appears in real specs (e.g. the GitHub
// rest-api-description, where a description value continues on a line starting
// with "- for example, ...").
func TestPlainScalarFoldsNestedDashLine(t *testing.T) {
	data := []byte(`name: something
  - not part of the scalar
title: Done
`)
	root, err := LoadFile("seqind.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := "something - not part of the scalar"
	if got := scalarOf(t, root, "name"); got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if got := scalarOf(t, root, "title"); got != "Done" {
		t.Errorf("title = %q, want Done", got)
	}
}

// TestPlainScalarEndsAtNestedMapping asserts a continuation line that itself
// looks like a mapping entry ("k: v") ends the scalar and is rejected: that is
// a structural indicator, and reference parsers reject it as "mapping values
// are not allowed in this context".
func TestPlainScalarEndsAtNestedMapping(t *testing.T) {
	data := []byte(`key: value
  continuation: with colon
`)
	if _, err := LoadFile("nestedmap.yaml", data); err == nil {
		t.Fatal("expected error for nested mapping under inline scalar")
	}
}

// TestBlockScalarStyles exercises the literal/folded styles, chomping
// indicators, and the explicit indentation indicator.
func TestBlockScalarStyles(t *testing.T) {
	data := []byte(`a: |
  line one
  line two
b: >-
  fold one
  fold two
c: |+
  keep
d: |2
  explicit
e: |-
  strip
`)
	root, err := LoadFile("blocks.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	cases := map[string]string{
		"a": "line one\nline two\n", // literal, clip chomp
		"b": "fold one fold two",    // folded, strip chomp
		"c": "keep\n",               // literal, keep chomp (retains the trailing newline)
		"d": "explicit\n",           // literal with explicit indent indicator, clip chomp
		"e": "strip",                // literal, strip chomp
	}
	for k, want := range cases {
		if got := scalarOf(t, root, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// TestMultiLineQuotedScalarSameIndent folds a quoted scalar whose continuation
// lines sit at the same indentation as the key that opened them. This pattern
// appears in real specs (e.g. the GigaVUE-FM openapi.fm.yaml, where parameter
// descriptions span three lines at the same indent) and is accepted by PyYAML
// and libyaml: the closing quote alone ends the string, regardless of the
// continuation lines' indentation. A following sibling key is not absorbed.
func TestMultiLineQuotedScalarSameIndent(t *testing.T) {
	data := []byte(`parameters:
  - name: sort
    description: 'parentheses-enclosed comma-separated list of entity attributes,
    optionally qualified with the sort order attribute. The default sort order
    is ASC. Example: sort=(aaa,bbb:ASC,ccc:DESC)'
    required: false
`)
	root, err := LoadFile("same-indent.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	params := seqOf(t, root, "parameters")
	if len(params.Items) != 1 {
		t.Fatalf("parameters has %d items, want 1", len(params.Items))
	}
	param := params.Items[0].(*MapNode)
	want := "parentheses-enclosed comma-separated list of entity attributes, optionally qualified with the sort order attribute. The default sort order is ASC. Example: sort=(aaa,bbb:ASC,ccc:DESC)"
	if got := findKey(param, "description").(*ScalarNode).Value; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
	if got := findKey(param, "required").(*ScalarNode).Value; got != false {
		t.Errorf("required = %v, want false", got)
	}
}

// TestMultiLineQuotedScalarDedented folds a quoted scalar whose continuation
// lines are dedented below the key, aligned with the enclosing sequence item.
// This is the exact formatting found in GigaVUE-FM's openapi.fm.yaml, and both
// PyYAML and libyaml accept it: indentation is not a structural signal inside a
// quoted scalar, only the closing quote ends the string.
func TestMultiLineQuotedScalarDedented(t *testing.T) {
	data := []byte(`parameters:
  - name: sort
    in: query
    description: 'parentheses-enclosed comma-separated list of entity attributes,
  optionally qualified with the sort order attribute. The default sort order
  is ASC. Example: sort=(aaa,bbb:ASC,ccc:DESC)'
    required: false
`)
	root, err := LoadFile("dedented.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	param := seqOf(t, root, "parameters").Items[0].(*MapNode)
	want := "parentheses-enclosed comma-separated list of entity attributes, optionally qualified with the sort order attribute. The default sort order is ASC. Example: sort=(aaa,bbb:ASC,ccc:DESC)"
	if got := findKey(param, "description").(*ScalarNode).Value; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
	if got := findKey(param, "required").(*ScalarNode).Value; got != false {
		t.Errorf("required = %v, want false", got)
	}
}

// TestQuotedScalarTrailingContentRejected asserts content after the closing
// quote on the terminating line of a multi-line quoted scalar fails loud
// instead of being silently dropped (a sibling key whose quote prematurely
// closed the string must not swallow the rest of the line).
func TestQuotedScalarTrailingContentRejected(t *testing.T) {
	data := []byte("description: 'foo,\nother: 'bar'\n")
	if _, err := LoadFile("trailing.yaml", data); err == nil {
		t.Fatal("expected error for trailing content after closing quote")
	}
}

// TestMultiLineDoubleQuotedScalar folds a double-quoted scalar that spans
// several lines: single breaks fold to spaces.
func TestMultiLineDoubleQuotedScalar(t *testing.T) {
	data := []byte(`description: "Anchor timestamp after which the policy applies.
  Supported anchors: 'created_at'. Note that the anchor is the file
  creation time, not the time the batch is created."
title: Done
`)
	root, err := LoadFile("dquote.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := "Anchor timestamp after which the policy applies. Supported anchors: 'created_at'. Note that the anchor is the file creation time, not the time the batch is created."
	if got := scalarOf(t, root, "description"); got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

// TestMultiLineQuotedBackslashContinuation asserts a backslash at the end of a
// line escapes the following line break in a double-quoted scalar: the pair is
// dropped entirely, no space is inserted.
func TestMultiLineQuotedBackslashContinuation(t *testing.T) {
	data := []byte(`name: "Objec\
  tName"
`)
	root, err := LoadFile("cont.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := scalarOf(t, root, "name"); got != "ObjectName" {
		t.Errorf("name = %q, want ObjectName", got)
	}
}

// TestCompactNestedSequence parses a sequence whose items are themselves
// sequences begun on the same line as their dash ("- - a").
func TestCompactNestedSequence(t *testing.T) {
	data := []byte(`items:
  - - 1435781430
    - '1'
  - - 1435781445
    - '1'
`)
	root, err := LoadFile("nested.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	items := seqOf(t, root, "items")
	if len(items.Items) != 2 {
		t.Fatalf("items has %d entries, want 2", len(items.Items))
	}
	for i, want := range []float64{1435781430, 1435781445} {
		inner, ok := items.Items[i].(*SequenceNode)
		if !ok {
			t.Fatalf("item %d is %T, want *SequenceNode", i, items.Items[i])
		}
		if len(inner.Items) != 2 {
			t.Fatalf("item %d has %d entries, want 2", i, len(inner.Items))
		}
		first := inner.Items[0].(*ScalarNode)
		if first.Value != want {
			t.Errorf("item %d first = %v, want %v", i, first.Value, want)
		}
		second := inner.Items[1].(*ScalarNode)
		if second.Value != "1" {
			t.Errorf("item %d second = %v, want \"1\"", i, second.Value)
		}
	}
}

// TestAnchorAliasRoundTrip anchors a block value and resolves a later alias to
// an independent copy.
func TestAnchorAliasRoundTrip(t *testing.T) {
	data := []byte(`a:
  required: &rq
    - type
b:
  required: *rq
`)
	root, err := LoadFile("anchor.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)
	a := findKey(rootMap, "a").(*MapNode)
	b := findKey(rootMap, "b").(*MapNode)

	aSeq := findKey(a, "required").(*SequenceNode)
	bSeq := findKey(b, "required").(*SequenceNode)
	if len(aSeq.Items) != 1 || len(bSeq.Items) != 1 {
		t.Fatalf("expected single-item sequences, got %d and %d", len(aSeq.Items), len(bSeq.Items))
	}
	if av, bv := aSeq.Items[0].(*ScalarNode).Value, bSeq.Items[0].(*ScalarNode).Value; av != "type" || bv != "type" {
		t.Errorf("alias value mismatch: %v vs %v", av, bv)
	}
	// The alias is a deep copy, not the anchored node itself.
	if aSeq == bSeq {
		t.Error("aliased sequence shares the anchored node; want an independent copy")
	}
}

// TestAnchorInlineValue anchors an inline value and resolves it via alias.
func TestAnchorInlineValue(t *testing.T) {
	data := []byte(`count: &n 5
copy: *n
`)
	root, err := LoadFile("anchor-inline.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := scalarOf(t, root, "copy"); got != float64(5) {
		t.Errorf("copy = %v, want 5", got)
	}
}

// TestUnknownAlias fails loud rather than silently producing a null value.
func TestUnknownAlias(t *testing.T) {
	data := []byte("value: *missing\n")
	if _, err := LoadFile("bad-alias.yaml", data); err == nil {
		t.Fatal("expected error for unknown alias")
	} else if !strings.Contains(err.Error(), "unknown YAML alias *missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFlowAliasResolves verifies that a YAML alias inside a flow collection
// ("*name" as a flow sequence item or flow mapping value) resolves against the
// anchor table instead of becoming the literal string "*name" (L-2).
func TestFlowAliasResolves(t *testing.T) {
	data := []byte(`shared: &shared
  type: string
  description: reused
oneOf: [*shared]
props: { x: *shared }
`)
	root, err := LoadFile("flow-alias.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)

	// oneOf: [*shared] — the alias is a flow sequence item and must resolve to
	// the anchored map, not the literal string "*shared".
	oneOf := seqOf(t, root, "oneOf")
	if len(oneOf.Items) != 1 {
		t.Fatalf("oneOf has %d items, want 1", len(oneOf.Items))
	}
	item, ok := oneOf.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("oneOf[0] is %T, want *MapNode (alias not resolved)", oneOf.Items[0])
	}
	if got := findKey(item, "type").(*ScalarNode).Value; got != "string" {
		t.Errorf("oneOf[0].type = %v, want string", got)
	}

	// props: { x: *shared } — the alias is a flow mapping value.
	props := findKey(rootMap, "props").(*MapNode)
	x, ok := findKey(props, "x").(*MapNode)
	if !ok {
		t.Fatalf("props.x is %T, want *MapNode (alias not resolved)", findKey(props, "x"))
	}
	if got := findKey(x, "description").(*ScalarNode).Value; got != "reused" {
		t.Errorf("props.x.description = %v, want reused", got)
	}

	// The alias is a deep copy, not the anchored node itself.
	if item == findKey(rootMap, "shared").(*MapNode) {
		t.Error("flow alias shares the anchored node; want an independent copy")
	}
}

// TestFlowAliasUnknownFailsLoud verifies an unresolvable alias inside a flow
// collection fails loud rather than degrading to the literal string (L-2).
func TestFlowAliasUnknownFailsLoud(t *testing.T) {
	data := []byte("oneOf: [*missing]\n")
	if _, err := LoadFile("flow-bad-alias.yaml", data); err == nil {
		t.Fatal("expected error for unknown flow alias")
	} else if !strings.Contains(err.Error(), "unknown YAML alias *missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestAliasExpansionBombRejected verifies that a nested-alias document that
// would amplify exponentially during lexing is rejected by the alias-expansion
// guardrails rather than exhausting memory (H-1). Each level aliases the
// previous level ten times, so the deepest clone would be 10^levels items; the
// cumulative expansion-size cap fires before the blowup materializes.
func TestAliasExpansionBombRejected(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("base: &base\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "  - x%d\n", i)
	}
	prev := "base"
	for lvl := 0; lvl < 6; lvl++ {
		name := fmt.Sprintf("lvl%d", lvl)
		fmt.Fprintf(&sb, "%s: &%s\n", name, name)
		for j := 0; j < 10; j++ {
			fmt.Fprintf(&sb, "  - *%s\n", prev)
		}
		prev = name
	}
	_, err := LoadFile("bomb.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected alias expansion limit error, got nil")
	}
	if !strings.Contains(err.Error(), "alias expansion limit exceeded") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestAliasExpansionCountLimitRejected verifies the alias-count cap fires for a
// document with many distinct alias resolutions even when each clone is small.
func TestAliasExpansionCountLimitRejected(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("base: &base\n  - x\n")
	for i := 0; i < maxAliasExpansions+1; i++ {
		fmt.Fprintf(&sb, "k%d: *base\n", i)
	}
	_, err := LoadFile("bomb-count.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatal("expected alias expansion limit error, got nil")
	}
	if !strings.Contains(err.Error(), "alias expansion limit exceeded") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestApostropheInUnquotedKey asserts an apostrophe inside an unquoted key is a
// literal character, not a quote delimiter.
func TestApostropheInUnquotedKey(t *testing.T) {
	data := []byte("Let's Encrypt Certificate:\n  value: 1\n")
	root, err := LoadFile("apostrophe.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)
	if len(rootMap.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rootMap.Entries))
	}
	if got := rootMap.Entries[0].Key.Value; got != "Let's Encrypt Certificate" {
		t.Errorf("key = %q, want %q", got, "Let's Encrypt Certificate")
	}
}

// TestQuotedKeyWithColon ensures a quoted key still shields its internal colon
// from being treated as a mapping separator after the splitMapping fix.
func TestQuotedKeyWithColon(t *testing.T) {
	data := []byte(`"a:b": value
`)
	root, err := LoadFile("qkey.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)
	if len(rootMap.Entries) != 1 || rootMap.Entries[0].Key.Value != "a:b" {
		t.Fatalf("quoted key not parsed: %#v", rootMap.Entries)
	}
	if got := rootMap.Entries[0].Value.(*ScalarNode).Value; got != "value" {
		t.Errorf("value = %v, want value", got)
	}
}

// TestQuotedKeyInSequenceItem ensures a quoted key on a sequence-item inline
// mapping (`- "$ref": "..."`, the shape OpenAPI specs use for $ref-only
// operation parameters) is unquoted. Before the parseSingleMapping fix the
// surrounding quotes were retained in the ScalarNode value, so key comparisons
// against "$ref" never matched and the $ref was silently dropped — producing
// empty-name schema attributes and unresolved path parameters (L-101).
func TestQuotedKeyInSequenceItem(t *testing.T) {
	data := []byte("parameters:\n- \"$ref\": \"#/components/parameters/gist-id\"\n")
	root, err := LoadFile("seqref.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)
	params := rootMap.Entries[0].Value.(*SequenceNode)
	if len(params.Items) != 1 {
		t.Fatalf("expected 1 sequence item, got %d", len(params.Items))
	}
	item := params.Items[0].(*MapNode)
	if len(item.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(item.Entries))
	}
	if got := item.Entries[0].Key.Value; got != "$ref" {
		t.Errorf("key = %q, want %q (quotes must be stripped)", got, "$ref")
	}
	if got := item.Entries[0].Value.(*ScalarNode).Value; got != "#/components/parameters/gist-id" {
		t.Errorf("value = %q, want %q", got, "#/components/parameters/gist-id")
	}
}

// TestYAMLFlowUnquotedKeys parses a single-line flow mapping with unquoted keys
// ({ type: string }), which the JSON parser rejects but real specs use (e.g.
// GigaVUE-FM's openapi.fm.yaml writes parameter schemas this way).
func TestYAMLFlowUnquotedKeys(t *testing.T) {
	data := []byte(`parameters:
  - name: alias
    in: path
    schema: { type: string }
`)
	root, err := LoadFile("flow-unquoted.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	param := seqOf(t, root, "parameters").Items[0].(*MapNode)
	schema := findKey(param, "schema").(*MapNode)
	if got := findKey(schema, "type").(*ScalarNode).Value; got != "string" {
		t.Errorf("schema.type = %v, want string", got)
	}
}

// TestYAMLFlowNestedAndQuoted exercises nested flow collections and quoted
// keys/values inside a single-line flow value.
func TestYAMLFlowNestedAndQuoted(t *testing.T) {
	data := []byte(`a: { items: [1, "two", true], "quoted key": 'it''s' }
b: [ { x: 1 }, { x: 2 } ]
`)
	root, err := LoadFile("flow-nested.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	rootMap := root.(*MapNode)
	a := findKey(rootMap, "a").(*MapNode)
	items := findKey(a, "items").(*SequenceNode)
	if len(items.Items) != 3 {
		t.Fatalf("items has %d entries, want 3", len(items.Items))
	}
	if got := items.Items[0].(*ScalarNode).Value; got != float64(1) {
		t.Errorf("items[0] = %v, want 1", got)
	}
	if got := items.Items[1].(*ScalarNode).Value; got != "two" {
		t.Errorf("items[1] = %v, want two", got)
	}
	if got := items.Items[2].(*ScalarNode).Value; got != true {
		t.Errorf("items[2] = %v, want true", got)
	}
	if got := findKey(a, "quoted key").(*ScalarNode).Value; got != "it's" {
		t.Errorf("quoted key value = %q, want it's", got)
	}
	b := seqOf(t, root, "b")
	if len(b.Items) != 2 {
		t.Fatalf("b has %d items, want 2", len(b.Items))
	}
	if got := findKey(b.Items[0].(*MapNode), "x").(*ScalarNode).Value; got != float64(1) {
		t.Errorf("b[0].x = %v, want 1", got)
	}
}

// TestYAMLFlowNullValue parses a flow mapping entry with a bare key (null value)
// and an explicit null.
func TestYAMLFlowNullValue(t *testing.T) {
	data := []byte(`a: { x: , y: null, z: ~ }
`)
	root, err := LoadFile("flow-null.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	a := findKey(root.(*MapNode), "a").(*MapNode)
	for _, k := range []string{"x", "y", "z"} {
		if got := findKey(a, k).(*ScalarNode).Value; got != nil {
			t.Errorf("%s = %v, want nil", k, got)
		}
	}
}

// TestYAMLFlowLookalikeFallsBackToScalar asserts a value that merely looks like
// flow (starts and ends with braces but is not a collection) still parses as a
// plain string rather than erroring.
func TestYAMLFlowLookalikeFallsBackToScalar(t *testing.T) {
	data := []byte("description: {foo}\n")
	root, err := LoadFile("flow-lookalike.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := scalarOf(t, root, "description"); got != "{foo}" {
		t.Errorf("description = %q, want {foo}", got)
	}
}

// TestYAMLFlowScalarsAfterChanges re-checks inline flow collections still parse
// after the value-routing changes (regression guard).
func TestYAMLFlowScalarsAfterChanges(t *testing.T) {
	data := []byte(`tags: ["a", "b"]
props: {"x": 1}
`)
	root, err := LoadFile("flow.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	tags := seqOf(t, root, "tags")
	if len(tags.Items) != 2 {
		t.Fatalf("tags has %d items, want 2", len(tags.Items))
	}
	props := findKey(root.(*MapNode), "props").(*MapNode)
	if len(props.Entries) != 1 {
		t.Fatalf("props has %d entries, want 1", len(props.Entries))
	}
}

// TestValueContainingCommentAfterApostrophe ensures a comment following a
// scalar containing an apostrophe is still stripped (stripYAMLComment fix).
func TestValueContainingCommentAfterApostrophe(t *testing.T) {
	data := []byte(`note: Let's do it # trailing comment
`)
	root, err := LoadFile("apost.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := scalarOf(t, root, "note"); got != "Let's do it" {
		t.Errorf("note = %q, want %q", got, "Let's do it")
	}
}

// TestCompactNestedSequenceDeep parses a triple-nested compact sequence.
func TestCompactNestedSequenceDeep(t *testing.T) {
	data := []byte(`grid:
  - - - a
`)
	root, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	grid := seqOf(t, root, "grid")
	l1 := grid.Items[0].(*SequenceNode)
	l2 := l1.Items[0].(*SequenceNode)
	if got := l2.Items[0].(*ScalarNode).Value; got != "a" {
		t.Errorf("deepest item = %v, want a", got)
	}
}

// TestCompactNestedSequenceDepthGuard ensures a pathological one-line compact
// sequence ("- - - ... a") hits ErrMaxNestingDepth instead of exhausting the
// goroutine stack (C-1).
func TestCompactNestedSequenceDepthGuard(t *testing.T) {
	data := []byte(strings.Repeat("- ", 20000) + "a")
	_, err := LoadFile("deep.yaml", data)
	if !errors.Is(err, ErrMaxNestingDepth) {
		t.Fatalf("LoadFile err = %v, want ErrMaxNestingDepth", err)
	}
}

// TestFlowMappingAsSequenceItem ensures a flow map used as a block sequence
// item ("- {name: limit, ...}") is parsed as a flow mapping, not mangled into
// a junk "{name" key (C-2).
func TestFlowMappingAsSequenceItem(t *testing.T) {
	data := []byte("parameters:\n- {name: limit, in: query, schema: {type: integer}}\n")
	root, err := LoadFile("flowseq.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	params := seqOf(t, root, "parameters")
	if len(params.Items) != 1 {
		t.Fatalf("parameters has %d items, want 1", len(params.Items))
	}
	item, ok := params.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("item is %T, want *MapNode", params.Items[0])
	}
	if len(item.Entries) != 3 {
		t.Fatalf("item has %d entries, want 3", len(item.Entries))
	}
	if got := item.Entries[0].Key.Value; got != "name" {
		t.Errorf("first key = %q, want name", got)
	}
	if got := item.Entries[0].Value.(*ScalarNode).Value; got != "limit" {
		t.Errorf("first value = %q, want limit", got)
	}
}

// TestCloneNodeCopiesTrees exercises cloneNode directly on a mixed tree.
func TestCloneNodeCopiesTrees(t *testing.T) {
	orig := &MapNode{
		Entries: []MapEntry{{
			Key:   &ScalarNode{Value: "k", Raw: "k"},
			Value: &SequenceNode{Items: []Node{&ScalarNode{Value: "v", Raw: "v"}}},
		}},
	}
	c := cloneNode(orig).(*MapNode)
	orig.Entries[0].Value.(*SequenceNode).Items[0].(*ScalarNode).Value = "changed"
	got := c.Entries[0].Value.(*SequenceNode).Items[0].(*ScalarNode).Value
	if got != "v" {
		t.Errorf("clone shares mutation: got %q, want v", got)
	}
}
