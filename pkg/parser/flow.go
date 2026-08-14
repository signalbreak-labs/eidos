package parser

import (
	"errors"
	"fmt"
	"strings"
)

// yamlFlowParser parses a single-line YAML flow-style collection ({...} or
// [...]) that the JSON parser rejected because it uses YAML-only syntax such as
// unquoted keys ({ type: string }). It implements the subset of YAML 1.2 flow
// collections needed for OpenAPI specs: quoted or unquoted keys, YAML scalar
// inference for values, nested flow collections, and null values. Multi-line
// flow collections remain unsupported (see the package doc).
type yamlFlowParser struct {
	file  string
	text  string
	pos   int
	depth int
}

// parseYAMLFlow parses text as a single-line YAML flow collection. ok is true
// only when text is a well-formed flow collection that consumed the entire
// input; otherwise ok is false and callers fall back to plain-scalar handling
// (preserving the pre-existing lenient behavior for values that merely look
// like flow, e.g. a description of "{foo}"). ErrMaxNestingDepth is propagated
// so deeply-nested input cannot blow the stack.
func parseYAMLFlow(file, text string, depth int) (node Node, ok bool, err error) {
	p := &yamlFlowParser{file: file, text: text, depth: depth}
	p.skipWS()
	if p.pos >= len(p.text) {
		return nil, false, nil
	}
	loc := p.loc()
	switch p.text[p.pos] {
	case '{':
		node, err = p.parseMap(loc)
	case '[':
		node, err = p.parseSeq(loc)
	default:
		return nil, false, nil
	}
	if err != nil {
		if errors.Is(err, ErrMaxNestingDepth) {
			return nil, false, err
		}
		return nil, false, nil
	}
	p.skipWS()
	if p.pos != len(p.text) {
		return nil, false, nil
	}
	return node, true, nil
}

func (p *yamlFlowParser) enterComposite() error {
	if p.depth >= maxNestingDepth {
		return fmt.Errorf("%s:%d:%d: %w (%d)", p.file, 1, p.pos+1, ErrMaxNestingDepth, maxNestingDepth)
	}
	p.depth++
	return nil
}

func (p *yamlFlowParser) leaveComposite() { p.depth-- }

func (p *yamlFlowParser) skipWS() {
	for p.pos < len(p.text) && (p.text[p.pos] == ' ' || p.text[p.pos] == '\t') {
		p.pos++
	}
}

func (p *yamlFlowParser) loc() SourceLocation {
	return SourceLocation{File: p.file, Line: 1, Column: p.pos + 1}
}

func (p *yamlFlowParser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d:%d: %s", p.file, 1, p.pos+1, fmt.Sprintf(format, args...))
}

// parseMap parses a flow mapping starting at the '{' at p.pos.
func (p *yamlFlowParser) parseMap(loc SourceLocation) (*MapNode, error) {
	if err := p.enterComposite(); err != nil {
		return nil, err
	}
	defer p.leaveComposite()
	p.pos++ // consume '{'
	node := &MapNode{SourceLocation: loc}
	p.skipWS()
	if p.pos < len(p.text) && p.text[p.pos] == '}' {
		p.pos++
		return node, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unterminated flow mapping")
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.text) || p.text[p.pos] != ':' {
			return nil, p.errorf("expected ':' after flow mapping key")
		}
		p.pos++ // consume ':'
		p.skipWS()
		var value Node
		if p.pos < len(p.text) && (p.text[p.pos] == ',' || p.text[p.pos] == '}') {
			// A bare "key:" entry has a null value.
			value = &ScalarNode{Value: nil, Raw: "null", SourceLocation: p.loc()}
		} else {
			value, err = p.parseValue()
			if err != nil {
				return nil, err
			}
		}
		node.Entries = append(node.Entries, MapEntry{Key: key, Value: value})
		p.skipWS()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unterminated flow mapping")
		}
		c := p.text[p.pos]
		if c == '}' {
			p.pos++
			return node, nil
		}
		if c != ',' {
			return nil, p.errorf("expected ',' or '}' in flow mapping")
		}
		p.pos++ // consume ','
	}
}

// parseSeq parses a flow sequence starting at the '[' at p.pos.
func (p *yamlFlowParser) parseSeq(loc SourceLocation) (*SequenceNode, error) {
	if err := p.enterComposite(); err != nil {
		return nil, err
	}
	defer p.leaveComposite()
	p.pos++ // consume '['
	node := &SequenceNode{SourceLocation: loc}
	p.skipWS()
	if p.pos < len(p.text) && p.text[p.pos] == ']' {
		p.pos++
		return node, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unterminated flow sequence")
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.Items = append(node.Items, value)
		p.skipWS()
		if p.pos >= len(p.text) {
			return nil, p.errorf("unterminated flow sequence")
		}
		c := p.text[p.pos]
		if c == ']' {
			p.pos++
			return node, nil
		}
		if c != ',' {
			return nil, p.errorf("expected ',' or ']' in flow sequence")
		}
		p.pos++ // consume ','
	}
}

// parseValue parses a flow value: a nested collection, a quoted string, or a
// plain scalar.
func (p *yamlFlowParser) parseValue() (Node, error) {
	p.skipWS()
	if p.pos >= len(p.text) {
		return nil, p.errorf("unexpected end of flow collection")
	}
	loc := p.loc()
	switch p.text[p.pos] {
	case '{':
		return p.parseMap(loc)
	case '[':
		return p.parseSeq(loc)
	case '"':
		return p.parseQuoted(loc, '"')
	case '\'':
		return p.parseQuoted(loc, '\'')
	default:
		return p.parsePlain(loc, false)
	}
}

// parseKey parses a flow mapping key: a quoted string or a plain scalar that
// ends at the ':' separator (a colon not followed by whitespace is part of the
// key, e.g. "a:b").
func (p *yamlFlowParser) parseKey() (*ScalarNode, error) {
	p.skipWS()
	if p.pos >= len(p.text) {
		return nil, p.errorf("unexpected end of flow mapping")
	}
	loc := p.loc()
	switch p.text[p.pos] {
	case '"':
		return p.parseQuoted(loc, '"')
	case '\'':
		return p.parseQuoted(loc, '\'')
	default:
		return p.parsePlain(loc, true)
	}
}

// parseQuoted parses a single- or double-quoted string starting at p.pos.
func (p *yamlFlowParser) parseQuoted(loc SourceLocation, quote byte) (*ScalarNode, error) {
	start := p.pos
	p.pos++ // consume opening quote
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if quote == '\'' {
			if c == '\'' {
				if p.pos+1 < len(p.text) && p.text[p.pos+1] == '\'' {
					p.pos += 2 // escaped '' (YAML 1.2 §7.3.2)
					continue
				}
				p.pos++ // closing quote
				raw := p.text[start:p.pos]
				return &ScalarNode{Value: unescapeYAMLSingleQuoted(raw[1 : len(raw)-1]), Raw: raw, SourceLocation: loc}, nil
			}
			p.pos++
			continue
		}
		// Double-quoted: backslash escapes the next character.
		if c == '\\' {
			p.pos += 2
			continue
		}
		if c == '"' {
			p.pos++
			raw := p.text[start:p.pos]
			val, err := unescapeYAMLDoubleQuoted(raw[1 : len(raw)-1])
			if err != nil {
				return nil, p.errorf("invalid double-quoted string: %v", err)
			}
			return &ScalarNode{Value: val, Raw: raw, SourceLocation: loc}, nil
		}
		p.pos++
	}
	return nil, p.errorf("unterminated quoted string in flow collection")
}

// parsePlain parses an unquoted flow scalar. When isKey is true the token ends
// at a ':' followed by whitespace or end-of-input (the mapping separator);
// otherwise it ends at a flow indicator (',', '}', ']'). The token is trimmed
// and passed through YAML scalar inference so numbers, booleans, and null keep
// their types.
func (p *yamlFlowParser) parsePlain(loc SourceLocation, isKey bool) (*ScalarNode, error) {
	start := p.pos
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if c == ',' || c == '}' || c == ']' {
			break
		}
		if isKey && c == ':' {
			if p.pos+1 >= len(p.text) || p.text[p.pos+1] == ' ' || p.text[p.pos+1] == '\t' {
				break
			}
		}
		p.pos++
	}
	token := strings.TrimSpace(p.text[start:p.pos])
	if token == "" {
		return nil, p.errorf("empty flow scalar")
	}
	val, err := inferScalar(token)
	if err != nil {
		return nil, p.errorf("invalid flow scalar %q: %v", token, err)
	}
	return &ScalarNode{Value: val, Raw: token, SourceLocation: loc}, nil
}
