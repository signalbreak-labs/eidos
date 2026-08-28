package parser

import (
	"strings"
	"testing"
)

// TestParseHex4Short drives the len != 4 guard of parseHex4.
func TestParseHex4Short(t *testing.T) {
	if _, ok := parseHex4("ab"); ok {
		t.Error("parseHex4(\"ab\") = ok, want false for a 2-char slice")
	}
	if _, ok := parseHex4("abcde"); ok {
		t.Error("parseHex4(\"abcde\") = ok, want false for a 5-char slice")
	}
}

// TestParseHexNShort drives the len != n guard of parseHexN.
func TestParseHexNShort(t *testing.T) {
	if _, ok := parseHexN("ab", 4); ok {
		t.Error("parseHexN(\"ab\", 4) = ok, want false")
	}
	if _, ok := parseHexN("abcdef", 4); ok {
		t.Error("parseHexN(\"abcdef\", 4) = ok, want false")
	}
}

// TestJSONMalformedInputs drives the error branches of the JSON lexer with
// malformed documents. Each case must fail with a parse error.
func TestJSONMalformedInputs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // substring expected in the error
	}{
		{"unterminated object", `{`, "unterminated object"},
		{"unterminated object after value", `{"a":1`, "expected ',' or '}'"},
		{"unquoted key", `{a:1}`, "object key must be a string"},
		{"missing colon", `{"a" 1}`, "expected"},
		{"missing comma in object", `{"a":1 "b":2}`, "expected ',' or '}'"},
		{"missing comma in array", `[1 2]`, "expected ',' or ']'"},
		{"top-level close brace", `}`, "expected"},
		{"top-level close bracket", `]`, "expected"},
		{"unterminated string", `"abc`, "unterminated string"},
		{"unterminated escape", `"abc\`, "unterminated string escape"},
		{"invalid escape", `"\q"`, "invalid escape sequence"},
		{"control char in string", "\"a\x01b\"", "invalid control character"},
		{"unicode escape too short", `"\u12"`, "invalid unicode escape"},
		{"unicode escape bad hex", `"\uZZZZ"`, "invalid unicode escape"},
		{"empty literal", `{"a": }`, "expected literal value"},
		{"unterminated single quote literal", `{"a": 'x}`, "invalid literal"},
		{"plus number", `{"a": +1}`, "invalid JSON literal"},
		{"leading zero number", `{"a": 01}`, "invalid JSON literal"},
		{"bare minus", `{"a": -}`, "invalid JSON literal"},
		{"trailing dot", `{"a": 1.}`, "invalid JSON literal"},
		{"bare exponent", `{"a": 1e}`, "invalid JSON literal"},
		{"trailing data", `{"a":1} x`, "unexpected trailing data"},
		{"empty document", `   `, "empty document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFileAsJSON("spec.json", []byte(tc.in))
			if err == nil {
				t.Fatalf("LoadFileAsJSON(%q) succeeded, want error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

// TestJSONCarriageReturnWhitespace drives the '\r' branch of skipWhitespace:
// a trailing carriage return after a complete document is skipped before the
// end-of-input check.
func TestJSONCarriageReturnWhitespace(t *testing.T) {
	node, err := LoadFileAsJSON("spec.json", []byte("{\"a\":1}\r"))
	if err != nil {
		t.Fatalf("LoadFileAsJSON with trailing CR: %v", err)
	}
	m, ok := node.(*MapNode)
	if !ok || len(m.Entries) != 1 {
		t.Fatalf("expected 1-entry map, got %#v", node)
	}
}

// TestJSONBMPUnicodeEscape drives the default branch of appendUnicodeEscape
// (a plain BMP code point, not a surrogate pair).
func TestJSONBMPUnicodeEscape(t *testing.T) {
	node, err := LoadFileAsJSON("spec.json", []byte(`{"a":"\u0041"}`))
	if err != nil {
		t.Fatalf("LoadFileAsJSON: %v", err)
	}
	m := node.(*MapNode)
	if v, ok := m.Entries[0].Value.(*ScalarNode); !ok || v.Value != "A" {
		t.Errorf("expected decoded A, got %#v", m.Entries[0].Value)
	}
}

// TestJSONMalformedKeyString drives the parseString error branch inside
// parseObject (a malformed key, not just an unquoted one).
func TestJSONMalformedKeyString(t *testing.T) {
	_, err := LoadFileAsJSON("spec.json", []byte(`{"\q":1}`))
	if err == nil {
		t.Fatal("expected an error for an invalid escape in a key")
	}
	if !strings.Contains(err.Error(), "invalid escape") {
		t.Errorf("error %q does not mention the invalid escape", err)
	}
}

// TestJSONDeepNesting drives the enterComposite depth guard for both objects
// and arrays. Arrays nest directly; objects nest through a key/value pair.
func TestJSONDeepNesting(t *testing.T) {
	deep := strings.Repeat("[", maxNestingDepth+1) + strings.Repeat("]", maxNestingDepth+1)
	if _, err := LoadFileAsJSON("spec.json", []byte(deep)); err == nil {
		t.Error("expected a nesting-depth error for deeply nested arrays")
	}
	deepObj := strings.Repeat(`{"a":`, maxNestingDepth+1) + strings.Repeat("}", maxNestingDepth+1)
	if _, err := LoadFileAsJSON("spec.json", []byte(deepObj)); err == nil {
		t.Error("expected a nesting-depth error for deeply nested objects")
	}
}
