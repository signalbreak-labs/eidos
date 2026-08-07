package parser

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLoadJSONObject(t *testing.T) {
	data := []byte(`{
		"openapi": "3.1.0",
		"info": {
			"title": "Test API",
			"version": 1
		},
		"tags": ["pets", "store"]
	}`)

	node, err := LoadFile("test.json", data)
	if err != nil {
		t.Fatalf("LoadFile json: %v", err)
	}
	m, ok := node.(*MapNode)
	if !ok {
		t.Fatalf("expected *MapNode, got %T", node)
	}
	if got := len(m.Entries); got != 3 {
		t.Fatalf("expected 3 top-level entries, got %d", got)
	}

	info := findKey(m, "info")
	if info == nil {
		t.Fatalf("missing info entry")
	}
	infoMap, ok := info.(*MapNode)
	if !ok {
		t.Fatalf("info should be a map, got %T", info)
	}
	if title := findKey(infoMap, "title"); title == nil {
		t.Fatalf("missing title")
	} else if s, ok := title.(*ScalarNode); !ok || s.Value != "Test API" {
		t.Fatalf("unexpected title: %+v", title)
	}

	tags := findKey(m, "tags")
	if tags == nil {
		t.Fatalf("missing tags")
	}
	seq, ok := tags.(*SequenceNode)
	if !ok {
		t.Fatalf("tags should be a sequence, got %T", tags)
	}
	if len(seq.Items) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(seq.Items))
	}
}

func TestLoadJSONArray(t *testing.T) {
	data := []byte(`[null, true, false, 42, 3.14, "hello"]`)
	node, err := LoadFile("test.json", data)
	if err != nil {
		t.Fatalf("LoadFile json array: %v", err)
	}
	seq, ok := node.(*SequenceNode)
	if !ok {
		t.Fatalf("expected *SequenceNode, got %T", node)
	}
	want := []any{nil, true, false, float64(42), 3.14, "hello"}
	if len(seq.Items) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(seq.Items))
	}
	for i, w := range want {
		s, ok := seq.Items[i].(*ScalarNode)
		if !ok {
			t.Fatalf("item %d not scalar: %T", i, seq.Items[i])
		}
		if !reflect.DeepEqual(s.Value, w) {
			t.Fatalf("item %d: got %v, want %v", i, s.Value, w)
		}
	}
}

func TestLoadYAMLMapping(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com
    description: Production
paths:
  /pets:
    get:
      operationId: listPets
`)

	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml: %v", err)
	}
	root, ok := node.(*MapNode)
	if !ok {
		t.Fatalf("expected *MapNode, got %T", node)
	}

	info := findKey(root, "info")
	if info == nil {
		t.Fatalf("missing info")
	}
	infoMap, ok := info.(*MapNode)
	if !ok {
		t.Fatalf("info should be map, got %T", info)
	}
	if title := findKey(infoMap, "title"); title == nil {
		t.Fatalf("missing title")
	} else if s, ok := title.(*ScalarNode); !ok || s.Value != "Test API" {
		t.Fatalf("unexpected title: %+v", title)
	}

	servers := findKey(root, "servers")
	if servers == nil {
		t.Fatalf("missing servers")
	}
	seq, ok := servers.(*SequenceNode)
	if !ok || len(seq.Items) != 1 {
		t.Fatalf("servers should be sequence with 1 item, got %+v", servers)
	}
	server, ok := seq.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("server item should be map, got %T", seq.Items[0])
	}
	if url := findKey(server, "url"); url == nil {
		t.Fatalf("missing url")
	} else if s, ok := url.(*ScalarNode); !ok || s.Value != "https://api.example.com" {
		t.Fatalf("unexpected url: %+v", url)
	}
}

func TestLoadYAMLSequence(t *testing.T) {
	data := []byte(`- alpha
- 123
- true
- null
- nested:
    key: value
`)

	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml sequence: %v", err)
	}
	seq, ok := node.(*SequenceNode)
	if !ok {
		t.Fatalf("expected *SequenceNode, got %T", node)
	}
	if len(seq.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(seq.Items))
	}
	checks := []any{"alpha", float64(123), true, nil, nil}
	for i, w := range checks {
		if i == 4 {
			m, ok := seq.Items[i].(*MapNode)
			if !ok {
				t.Fatalf("item 4 should be map, got %T", seq.Items[i])
			}
			nested := findKey(m, "nested")
			if nested == nil {
				t.Fatalf("missing nested key")
			}
			nestedMap, ok := nested.(*MapNode)
			if !ok {
				t.Fatalf("nested should be map, got %T", nested)
			}
			if v := findKey(nestedMap, "key"); v == nil {
				t.Fatalf("missing key under nested")
			} else if s, ok := v.(*ScalarNode); !ok || s.Value != "value" {
				t.Fatalf("unexpected nested value: %+v", v)
			}
			continue
		}
		s, ok := seq.Items[i].(*ScalarNode)
		if !ok {
			t.Fatalf("item %d not scalar: %T", i, seq.Items[i])
		}
		if !reflect.DeepEqual(s.Value, w) {
			t.Fatalf("item %d: got %v, want %v", i, s.Value, w)
		}
	}
}

func TestLineNumbersPreserved(t *testing.T) {
	jsonData := []byte(`{
  "info": {
    "title": "T"
  }
}`)
	node, err := LoadFile("api.json", jsonData)
	if err != nil {
		t.Fatalf("json line numbers: %v", err)
	}
	root := node.(*MapNode)
	if root.Line != 1 {
		t.Fatalf("root line = %d, want 1", root.Line)
	}
	info := findKey(root, "info").(*MapNode)
	if info.Line != 2 {
		t.Fatalf("info line = %d, want 2", info.Line)
	}
	title := findKey(info, "title").(*ScalarNode)
	if title.Line != 3 {
		t.Fatalf("title line = %d, want 3", title.Line)
	}

	yamlData := []byte(`openapi: 3.0.3
info:
  title: T
  version: "1"
`)
	node, err = LoadFile("api.yaml", yamlData)
	if err != nil {
		t.Fatalf("yaml line numbers: %v", err)
	}
	root = node.(*MapNode)
	if root.Line != 1 {
		t.Fatalf("yaml root line = %d, want 1", root.Line)
	}
	info = findKey(root, "info").(*MapNode)
	if info.Line != 2 {
		t.Fatalf("yaml info line = %d, want 2", info.Line)
	}
	title = findKey(info, "title").(*ScalarNode)
	if title.Line != 3 {
		t.Fatalf("yaml title line = %d, want 3", title.Line)
	}
}

func TestYAMLCommentsAndBlankLines(t *testing.T) {
	data := []byte(`
# leading comment
openapi: 3.0.3

# info section
info:
  title: Test # inline comment
  version: 1
`)
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml comments: %v", err)
	}
	root := node.(*MapNode)
	if v := findKey(root, "openapi"); v == nil {
		t.Fatalf("missing openapi")
	} else if s := v.(*ScalarNode); s.Value != "3.0.3" {
		t.Fatalf("unexpected openapi: %v", s.Value)
	}
	info := findKey(root, "info").(*MapNode)
	if title := findKey(info, "title"); title == nil {
		t.Fatalf("missing title")
	} else if s := title.(*ScalarNode); s.Value != "Test" {
		t.Fatalf("title should not include comment: %v", s.Value)
	}
}

// TestYAMLHashInsideScalar is a regression test for H-16: stripYAMLComment must
// only cut at a '#' that begins a YAML comment (preceded by a separation space
// or at the start of the value). A '#' inside an unquoted scalar — such as
// "Use C#" or a URL fragment "https://x.com/a#frag" — must be preserved, while a
// real inline comment following whitespace must still be stripped.
func TestYAMLHashInsideScalar(t *testing.T) {
	data := []byte(`
openapi: 3.0.3
info:
  title: Use C# or F#
  url: https://x.com/a#frag
  tag: value # real comment
`)
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root := node.(*MapNode)
	info := findKey(root, "info").(*MapNode)

	if s := findKey(info, "title").(*ScalarNode); s.Value != "Use C# or F#" {
		t.Fatalf("title with inline '#' corrupted: %q", s.Value)
	}
	if s := findKey(info, "url").(*ScalarNode); s.Value != "https://x.com/a#frag" {
		t.Fatalf("url fragment dropped: %q", s.Value)
	}
	if s := findKey(info, "tag").(*ScalarNode); s.Value != "value" {
		t.Fatalf("real inline comment not stripped: %q", s.Value)
	}
}

func TestYAMLInlineFlowCollections(t *testing.T) {
	data := []byte(`types: ["string", "null"]
config: {"enabled": true, "count": 42}
`)
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml flow: %v", err)
	}
	root := node.(*MapNode)
	types := findKey(root, "types").(*SequenceNode)
	if len(types.Items) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types.Items))
	}
	config := findKey(root, "config").(*MapNode)
	if enabled := findKey(config, "enabled"); enabled == nil {
		t.Fatalf("missing enabled")
	} else if s := enabled.(*ScalarNode); s.Value != true {
		t.Fatalf("unexpected enabled: %v", s.Value)
	}
}

func TestJSONEscapesAndUnicode(t *testing.T) {
	data := []byte(`{"line": "a\nb", "tab": "a\tb", "uni": "Aé"}`)
	node, err := LoadFile("test.json", data)
	if err != nil {
		t.Fatalf("LoadFile json escapes: %v", err)
	}
	root := node.(*MapNode)
	line := findKey(root, "line").(*ScalarNode)
	if line.Value != "a\nb" {
		t.Fatalf("line escape: got %q", line.Value)
	}
	uni := findKey(root, "uni").(*ScalarNode)
	if uni.Value != "Aé" {
		t.Fatalf("unicode escape: got %q", uni.Value)
	}
}

// TestJSONSurrogatePairEscape locks in the M-34 fix: a UTF-16 surrogate pair
// (😀) must combine into the supplementary code point it encodes
// (U+1F600, 😀) rather than decoding to two U+FFFD replacement characters.
func TestJSONSurrogatePairEscape(t *testing.T) {
	data := []byte(`{"emoji": "😀", "loneHigh": "\uD83D", "loneLow": "\uDE00"}`)
	node, err := LoadFile("surrogate.json", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root := node.(*MapNode)
	emoji := findKey(root, "emoji").(*ScalarNode)
	if emoji.Value != "😀" {
		t.Fatalf("surrogate pair: got %q want 😀", emoji.Value)
	}
	loneHigh := findKey(root, "loneHigh").(*ScalarNode)
	if loneHigh.Value != "�" {
		t.Fatalf("lone high surrogate: got %q want U+FFFD", loneHigh.Value)
	}
	loneLow := findKey(root, "loneLow").(*ScalarNode)
	if loneLow.Value != "�" {
		t.Fatalf("lone low surrogate: got %q want U+FFFD", loneLow.Value)
	}
}

// TestYAMLSurrogatePairEscape is the YAML double-quoted analog of
// TestJSONSurrogatePairEscape (M-34).
func TestYAMLSurrogatePairEscape(t *testing.T) {
	data := []byte("emoji: \"\\uD83D\\uDE00\"\n")
	node, err := LoadFile("surrogate.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root := node.(*MapNode)
	emoji := findKey(root, "emoji").(*ScalarNode)
	if emoji.Value != "😀" {
		t.Fatalf("surrogate pair: got %q want 😀", emoji.Value)
	}
}

func TestEmptyJSON(t *testing.T) {
	_, err := LoadFile("empty.json", []byte{})
	if err == nil {
		t.Fatalf("expected error for empty JSON")
	}
}

func TestEmptyYAML(t *testing.T) {
	_, err := LoadFile("empty.yaml", []byte{})
	if err == nil {
		t.Fatalf("expected error for empty YAML")
	}
}

func TestYAMLQuotedKeysWithColons(t *testing.T) {
	data := []byte("\"http://schema.org\": value\n'urn:some:thing': other\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml quoted keys: %v", err)
	}
	root := node.(*MapNode)
	if v := findKey(root, "http://schema.org"); v == nil {
		t.Fatalf("missing double-quoted URL key")
	} else if s, ok := v.(*ScalarNode); !ok || s.Value != "value" {
		t.Fatalf("unexpected value for quoted URL key: %+v", v)
	}
	if v := findKey(root, "urn:some:thing"); v == nil {
		t.Fatalf("missing single-quoted URN key")
	} else if s, ok := v.(*ScalarNode); !ok || s.Value != "other" {
		t.Fatalf("unexpected value for single-quoted URN key: %+v", v)
	}
}

func TestYAMLUnquotedColonKeyRejected(t *testing.T) {
	data := []byte("http://schema.org: value\n")
	_, err := LoadFile("test.yaml", data)
	if err == nil {
		t.Fatalf("expected error for unquoted key with colon")
	}
}

func TestYAMLEscapesInDoubleQuoted(t *testing.T) {
	data := []byte("line: \"a\\nb\"\ntab: \"a\\tb\"\nbackslash: \"a\\\\b\"\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml escapes: %v", err)
	}
	root := node.(*MapNode)
	line := findKey(root, "line").(*ScalarNode)
	if line.Value != "a\nb" {
		t.Fatalf("escape \\n: got %q", line.Value)
	}
	tab := findKey(root, "tab").(*ScalarNode)
	if tab.Value != "a\tb" {
		t.Fatalf("escape \\t: got %q", tab.Value)
	}
	bs := findKey(root, "backslash").(*ScalarNode)
	if bs.Value != "a\\b" {
		t.Fatalf("escape \\\\: got %q", bs.Value)
	}
}

func TestJSONInvalidEscapes(t *testing.T) {
	cases := []string{
		`{"x": "a\qb"}`,
		`{"x": "a\xb"}`,
		`{"x": "a\u00"}`,
		`{"x": "unterminated`,
		`{"x": "ok"} trailing`,
	}
	for _, c := range cases {
		if _, err := LoadFile("test.json", []byte(c)); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

// TestJSONRejectsInvalidLiterals locks in the M-33 fix: the JSON parser must
// reject malformed literals that inferScalar would otherwise accept as strings
// (unquoted words, leading-zero/multi-dot/signed numbers).
func TestJSONRejectsInvalidLiterals(t *testing.T) {
	cases := []string{
		`{"a": hello}`,
		`{"a": 01}`,
		`{"a": +1}`,
		`{"a": 1.2.3}`,
		`{"a": .5}`,
		`{"a": 1.}`,
		`{"a": --1}`,
	}
	for _, c := range cases {
		if _, err := LoadFile("test.json", []byte(c)); err == nil {
			t.Fatalf("expected error for invalid JSON literal %q", c)
		}
	}
}

// TestJSONAcceptsValidNumbers confirms strict number validation still accepts
// the full RFC 8259 number grammar (M-33).
func TestJSONAcceptsValidNumbers(t *testing.T) {
	cases := map[string]any{
		`{"a": 0}`:        float64(0),
		`{"a": -0}`:       float64(0),
		`{"a": 42}`:       float64(42),
		`{"a": 3.14}`:     float64(3.14),
		`{"a": 1e5}`:      float64(100000),
		`{"a": 1.5e-3}`:   float64(0.0015),
		`{"a": -1.0E+10}`: float64(-1e10),
	}
	for c, want := range cases {
		node, err := LoadFile("test.json", []byte(c))
		if err != nil {
			t.Fatalf("unexpected error for valid JSON %q: %v", c, err)
		}
		a := findKey(node.(*MapNode), "a").(*ScalarNode)
		if a.Value != want {
			t.Fatalf("for %q: got %v want %v", c, a.Value, want)
		}
	}
}

func TestDeepNestedYAML(t *testing.T) {
	data := []byte(`a:
  b:
    c:
      d:
        - 1
        - 2
      e: value
`)
	node, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile deep yaml: %v", err)
	}
	root := node.(*MapNode)
	a := findKey(root, "a").(*MapNode)
	b := findKey(a, "b").(*MapNode)
	c := findKey(b, "c").(*MapNode)
	d := findKey(c, "d").(*SequenceNode)
	if len(d.Items) != 2 {
		t.Fatalf("expected 2 items under d, got %d", len(d.Items))
	}
	e := findKey(c, "e").(*ScalarNode)
	if e.Value != "value" {
		t.Fatalf("unexpected e: %v", e.Value)
	}
}

func findKey(m *MapNode, key string) Node {
	for _, e := range m.Entries {
		if k, ok := e.Key.Value.(string); ok && k == key {
			return e.Value
		}
	}
	return nil
}

func TestYAMLUnterminatedSingleQuote(t *testing.T) {
	data := []byte("name: 'unterminated\n")
	_, err := LoadFile("test.yaml", data)
	if err == nil {
		t.Fatalf("expected error for unterminated single-quoted value")
	}
}

func TestYAMLSingleQuoteEscape(t *testing.T) {
	data := []byte("value: 'it''s here'\n'it''s here': other\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml single-quote escape: %v", err)
	}
	root := node.(*MapNode)
	v := findKey(root, "value").(*ScalarNode)
	if v.Value != "it's here" {
		t.Fatalf("single-quote escape value: got %q, want %q", v.Value, "it's here")
	}
	w := findKey(root, "it's here").(*ScalarNode)
	if w.Value != "other" {
		t.Fatalf("single-quote escape key: got %v, want %q", w.Value, "other")
	}
}

func TestYAMLDocumentMarkers(t *testing.T) {
	data := []byte("---\nopenapi: 3.0.3\n...\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml markers: %v", err)
	}
	root := node.(*MapNode)
	if v := findKey(root, "---"); v != nil {
		t.Fatalf("document start marker should not appear as a key")
	}
	if v := findKey(root, "openapi"); v == nil {
		t.Fatalf("missing openapi")
	}
}

// TestYAMLMultiDocumentStreamRejected locks in the M-35 fix: a multi-document
// YAML stream (two documents separated by ---) must be rejected rather than
// having document 2's keys silently merged into document 1's root map.
func TestYAMLMultiDocumentStreamRejected(t *testing.T) {
	data := []byte("openapi: \"3.0.3\"\ninfo:\n  title: A\n  version: \"1.0\"\n---\nopenapi: \"3.0.3\"\ninfo:\n  title: B\n  version: \"1.0\"\n")
	_, err := LoadFile("multi.yaml", data)
	if err == nil {
		t.Fatal("expected error for multi-document YAML stream, got nil")
	}
	if !strings.Contains(err.Error(), "extra content after document") {
		t.Fatalf("expected an extra-content error, got %v", err)
	}
}

// TestYAMLDocumentEndMarkerTerminates confirms a `...` end marker followed by
// a second document is rejected, and that a single document with a trailing
// `...` still parses (M-35).
func TestYAMLDocumentEndMarkerTerminates(t *testing.T) {
	// Single document with trailing end marker must still parse.
	single := []byte("openapi: \"3.0.3\"\n...\n")
	node, err := LoadFile("end.yaml", single)
	if err != nil {
		t.Fatalf("single doc with trailing ...: %v", err)
	}
	if findKey(node.(*MapNode), "openapi") == nil {
		t.Fatal("missing openapi in single doc")
	}

	// Two documents separated by ... must be rejected.
	multi := []byte("openapi: \"3.0.3\"\n...\nopenapi: \"3.0.3\"\n")
	if _, err := LoadFile("multi-end.yaml", multi); err == nil {
		t.Fatal("expected error for multi-document stream after ... marker")
	}
}

func TestYAMLBOM(t *testing.T) {
	data := []byte("\xef\xbb\xbfopenapi: 3.0.3\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml BOM: %v", err)
	}
	root := node.(*MapNode)
	if v := findKey(root, "openapi"); v == nil {
		t.Fatalf("missing openapi after BOM")
	}
}

func TestYAMLInlineFlowCollectionLineNumbers(t *testing.T) {
	data := []byte("types: [\"string\", \"null\"]\nconfig: {\"enabled\": true}\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml flow lines: %v", err)
	}
	root := node.(*MapNode)
	types := findKey(root, "types").(*SequenceNode)
	if types.Line != 1 || types.Column < 1 {
		t.Fatalf("types sequence location = %+v, want line 1, column > 0", types.SourceLocation)
	}
	for i, item := range types.Items {
		s := item.(*ScalarNode)
		if s.Line != 1 {
			t.Fatalf("types item %d line = %d, want 1", i, s.Line)
		}
	}
	config := findKey(root, "config").(*MapNode)
	if config.Line != 2 {
		t.Fatalf("config map line = %d, want 2", config.Line)
	}
	enabled := findKey(config, "enabled").(*ScalarNode)
	if enabled.Line != 2 {
		t.Fatalf("config.enabled line = %d, want 2", enabled.Line)
	}
}

func TestYAMLColumnTracking(t *testing.T) {
	data := []byte("openapi: 3.0.3\ninfo:\n  title: T\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml columns: %v", err)
	}
	root := node.(*MapNode)
	if root.Column != 1 {
		t.Fatalf("root column = %d, want 1", root.Column)
	}
	info := findKey(root, "info").(*MapNode)
	if info.Column != 1 {
		t.Fatalf("info column = %d, want 1", info.Column)
	}
	title := findKey(info, "title").(*ScalarNode)
	if title.Column != 10 {
		t.Fatalf("title value column = %d, want 10", title.Column)
	}
	if info.Entries[0].Key.Column != 3 {
		t.Fatalf("title key column = %d, want 3", info.Entries[0].Key.Column)
	}
}

func TestYAMLColumnTrackingDuplicateValue(t *testing.T) {
	// The value text "title" also appears as the key; column tracking must use
	// the post-colon position rather than strings.Index on the raw line.
	// Columns are 1-based. With a 2-space indent, "title" occupies columns 3-7,
	// ':' is at column 8, the separating space is at column 9, and the value
	// "title" therefore starts at column 10.
	data := []byte("info:\n  title: title\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml duplicate value columns: %v", err)
	}
	root := node.(*MapNode)
	info := findKey(root, "info").(*MapNode)
	title := findKey(info, "title").(*ScalarNode)
	if title.Column != 10 {
		t.Fatalf("title value column = %d, want 10", title.Column)
	}
}

func TestYAMLSequenceInlineMappingColumn(t *testing.T) {
	// Sequence inline mappings must report the value column, not inherit the
	// '-' marker column.
	data := []byte("- foo: bar\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml sequence mapping columns: %v", err)
	}
	seq := node.(*SequenceNode)
	item := seq.Items[0].(*MapNode)
	if item.Entries[0].Key.Column != 3 {
		t.Fatalf("sequence key column = %d, want 3", item.Entries[0].Key.Column)
	}
	bar := item.Entries[0].Value.(*ScalarNode)
	if bar.Column != 8 {
		t.Fatalf("sequence value column = %d, want 8", bar.Column)
	}
}

func TestYAMLInlineFlowCollectionColumns(t *testing.T) {
	// Items inside inline flow collections should preserve their relative column
	// offsets rather than all collapsing to the collection start column.
	data := []byte("types: [\"string\", \"null\"]\nconfig: {\"enabled\": true}\n")
	node, err := LoadFile("test.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile yaml flow collection columns: %v", err)
	}
	root := node.(*MapNode)

	types := findKey(root, "types").(*SequenceNode)
	if types.Column != 8 {
		t.Fatalf("types sequence column = %d, want 8", types.Column)
	}
	if s := types.Items[0].(*ScalarNode); s.Column != 9 {
		t.Fatalf("types[0] column = %d, want 9", s.Column)
	}
	if s := types.Items[1].(*ScalarNode); s.Column != 19 {
		t.Fatalf("types[1] column = %d, want 19", s.Column)
	}

	config := findKey(root, "config").(*MapNode)
	if config.Column != 9 {
		t.Fatalf("config map column = %d, want 9", config.Column)
	}
	if config.Entries[0].Key.Column != 10 {
		t.Fatalf("config key column = %d, want 10", config.Entries[0].Key.Column)
	}
	if config.Entries[0].Value.(*ScalarNode).Column != 21 {
		t.Fatalf("config value column = %d, want 21", config.Entries[0].Value.(*ScalarNode).Column)
	}
}

// TestYAMLVersionScalarTopLevelOnly asserts the openapi/swagger version-scalar
// special case fires only at the top level. A nested schema property named
// "openapi" must have its value parsed normally (here a bare number stays a
// float64, not string-coerced) (L-75).
func TestYAMLVersionScalarTopLevelOnly(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: T
  version: "1.0"
paths: {}
components:
  schemas:
    Weird:
      type: object
      properties:
        openapi:
          type: number
        swagger:
          type: number
`)
	node, err := LoadFile("nested-openapi.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root := node.(*MapNode)
	// Top-level openapi stays a string ("3.0.3") — the special case applies.
	top := findKey(root, "openapi").(*ScalarNode)
	if top.Value != "3.0.3" {
		t.Fatalf("top-level openapi: want string %q, got %v", "3.0.3", top.Value)
	}
	schemas := findKey(findKey(root, "components").(*MapNode), "schemas").(*MapNode)
	weird := findKey(schemas, "Weird").(*MapNode)
	props := findKey(weird, "properties").(*MapNode)
	// A nested property named "openapi" is a normal schema: its "type" value is
	// a plain string "number" (not version-coerced), and the property itself is
	// a mapping, not a string scalar.
	openapiProp := findKey(props, "openapi").(*MapNode)
	if findKey(openapiProp, "type") == nil {
		t.Fatalf("nested 'openapi' property should be a mapping with a type, got %T", props.Entries)
	}
}

// TestYAMLScalarRejectsNaNInf asserts that NaN/Inf scalars are kept as strings
// rather than float64, so the AST stays JSON-serializable (L-76).
func TestYAMLScalarRejectsNaNInf(t *testing.T) {
	for _, tc := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "inf"} {
		t.Run(tc, func(t *testing.T) {
			data := []byte(fmt.Sprintf("value: %s\n", tc))
			node, err := LoadFile("nan.yaml", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			s := findKey(node.(*MapNode), "value").(*ScalarNode)
			if _, isFloat := s.Value.(float64); isFloat {
				t.Fatalf("scalar %q was parsed as float64 %v; expected string to stay JSON-serializable", tc, s.Value)
			}
			if s.Value != tc {
				t.Fatalf("scalar %q: want string value %q, got %v", tc, tc, s.Value)
			}
		})
	}
}

// TestYAMLRootMisindentedKeyRejected asserts that a stray over-indented key
// following a root key with an inline scalar value is rejected rather than
// silently absorbed into the root map (L-77).
func TestYAMLRootMisindentedKeyRejected(t *testing.T) {
	data := []byte(`openapi: 3.0.3
  stray: value
`)
	_, err := LoadFile("misindented.yaml", data)
	if err == nil {
		t.Fatal("expected an error for an over-indented stray key at the root, got nil")
	}
}
