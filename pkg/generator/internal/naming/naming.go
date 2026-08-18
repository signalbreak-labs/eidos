// Package naming contains the pure identifier-transformation helpers shared by
// the generator and its internal/schema subpackage. Keeping them here, rather
// than in either caller, guarantees the resolved Go field names, model type
// names, and file paths stay identical no matter which package derives them —
// field names must match between schema-generated and provider-generated code.
package naming

import (
	"strings"
	"unicode"
)

// SplitIdentifier breaks a string into words, splitting only on non-alphanumeric
// runes (underscore, dash, space, dot, ...). It deliberately does not split
// camelCase or digit/letter boundaries: SnakeCase relies on this to leave
// "MyCloud" as a single word (SnakeCase("MyCloud") == "mycloud"), and the
// H-7 foo_bar/fooBar collision is resolved by the schema package's
// ResolveFieldNames collision detection rather than by splitting, since
// camelCase splitting would not disambiguate them anyway (both still normalize
// to "FooBar").
func SplitIdentifier(s string) []string {
	var parts []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			parts = append(parts, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return parts
}

// PascalCase converts an identifier to PascalCase.
func PascalCase(s string) string {
	parts := SplitIdentifier(s)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

// SanitizeGoIdentifier ensures s is a valid, exported Go identifier. An empty
// result becomes "X"; a result beginning with a non-letter (e.g. a digit) is
// prefixed with "X". SplitIdentifier already strips everything except letters
// and digits, so only the first rune and emptiness need correcting here.
func SanitizeGoIdentifier(s string) string {
	if s == "" {
		return "X"
	}
	r := []rune(s)
	if !unicode.IsLetter(r[0]) {
		return "X" + s
	}
	return s
}

// GoTypeName returns a Go-exported type identifier from an arbitrary name. Like
// GoFieldName it validates the result so user-controlled names cannot produce
// invalid Go identifiers: a resource/data source/function/ephemeral/list name
// of "2fa" yields "X2fa" (not the invalid "2fa"), and "---" or "" yield "X".
// Used for generated struct and type names derived from IR construct names
// (M-10); GoFieldName is the same transformation for model field names.
func GoTypeName(s string) string {
	return SanitizeGoIdentifier(PascalCase(s))
}

// GoFieldName returns a Go-exported field name from an attribute name. Unlike
// PascalCase, it validates the result so hostile spec property names cannot
// produce invalid Go identifiers: a property named "2fa" yields "X2fa" (not the
// invalid "2fa"), and "---" or "" yield "X".
func GoFieldName(s string) string {
	return GoTypeName(s)
}

// CamelCase converts an identifier to lower-camelCase.
func CamelCase(s string) string {
	pc := PascalCase(s)
	if pc == "" {
		return ""
	}
	r := []rune(pc)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// SnakeCase converts an identifier to lower_snake_case, splitting on
// non-alphanumeric characters and underscores.
func SnakeCase(s string) string {
	parts := SplitIdentifier(s)
	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}
	return strings.Join(parts, "_")
}
