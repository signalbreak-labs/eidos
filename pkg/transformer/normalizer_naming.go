package transformer

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var pathParamRegex = regexp.MustCompile(`\{[^}]*\}`)

// ToSnakeCase sanitizes an identifier into lower-case snake_case. It handles
// camelCase, PascalCase, kebab-case, space-separated phrases, and mixed inputs.
// Identifiers that would begin with a digit (e.g. "2fa" -> "2_fa") are prefixed
// with "x", since neither Go identifiers nor Terraform/HCL identifiers may start
// with a digit; the prefix is idempotent (ToSnakeCase("x2_fa") == "x2_fa")
// (L-99). DeriveOperationID and NormalizeOperationID inherit this guard because
// they delegate to ToSnakeCase.
func ToSnakeCase(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return ""
	}
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	result := strings.Join(words, "_")
	if result != "" {
		if r, _ := utf8.DecodeRuneInString(result); unicode.IsDigit(r) {
			result = "x" + result
		}
	}
	return result
}

// reservedRootNames are Terraform root attribute/block names that a schema
// attribute may not use (the framework rejects them as reserved). A spec
// property that normalizes to one of these (e.g. SSO provider settings'
// "provider") is suffixed with "_" so the generated schema stays valid (G14).
var reservedRootNames = map[string]bool{
	"provider": true, "provisioner": true, "connection": true,
	"count": true, "depends_on": true, "for_each": true, "lifecycle": true,
}

// SanitizeAttributeName returns a Terraform-valid attribute name for a spec
// property name: snake_case, digit-guarded (via ToSnakeCase), and suffixed with
// "_" when the result is a reserved root name.
func SanitizeAttributeName(name string) string {
	snake := ToSnakeCase(name)
	if reservedRootNames[snake] {
		return snake + "_"
	}
	return snake
}

// ToPascalCase sanitizes an identifier into PascalCase. It handles camelCase,
// snake_case, kebab-case, space-separated phrases, and mixed inputs.
func ToPascalCase(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return ""
	}
	for i, w := range words {
		words[i] = capitalize(w)
	}
	return strings.Join(words, "")
}

// DeriveOperationID generates a snake_case operationId from the HTTP method and
// path when the spec does not provide one. Path parameters are stripped so the
// derived name is stable across placeholder naming differences.
func DeriveOperationID(method, path string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	path = strings.TrimSpace(path)

	parts := make([]string, 0)
	if method != "" {
		parts = append(parts, method)
	}

	if path != "" {
		path = strings.Trim(path, "/")
		path = pathParamRegex.ReplaceAllString(path, "")
		path = collapseSlashes(path)
		for _, seg := range strings.Split(path, "/") {
			seg = strings.TrimSpace(seg)
			if seg != "" {
				parts = append(parts, seg)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return ToSnakeCase(strings.Join(parts, "_"))
}

// NormalizeOperationID sanitizes an explicit operationId to snake_case. If the
// explicit id is empty or sanitizes to an empty string, it derives one from
// method and path so the result is always a usable identifier.
func NormalizeOperationID(operationID, method, path string) string {
	if strings.TrimSpace(operationID) != "" {
		if sanitized := ToSnakeCase(operationID); sanitized != "" {
			return sanitized
		}
	}
	return DeriveOperationID(method, path)
}

// splitWords breaks an identifier into words using case transitions and
// non-alphanumeric separators. Digits are kept attached to the preceding word
// (e.g. "v2" stays "v2") unless they appear at the start of a word. A letter
// that immediately follows a digit starts a new word segment (e.g. "v2alpha"
// splits into "v2" and "alpha").
func splitWords(s string) []string {
	runes := []rune(s)
	var words []string
	var current []rune

	for i, r := range runes {
		prev := rune(0)
		if i > 0 {
			prev = runes[i-1]
		}
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		if !isWordRune(r) {
			if len(current) > 0 {
				words = append(words, string(current))
				current = nil
			}
			continue
		}

		newWord := false
		switch {
		case len(current) == 0:
			newWord = true
		case !isWordRune(prev):
			newWord = true
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || (unicode.IsUpper(prev) && unicode.IsLower(next))):
			newWord = true
		case unicode.IsLetter(r) && unicode.IsDigit(prev):
			newWord = true
		}

		if newWord && len(current) > 0 {
			words = append(words, string(current))
			current = nil
		}
		current = append(current, r)
	}

	if len(current) > 0 {
		words = append(words, string(current))
	}

	return words
}

// isWordRune reports whether a rune is a letter or digit.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// capitalize returns the word with its first rune upper-cased and the
// remaining runes lower-cased.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// collapseSlashes removes leading and trailing slashes and collapses multiple
// consecutive slashes into a single slash.
func collapseSlashes(s string) string {
	s = strings.Trim(s, "/")
	var b strings.Builder
	b.Grow(len(s))
	lastSlash := false
	for _, r := range s {
		if r == '/' {
			if !lastSlash {
				b.WriteRune(r)
			}
			lastSlash = true
			continue
		}
		lastSlash = false
		b.WriteRune(r)
	}
	return strings.TrimSuffix(b.String(), "/")
}
