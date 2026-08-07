package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// ResolveLocalRef resolves a local JSON Pointer $ref value against the raw
// document root. The ref string must begin with '#'; non-local references are
// rejected with an error diagnostic. The returned node is the AST node that
// the pointer targets, or nil when resolution fails. Diagnostics always
// include the source location of the reference (refLoc).
//
// ResolveLocalRef is non-recursive: it resolves exactly the pointer given and
// does not follow any nested $ref values inside the returned subtree. Callers
// that walk resolved subtrees must guard against cyclic references themselves.
// Token positions reported in "Unresolvable $ref" diagnostics are 1-based.
func ResolveLocalRef(root Node, ref string, refLoc SourceLocation) (Node, []Diagnostic) {
	if root == nil {
		loc := refLoc
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Unresolvable $ref",
			Detail:         "Cannot resolve $ref against nil document.",
			SourceLocation: &loc,
		}}
	}

	if !strings.HasPrefix(ref, "#") {
		loc := refLoc
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Non-local $ref",
			Detail:         fmt.Sprintf("Only local JSON Pointer $ref values are supported, got %q.", ref),
			SourceLocation: &loc,
		}}
	}

	pointer := ref[1:]
	if pointer == "" {
		return root, nil
	}

	tokens, err := parsePointerTokens(pointer)
	if err != nil {
		loc := refLoc
		return nil, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Invalid JSON Pointer",
			Detail:         err.Error(),
			SourceLocation: &loc,
		}}
	}

	node := root
	for i, token := range tokens {
		switch n := node.(type) {
		case *MapNode:
			next := findMapEntry(n, token)
			if next == nil {
				loc := refLoc
				return nil, []Diagnostic{{
					Severity:       SeverityError,
					Summary:        "Unresolvable $ref",
					Detail:         fmt.Sprintf("Could not resolve token %q at position %d in %q.", token, i+1, ref),
					SourceLocation: &loc,
				}}
			}
			node = next.Value
		case *SequenceNode:
			idx, convErr := strconv.Atoi(token)
			if convErr != nil {
				loc := refLoc
				return nil, []Diagnostic{{
					Severity:       SeverityError,
					Summary:        "Unresolvable $ref",
					Detail:         fmt.Sprintf("Expected array index at position %d in %q, got %q.", i+1, ref, token),
					SourceLocation: &loc,
				}}
			}
			if idx < 0 || idx >= len(n.Items) {
				loc := refLoc
				return nil, []Diagnostic{{
					Severity:       SeverityError,
					Summary:        "Unresolvable $ref",
					Detail:         fmt.Sprintf("Array index %d out of bounds at position %d in %q.", idx, i+1, ref),
					SourceLocation: &loc,
				}}
			}
			node = n.Items[idx]
		default:
			var detail string
			if node == nil {
				detail = fmt.Sprintf("Cannot traverse into null at token %q (position %d) in %q.", token, i+1, ref)
			} else {
				detail = fmt.Sprintf("Cannot traverse into %T at token %q (position %d) in %q.", node, token, i+1, ref)
			}
			loc := refLoc
			return nil, []Diagnostic{{
				Severity:       SeverityError,
				Summary:        "Unresolvable $ref",
				Detail:         detail,
				SourceLocation: &loc,
			}}
		}
	}

	return node, nil
}

// parsePointerTokens splits a JSON Pointer (without the leading '#') into its
// decoded tokens. Per RFC 6901, '~' is encoded as '~0' and '/' as '~1'. An
// empty pointer returns nil tokens (the whole document). A non-empty pointer
// must start with '/'.
func parsePointerTokens(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if pointer[0] != '/' {
		return nil, fmt.Errorf("JSON Pointer must be empty or start with '/', got %q", pointer)
	}

	raw := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(raw))
	for i, r := range raw {
		decoded, err := decodePointerToken(r)
		if err != nil {
			return nil, err
		}
		tokens[i] = decoded
	}
	return tokens, nil
}

// decodePointerToken unescapes a single JSON Pointer token. It rejects
// malformed '~' escapes.
func decodePointerToken(token string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			sb.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) {
			return "", fmt.Errorf("invalid JSON Pointer escape at end of token %q", token)
		}
		switch token[i+1] {
		case '0':
			sb.WriteByte('~')
		case '1':
			sb.WriteByte('/')
		default:
			return "", fmt.Errorf("invalid JSON Pointer escape %q in token %q", "~"+string(token[i+1]), token)
		}
		i++
	}
	return sb.String(), nil
}
