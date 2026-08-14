// Package parser provides a dedicated in-house OpenAPI parser for Eidos.
//
// The lexer in this file loads JSON and YAML documents into a small, generic
// AST (MapNode, SequenceNode, ScalarNode) while preserving the source line and
// column for every node. Column tracking is exact for JSON and best-effort for
// YAML because YAML is whitespace-significant and column inference depends on
// indentation and key text uniqueness. This raw AST is the input for the
// higher-level OpenAPI spec conversion code.
//
// Security note: the pipeline resolves only local (same-document) JSON Pointer
// $ref values (ref_local.go); external $ref values pointing at other files or
// URLs are rejected with an error diagnostic rather than fetched. There is
// therefore no network or filesystem $ref resolution to misuse for SSRF or
// path traversal, and no need to restrict schemes or sandbox file access
// (L-83: a previous external-ref resolver existed in the package but was never
// wired into the pipeline; it has been removed in favor of the local-only
// behavior the pipeline actually implements).
//
// Lexer limitations:
//   - YAML block keys containing an unquoted colon followed by whitespace are
//     rejected. Per YAML 1.2 §7.3.3, plain scalars may not contain a colon
//     followed by a space or end-of-line. Quoted keys (single or double) are
//     supported and can contain colons.
//   - YAML double-quoted scalars do interpret common escape sequences (\n, \t,
//     \\, \", \uXXXX). Single-quoted YAML scalars interpret the YAML 1.2 §7.3.2
//     doubled-quote escape: a pair of adjacent single quotes is collapsed to a
//     single '.
//   - YAML anchors and aliases (&name / *name) are supported for node values;
//     an anchor names the value node that follows it and later aliases resolve
//     to a copy of that node. YAML merge keys (<<) are not supported.
//   - YAML block scalars (| and >) and multi-line plain scalars are supported.
//     Block scalars honor the literal/folded styles, the chomping indicators
//     (+/-) and the explicit indentation indicator; multi-line plain scalars
//     fold single line breaks to spaces.
//   - YAML document start/end markers (--- and ...) are recognized and skipped.
//   - Inline flow collections ([...] and {...}) must appear on a single line;
//     multi-line flow collections are not supported.
//   - YAML scalars for the top-level `openapi:` and `swagger:` keys are
//     preserved as strings so that unquoted version values such as `3.0.0`
//     are not interpreted as numbers.
//   - A leading UTF-8 BOM is stripped from YAML input.
package parser

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// maxNestingDepth caps how many levels of objects/arrays (JSON) or nested
// blocks/flow collections (YAML) the lexer will recurse into. It prevents a
// malicious deeply-nested spec from exhausting the goroutine stack before the
// parser can return a recoverable error. The value is generous for real OpenAPI
// specs while staying well below Go's default stack limit.
const maxNestingDepth = 1000

// ErrMaxNestingDepth is returned when a document's object/array/block nesting
// exceeds maxNestingDepth. Callers can use errors.Is to detect it.
var ErrMaxNestingDepth = errors.New("document exceeds maximum nesting depth")

// isUTF16HighSurrogate reports whether code is a UTF-16 high (lead) surrogate.
// UTF-16 encodes code points above the BMP (U+10000..U+10FFFF) as a high
// surrogate followed by a low surrogate; the two must be combined into a
// single rune rather than emitted as two separate replacement characters (M-34).
func isUTF16HighSurrogate(code uint32) bool { return code >= 0xD800 && code <= 0xDBFF }

// isUTF16LowSurrogate reports whether code is a UTF-16 low (trail) surrogate.
func isUTF16LowSurrogate(code uint32) bool { return code >= 0xDC00 && code <= 0xDFFF }

// combineUTF16Surrogates combines a high and low UTF-16 surrogate pair into the
// supplementary code point they encode. It does not validate the pair; callers
// must first confirm high/low via isUTF16HighSurrogate/isUTF16LowSurrogate.
func combineUTF16Surrogates(high, low uint32) rune {
	// Validated surrogate pairs always land in [U+10000, U+10FFFF], which fits
	// in a rune (int32). The int->rune cast is therefore provably in range.
	return rune(0x10000 + (int(high)-0xD800)*0x400 + (int(low) - 0xDC00)) //nolint:gosec // G115: bounded to <= 0x10FFFF after surrogate validation
}

// parseHex4 parses exactly four hexadecimal characters as a uint32. It returns
// false if any character is not a hex digit or the slice is not four bytes long.
func parseHex4(s string) (uint32, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var n uint32
	for i := 0; i < 4; i++ {
		c := s[i]
		var v uint32
		switch {
		case c >= '0' && c <= '9':
			v = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = uint32(c-'A') + 10
		default:
			return 0, false
		}
		n = n*16 + v
	}
	return n, true
}

// parseHexN parses exactly n hexadecimal digits from s and returns the code
// point. It backs the \x (2 digits) and \U (8 digits) double-quoted escapes.
func parseHexN(s string, n int) (uint32, bool) {
	if len(s) != n {
		return 0, false
	}
	var code uint32
	for i := 0; i < n; i++ {
		c := s[i]
		var v uint32
		switch {
		case c >= '0' && c <= '9':
			v = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = uint32(c-'A') + 10
		default:
			return 0, false
		}
		code = code*16 + v
	}
	return code, true
}

// Node is the root type of the raw JSON/YAML AST produced by the lexer.
type Node interface {
	// GetSourceLocation returns where this node was found in the source file.
	GetSourceLocation() SourceLocation
}

// MapNode represents a JSON object or YAML mapping.
type MapNode struct {
	Entries []MapEntry
	SourceLocation
}

// MapEntry is a single key/value pair inside a MapNode.
type MapEntry struct {
	Key   *ScalarNode
	Value Node
}

// SequenceNode represents a JSON array or YAML sequence.
type SequenceNode struct {
	Items []Node
	SourceLocation
}

// ScalarNode represents a literal value such as a string, number, boolean, or null.
type ScalarNode struct {
	// Value is the parsed scalar value: string, float64, bool, or nil.
	Value any
	// Raw is the original source text before type inference.
	Raw string
	SourceLocation
}

// Ensure concrete node types satisfy the Node interface.
var (
	_ Node = (*MapNode)(nil)
	_ Node = (*SequenceNode)(nil)
	_ Node = (*ScalarNode)(nil)
)

// GetSourceLocation returns the source location of the map node.
func (m *MapNode) GetSourceLocation() SourceLocation { return m.SourceLocation }

// GetSourceLocation returns the source location of the sequence node.
func (s *SequenceNode) GetSourceLocation() SourceLocation { return s.SourceLocation }

// GetSourceLocation returns the source location of the scalar node.
func (s *ScalarNode) GetSourceLocation() SourceLocation { return s.SourceLocation }

// LoadFile parses a JSON or YAML document from data and returns a generic AST.
// The file extension is used to choose the parser; files ending in ".json" are
// parsed as JSON, everything else as YAML.
func LoadFile(file string, data []byte) (Node, error) {
	if strings.HasSuffix(strings.ToLower(file), ".json") {
		return loadJSON(file, data)
	}
	return loadYAML(file, data)
}

// LoadFileAsJSON parses data as a JSON document, attributing any errors to
// file. Unlike LoadFile it does not inspect the file extension, so callers that
// already know the format (from a Content-Type header or a first-byte sniff)
// can force the JSON parser regardless of the display name.
func LoadFileAsJSON(file string, data []byte) (Node, error) {
	return loadJSON(file, data)
}

// LoadFileAsYAML parses data as a YAML document, attributing any errors to
// file. Unlike LoadFile it does not inspect the file extension.
func LoadFileAsYAML(file string, data []byte) (Node, error) {
	return loadYAML(file, data)
}

// ---------- JSON loader ----------

type jsonParser struct {
	file  string
	data  []byte
	pos   int
	line  int
	col   int
	depth int
}

func loadJSON(file string, data []byte) (Node, error) {
	return loadJSONWithDepth(file, data, 0)
}

func loadJSONWithDepth(file string, data []byte, depth int) (Node, error) {
	p := &jsonParser{file: file, data: data, line: 1, col: 1, depth: depth}
	p.skipWhitespace()
	if p.pos >= len(p.data) {
		return nil, p.errorf("empty document")
	}
	node, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.pos != len(p.data) {
		return nil, p.errorf("unexpected trailing data")
	}
	return node, nil
}

func (p *jsonParser) parseValue() (Node, error) {
	p.skipWhitespace()
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of input")
	}
	loc := p.loc()
	c := p.data[p.pos]
	switch c {
	case '{':
		return p.parseObject(loc)
	case '[':
		return p.parseArray(loc)
	case '"':
		start := p.pos
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		// Raw is the original source text (including quotes/escapes), matching
		// the field doc and the JSON literal path; the prior code stored the
		// decoded string in Raw (L-78).
		return &ScalarNode{Value: s, Raw: string(p.data[start:p.pos]), SourceLocation: loc}, nil
	default:
		return p.parseLiteral()
	}
}

func (p *jsonParser) enterComposite() error {
	if p.depth >= maxNestingDepth {
		return fmt.Errorf("%s:%d:%d: %w (%d)", p.file, p.line, p.col, ErrMaxNestingDepth, maxNestingDepth)
	}
	p.depth++
	return nil
}

func (p *jsonParser) leaveComposite() {
	p.depth--
}

func (p *jsonParser) parseObject(loc SourceLocation) (*MapNode, error) {
	if err := p.enterComposite(); err != nil {
		return nil, err
	}
	defer p.leaveComposite()
	if err := p.consume('{'); err != nil {
		return nil, err
	}
	node := &MapNode{SourceLocation: loc}
	p.skipWhitespace()
	if p.peek() == '}' {
		p.advance()
		return node, nil
	}
	for {
		p.skipWhitespace()
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated object")
		}
		keyLoc := p.loc()
		if p.data[p.pos] != '"' {
			return nil, p.errorf("object key must be a string")
		}
		start := p.pos
		keyStr, err := p.parseString()
		if err != nil {
			return nil, err
		}
		// Raw is the original source text (including quotes), matching the
		// field doc and the JSON value-string path (L-78).
		keyNode := &ScalarNode{Value: keyStr, Raw: string(p.data[start:p.pos]), SourceLocation: keyLoc}
		p.skipWhitespace()
		if err := p.consume(':'); err != nil {
			return nil, err
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.Entries = append(node.Entries, MapEntry{Key: keyNode, Value: value})
		p.skipWhitespace()
		c := p.peek()
		if c == '}' {
			p.advance()
			return node, nil
		}
		if c != ',' {
			return nil, p.errorf("expected ',' or '}' in object")
		}
		p.advance()
	}
}

func (p *jsonParser) parseArray(loc SourceLocation) (*SequenceNode, error) {
	if err := p.enterComposite(); err != nil {
		return nil, err
	}
	defer p.leaveComposite()
	if err := p.consume('['); err != nil {
		return nil, err
	}
	node := &SequenceNode{SourceLocation: loc}
	p.skipWhitespace()
	if p.peek() == ']' {
		p.advance()
		return node, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.Items = append(node.Items, value)
		p.skipWhitespace()
		c := p.peek()
		if c == ']' {
			p.advance()
			return node, nil
		}
		if c != ',' {
			return nil, p.errorf("expected ',' or ']' in array")
		}
		p.advance()
	}
}

func (p *jsonParser) parseString() (string, error) {
	if err := p.consume('"'); err != nil {
		return "", err
	}
	var sb strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '"' {
			p.advance()
			return sb.String(), nil
		}
		if c == '\\' {
			p.advance()
			if p.pos >= len(p.data) {
				return "", p.errorf("unterminated string escape")
			}
			ec := p.data[p.pos]
			switch ec {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '/':
				sb.WriteByte('/')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				consumed, err := p.appendUnicodeEscape(&sb)
				if err != nil {
					return "", err
				}
				p.pos += consumed
				p.col += consumed
			default:
				return "", p.errorf("invalid escape sequence")
			}
			p.advance()
			continue
		}
		if c < 0x20 {
			return "", p.errorf("invalid control character in string")
		}
		sb.WriteByte(c)
		p.advance()
	}
	return "", p.errorf("unterminated string")
}

// appendUnicodeEscape handles a `\u` JSON escape sequence starting at p.data[p.pos]
// (which points at the 'u'). It writes the decoded rune(s) to sb and returns the
// number of hex digits consumed beyond the leading 'u' (4 for a single BMP code
// point, 10 for a combined surrogate pair). The shared caller-owned p.advance()
// then moves past the final hex digit.
func (p *jsonParser) appendUnicodeEscape(sb *strings.Builder) (int, error) {
	if p.pos+4 >= len(p.data) {
		return 0, p.errorf("invalid unicode escape")
	}
	code, ok := parseHex4(string(p.data[p.pos+1 : p.pos+5]))
	if !ok {
		return 0, p.errorf("invalid unicode escape")
	}
	if code > 0x10FFFF {
		return 0, p.errorf("invalid unicode escape")
	}
	consumed := 4
	switch {
	case isUTF16HighSurrogate(code):
		// A high surrogate must be followed by a \uXXXX low surrogate; combine
		// them into one supplementary code point instead of emitting two
		// replacement characters (M-34).
		if p.pos+11 <= len(p.data) && p.data[p.pos+5] == '\\' && p.data[p.pos+6] == 'u' {
			if low, lowOK := parseHex4(string(p.data[p.pos+7 : p.pos+11])); lowOK && isUTF16LowSurrogate(low) {
				sb.WriteRune(combineUTF16Surrogates(code, low))
				return 10, nil
			}
		}
		// Lone high surrogate: replace with U+FFFD rather than emitting an
		// invalid lone-surrogate rune.
		sb.WriteRune('�')
	case isUTF16LowSurrogate(code):
		// Lone low surrogate with no preceding high surrogate.
		sb.WriteRune('�')
	default:
		sb.WriteRune(rune(code))
	}
	return consumed, nil
}

func (p *jsonParser) parseLiteral() (*ScalarNode, error) {
	loc := p.loc()
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == ']' || c == '}' {
			break
		}
		p.advance()
	}
	raw := string(p.data[start:p.pos])
	if raw == "" {
		return nil, p.errorf("expected literal value")
	}
	val, err := inferScalar(raw)
	if err != nil {
		return nil, p.errorf("invalid literal %q: %v", raw, err)
	}
	// JSON literals are restricted to true, false, null, and numbers (RFC
	// 8259). inferScalar returns any unrecognized token as a string, so without
	// this check malformed input such as {"a": hello}, 01, +1, or 1.2.3 would
	// be silently accepted as valid JSON (M-33).
	switch raw {
	case "true", "false", "null":
		// Valid JSON keyword literals.
	default:
		if !isValidJSONNumber(raw) {
			return nil, p.errorf("invalid JSON literal %q", raw)
		}
	}
	return &ScalarNode{Value: val, Raw: raw, SourceLocation: loc}, nil
}

// isValidJSONNumber reports whether s is a number matching the RFC 8259 grammar:
// an optional leading minus, an integer part (0 or a non-zero digit followed by
// digits), an optional fraction, and an optional exponent. It rejects Go- but
// not JSON-valid forms such as +1, 01, .5, 1., and 1.2.3 (M-33).
func isValidJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	// Integer part: "0" alone, or a non-zero digit followed by any digits.
	if i >= len(s) {
		return false
	}
	switch {
	case s[i] == '0':
		i++
	case s[i] >= '1' && s[i] <= '9':
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	default:
		return false
	}
	// Optional fraction: '.' followed by at least one digit.
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	// Optional exponent: [eE] with optional sign and at least one digit.
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return i == len(s)
}

func (p *jsonParser) peek() byte {
	if p.pos >= len(p.data) {
		return 0
	}
	return p.data[p.pos]
}

func (p *jsonParser) consume(want byte) error {
	if p.pos >= len(p.data) || p.data[p.pos] != want {
		return p.errorf("expected %q", want)
	}
	p.advance()
	return nil
}

func (p *jsonParser) advance() {
	if p.pos >= len(p.data) {
		return
	}
	p.pos++
	p.col++
}

func (p *jsonParser) skipWhitespace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t':
			p.pos++
			p.col++
		case '\n':
			p.pos++
			p.line++
			p.col = 1
		case '\r':
			p.pos++
			p.col = 1
		default:
			return
		}
	}
}

func (p *jsonParser) loc() SourceLocation {
	return SourceLocation{File: p.file, Line: p.line, Column: p.col}
}

func (p *jsonParser) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d:%d: "+format, append([]any{p.file, p.line, p.col}, args...)...)
}

// ---------- YAML loader ----------

type yamlLine struct {
	lineNo int
	indent int
	column int    // 1-based column of the first non-whitespace character
	raw    string // original line text without newline
}

type yamlParser struct {
	file  string
	lines []yamlLine
	depth int
	// anchors maps a YAML anchor name to the node it names. Anchors are
	// recorded as they are parsed; aliases resolve to a deep copy of the
	// anchored node so the two occurrences never share mutable state.
	anchors map[string]Node
}

func loadYAML(file string, data []byte) (Node, error) {
	p := &yamlParser{file: file, anchors: make(map[string]Node)}
	p.splitLines(data)
	// Skip a leading document-start marker (---) and any blanks before the
	// first content line. Only one document is parsed; a multi-document stream
	// is rejected below (M-35).
	i := p.skipBlankAndMarkers(0)
	if i >= len(p.lines) {
		return nil, fmt.Errorf("%s: empty YAML document", file)
	}
	node, next, err := p.parseBlock(i, -1)
	if err != nil {
		return nil, err
	}
	// Allow trailing blank/comment/document-end markers. If a second document
	// follows (another --- with content), skipBlankAndMarkers skips the marker
	// but leaves the content, so the check fails and the stream is rejected
	// rather than silently merged (M-35).
	if p.skipBlankAndMarkers(next) != len(p.lines) {
		return nil, p.errorAt(next, "unexpected extra content after document")
	}
	return node, nil
}

func (p *yamlParser) enterBlock() error {
	if p.depth >= maxNestingDepth {
		return fmt.Errorf("%s: %w (%d)", p.file, ErrMaxNestingDepth, maxNestingDepth)
	}
	p.depth++
	return nil
}

func (p *yamlParser) leaveBlock() {
	p.depth--
}

func (p *yamlParser) splitLines(data []byte) {
	text := string(data)
	// Strip a leading UTF-8 BOM if present.
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	// Normalize line endings.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	for lineNo, raw := range strings.Split(text, "\n") {
		indent := 0
		for indent < len(raw) && (raw[indent] == ' ' || raw[indent] == '\t') {
			indent++
		}
		p.lines = append(p.lines, yamlLine{lineNo: lineNo + 1, indent: indent, column: indent + 1, raw: raw})
	}
}

// isDocMarkerLine reports whether line is a YAML document start (---) or end
// (...) marker. Such markers terminate the current document rather than being
// skipped as blank: a multi-document stream must not have document 2's keys
// silently merged into document 1's root map (M-35).
func isDocMarkerLine(line yamlLine) bool {
	trimmed := strings.TrimSpace(line.raw)
	return trimmed == "---" || trimmed == "..."
}

func (p *yamlParser) skipBlank(i int) int {
	for i < len(p.lines) {
		line := p.lines[i]
		trimmed := strings.TrimSpace(line.raw)
		// Blank and comment lines are skipped, but document markers (--- and
		// ...) are NOT: they terminate the current document, so the block
		// parsers must see them and stop (M-35).
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		break
	}
	return i
}

// skipBlankAndMarkers skips blank, comment, and document-marker lines. It is
// used only to skip leading markers before a document and trailing markers
// after a document; within a document, markers terminate parsing (handled by
// the block parsers, not by skipBlank).
func (p *yamlParser) skipBlankAndMarkers(i int) int {
	for i < len(p.lines) {
		line := p.lines[i]
		trimmed := strings.TrimSpace(line.raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || isDocMarkerLine(line) {
			i++
			continue
		}
		break
	}
	return i
}

// parseBlock parses a contiguous block of lines at the given base indentation.
// A negative base indent is used only for the root block.
func (p *yamlParser) parseBlock(i, baseIndent int) (Node, int, error) {
	i = p.skipBlank(i)
	if i >= len(p.lines) {
		return nil, i, nil
	}

	first := p.lines[i]
	if baseIndent >= 0 && first.indent < baseIndent {
		return nil, i, nil
	}
	if baseIndent >= 0 && first.indent > baseIndent {
		return nil, i, p.errorAt(i, "wrong indentation")
	}
	// A document marker terminates the block (M-35).
	if isDocMarkerLine(first) {
		return nil, i, nil
	}

	// At the document root (baseIndent == -1, only loadYAML passes this) the
	// indentation checks above are skipped, which would let a misindented stray
	// key be silently absorbed into the root map. Pin the root block to the
	// first content line's indent so the sub-parser enforces consistent
	// indentation (L-77).
	if baseIndent < 0 {
		baseIndent = first.indent
	}

	// Decide the block type based on the first content line.
	trimmed := strings.TrimSpace(first.raw[first.indent:])
	if strings.HasPrefix(trimmed, "-") && (len(trimmed) == 1 || trimmed[1] == ' ' || trimmed[1] == '\t') {
		return p.parseSequence(i, baseIndent)
	}
	if strings.Contains(trimmed, ":") {
		return p.parseMapping(i, baseIndent)
	}
	return p.parseScalarBlock(i, baseIndent)
}

func (p *yamlParser) parseMapping(i, baseIndent int) (*MapNode, int, error) {
	node := &MapNode{SourceLocation: p.locAt(i)}

	for i < len(p.lines) {
		i = p.skipBlank(i)
		if i >= len(p.lines) {
			break
		}
		line := p.lines[i]
		if baseIndent >= 0 && line.indent < baseIndent {
			break
		}
		if baseIndent >= 0 && line.indent > baseIndent {
			return nil, i, p.errorAt(i, "wrong indentation in mapping")
		}
		// A document marker terminates the mapping (the root mapping has no
		// indentation guard, so without this a following document's keys would
		// be absorbed into this one) (M-35).
		if isDocMarkerLine(line) {
			break
		}

		text := line.raw[line.indent:]
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		// An explicit-key line ("? key") has no colon, so it must be handled
		// before the mapping-separator check below (M-36).
		if isExplicitKeyIndicator(trimmed) {
			keyNode, value, next, err := p.parseExplicitKey(i, baseIndent)
			if err != nil {
				return nil, next, err
			}
			i = next
			node.Entries = append(node.Entries, MapEntry{Key: keyNode, Value: value})
			continue
		}
		if !strings.Contains(text, ":") {
			break
		}

		key, rawValue, colonIdx, ok := splitMapping(text)
		if !ok {
			return nil, i, p.errorAt(i, "invalid mapping entry")
		}
		if !isValidYAMLKey(key) {
			return nil, i, p.errorAt(i, "unquoted key contains invalid character")
		}
		key, keyErr := unquoteYAMLKey(key)
		if keyErr != nil {
			return nil, i, p.errorAt(i, keyErr.Error())
		}
		keyLoc := p.locAt(i)
		keyNode := &ScalarNode{Value: key, Raw: key, SourceLocation: keyLoc}

		value, next, err := p.parseMappingValue(i, baseIndent, key, keyLoc, rawValue, colonIdx, line)
		if err != nil {
			return nil, next, err
		}
		i = next

		node.Entries = append(node.Entries, MapEntry{Key: keyNode, Value: value})
	}

	return node, i, nil
}

// parseExplicitKey parses a YAML explicit-key mapping entry written as "? key"
// on one line and ": value" on the next (YAML 1.2 §7.1.3.1). The value after
// the colon is parsed as the value of the key: usually a mapping entry (e.g.
// "get:" or "type: object"), but it may be any YAML value. It returns the key
// node, the value node, and the index of the first line not consumed.
func (p *yamlParser) parseExplicitKey(i, baseIndent int) (*ScalarNode, Node, int, error) {
	line := p.lines[i]
	text := line.raw[line.indent:]
	keyText := strings.TrimSpace(text[1:]) // strip the "?" indicator
	key, keyErr := unquoteYAMLKey(keyText)
	if keyErr != nil {
		return nil, nil, i, p.errorAt(i, keyErr.Error())
	}
	keyLoc := p.locAt(i)
	keyNode := &ScalarNode{Value: key, Raw: key, SourceLocation: keyLoc}

	// The value follows on the next line, introduced by ": " at the same
	// indentation as the key.
	j := p.skipBlank(i + 1)
	if j >= len(p.lines) {
		return nil, nil, i, p.errorAt(i, "explicit key has no value")
	}
	valLine := p.lines[j]
	if valLine.indent != line.indent {
		return nil, nil, i, p.errorAt(j, "explicit key value must be at the same indentation as the key")
	}
	valText := valLine.raw[valLine.indent:]
	if !strings.HasPrefix(valText, ":") {
		return nil, nil, i, p.errorAt(j, "explicit key value must start with ':'")
	}
	afterColon := skipAfterColon(valText, 0)
	valueCol := valLine.indent + 1 + afterColon
	valueLoc := SourceLocation{File: p.file, Line: valLine.lineNo, Column: valueCol}
	rawValue := strings.TrimSpace(valText[afterColon:])

	value, next, err := p.parseExplicitKeyValue(j, baseIndent, valueCol, valueLoc, rawValue)
	if err != nil {
		return nil, nil, i, err
	}
	return keyNode, value, next, nil
}

// parseExplicitKeyValue parses the value that follows the ": " line of an
// explicit-key entry. The value text begins at valueCol on line i. When the
// value is a mapping entry (e.g. "get:" or "type: object"), it is parsed as a
// mapping whose entries sit at valueCol; otherwise it is parsed as a general
// YAML value.
func (p *yamlParser) parseExplicitKeyValue(i, baseIndent, valueCol int, valueLoc SourceLocation, rawValue string) (Node, int, error) {
	valueKeyLoc := SourceLocation{File: p.file, Line: valueLoc.Line, Column: valueCol}
	// The value mapping's entries sit at 0-based indent valueCol-1 (valueCol is
	// the 1-based column of the first entry), so that is the base indentation
	// used for the entry's block value and for sibling-entry merging.
	entryIndent := valueCol - 1
	itemMap, next, err := p.parseSingleMapping(rawValue, i, entryIndent, valueKeyLoc)
	if err != nil {
		// Not a mapping entry — parse as a general value. The value sits at the
		// same indentation as the key, so continuation lines are more indented
		// than the parent block's baseIndent.
		return p.parseValueAfterColon(i, baseIndent, rawValue, valueLoc)
	}
	i = next
	if itemMap.Entries[0].Value == nil {
		i, err = p.fillSequenceMappingFirstValue(i, entryIndent, itemMap, valueKeyLoc)
		if err != nil {
			return nil, i, err
		}
	}
	// Merge additional sibling entries that sit at the same column as the first
	// entry (e.g. "type: object" followed by "properties:", or "get:" followed
	// by "put:").
	j := p.skipBlank(i)
	if j < len(p.lines) && p.lines[j].indent == entryIndent && isMappingLine(p.lines[j].raw[p.lines[j].indent:]) {
		if err := p.enterBlock(); err != nil {
			return nil, i, err
		}
		extra, next, err := p.parseBlock(j, entryIndent)
		p.leaveBlock()
		if err != nil {
			return nil, i, err
		}
		if extraMap, ok := extra.(*MapNode); ok {
			itemMap.Entries = append(itemMap.Entries, extraMap.Entries...)
		}
		i = next
	}
	return itemMap, i, nil
}

func (p *yamlParser) parseMappingValue(i, baseIndent int, key string, keyLoc SourceLocation, rawValue string, colonIdx int, line yamlLine) (Node, int, error) {
	// The version-scalar special case applies only to the top-level `openapi:`
	// / `swagger:` keys (p.depth == 0 is the root mapping). Gating on depth
	// prevents a nested schema property literally named "openapi" or "swagger"
	// from having its value string-coerced, matching the package doc's
	// "top-level" claim (L-75: previously the special case fired at any depth).
	isVersionKey := (key == "openapi" || key == "swagger") && p.depth == 0

	if rawValue == "" {
		return p.parseMappingBlockValue(i, baseIndent, keyLoc)
	}

	text := line.raw[line.indent:]
	afterColon := skipAfterColon(text, colonIdx)
	valueCol := keyLoc.Column + afterColon
	valueLoc := SourceLocation{File: p.file, Line: line.lineNo, Column: valueCol}
	if isVersionKey {
		value := p.parseVersionScalar(rawValue, valueLoc)
		return value, i + 1, nil
	}
	value, next, err := p.parseValueAfterColon(i, baseIndent, rawValue, valueLoc)
	if err != nil {
		return nil, i, err
	}
	return value, next, nil
}

func (p *yamlParser) parseMappingBlockValue(i, baseIndent int, keyLoc SourceLocation) (Node, int, error) {
	i++
	i = p.skipBlank(i)
	if i >= len(p.lines) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: keyLoc}, i, nil
	}
	next := p.lines[i]
	// A block sequence may sit at the same indentation as its parent key
	// ("key:\n- item"), so a sequence item at baseIndent is still the key's
	// value rather than a sibling key.
	if next.indent < baseIndent || (next.indent == baseIndent && !isSequenceItemLine(next)) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: keyLoc}, i, nil
	}
	// A document marker after a key with no inline value means the key has a
	// null value; do not absorb the marker into the key's block (M-35).
	if isDocMarkerLine(next) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: keyLoc}, i, nil
	}
	if err := p.enterBlock(); err != nil {
		return nil, i, err
	}
	value, nextIdx, err := p.parseBlock(i, next.indent)
	p.leaveBlock()
	if err != nil {
		return nil, nextIdx, err
	}
	setNodeLoc(value, keyLoc)
	return value, nextIdx, nil
}

func (p *yamlParser) parseSequence(i, baseIndent int) (*SequenceNode, int, error) {
	itemLoc := p.locAt(i)
	node := &SequenceNode{SourceLocation: itemLoc}

	for i < len(p.lines) {
		i = p.skipBlank(i)
		if i >= len(p.lines) {
			break
		}
		line := p.lines[i]
		if baseIndent >= 0 && line.indent < baseIndent {
			break
		}
		if baseIndent >= 0 && line.indent > baseIndent {
			return nil, i, p.errorAt(i, "wrong indentation in sequence")
		}
		// A document marker terminates the sequence (M-35).
		if isDocMarkerLine(line) {
			break
		}

		text := line.raw[line.indent:]
		if !strings.HasPrefix(text, "-") {
			break
		}
		itemText := strings.TrimPrefix(text, "-")
		if itemText != "" && itemText[0] != ' ' && itemText[0] != '\t' {
			break
		}
		content := itemText
		itemText = strings.TrimSpace(itemText)
		itemLoc = p.locAt(i)
		// Account for the '-' marker plus any whitespace before the actual value.
		trimmedLeft := strings.TrimLeft(content, " \t")
		valueCol := itemLoc.Column + 1 + (len(content) - len(trimmedLeft))

		value, next, err := p.parseSequenceItemValue(i, baseIndent, itemText, itemLoc, valueCol)
		if err != nil {
			return nil, next, err
		}
		i = next

		node.Items = append(node.Items, value)
	}

	return node, i, nil
}

func (p *yamlParser) parseSequenceItemValue(i, baseIndent int, itemText string, itemLoc SourceLocation, valueCol int) (Node, int, error) {
	if itemText == "" {
		return p.parseSequenceBlockItem(i, baseIndent, itemLoc)
	}
	if isMappingLine(itemText) {
		return p.parseSequenceMappingItem(i, baseIndent, itemText, itemLoc, valueCol)
	}
	if isSequenceItemIndicator(itemText) {
		// A compact nested sequence: this item's value is itself a sequence
		// begun on the same line ("- - a"). The nested sequence's indentation
		// is the column before the second '-'.
		return p.parseNestedSequence(i, valueCol-1, itemText, itemLoc, valueCol)
	}
	valueLoc := SourceLocation{File: p.file, Line: itemLoc.Line, Column: valueCol}
	value, next, err := p.parseValueAfterColon(i, baseIndent, itemText, valueLoc)
	if err != nil {
		return nil, i, err
	}
	return value, next, nil
}

// isSequenceItemIndicator reports whether text begins a sequence item marker
// ("-" alone or "-" followed by whitespace).
func isSequenceItemIndicator(text string) bool {
	if text == "" || text[0] != '-' {
		return false
	}
	return len(text) == 1 || text[1] == ' ' || text[1] == '\t'
}

// isSequenceItemLine reports whether a line begins a sequence item marker after
// its indentation. It is used to recognize a block sequence that sits at the
// same indentation as its parent key ("key:\n- item"), which YAML treats as the
// key's value rather than a sibling key.
func isSequenceItemLine(line yamlLine) bool {
	trimmed := strings.TrimSpace(line.raw[line.indent:])
	return isSequenceItemIndicator(trimmed)
}

// isExplicitKeyIndicator reports whether text begins a YAML explicit-key
// indicator ("?" alone or "?" followed by whitespace). Explicit keys are written
// as "? key" on one line with the value introduced by ": " on the next
// (YAML 1.2 §7.1.3.1). A "?" that is not followed by whitespace is part of a
// plain scalar key (e.g. "?foo: bar") and is not an indicator.
func isExplicitKeyIndicator(text string) bool {
	if text == "" || text[0] != '?' {
		return false
	}
	return len(text) == 1 || text[1] == ' ' || text[1] == '\t'
}

// parseNestedSequence parses a compact nested sequence, where an item's value is
// itself a sequence begun on the same line (e.g. "- - a"). The first inner item
// shares the outer item's line after the second '-'; subsequent inner items
// continue on following lines at exactly baseIndent (the inner sequence's
// indentation). firstDashCol is the 1-based column of the second '-'.
func (p *yamlParser) parseNestedSequence(i, baseIndent int, firstText string, firstLoc SourceLocation, firstDashCol int) (*SequenceNode, int, error) {
	node := &SequenceNode{SourceLocation: firstLoc}

	// The first inner item begins on the current line, after the second '-'.
	innerText := firstText[1:]
	innerItemText := strings.TrimSpace(innerText)
	innerLeading := len(innerText) - len(innerItemText)
	innerValueCol := firstDashCol + 1 + innerLeading
	value, next, err := p.parseSequenceItemValue(i, baseIndent, innerItemText, firstLoc, innerValueCol)
	if err != nil {
		return nil, i, err
	}
	node.Items = append(node.Items, value)
	i = next

	// Remaining inner items follow on their own lines at exactly baseIndent.
	for i < len(p.lines) {
		i = p.skipBlank(i)
		if i >= len(p.lines) {
			break
		}
		line := p.lines[i]
		if baseIndent >= 0 && line.indent < baseIndent {
			break
		}
		if baseIndent >= 0 && line.indent > baseIndent {
			return nil, i, p.errorAt(i, "wrong indentation in sequence")
		}
		if isDocMarkerLine(line) {
			break
		}
		text := line.raw[line.indent:]
		if !strings.HasPrefix(text, "-") {
			break
		}
		itemText := strings.TrimPrefix(text, "-")
		if itemText != "" && itemText[0] != ' ' && itemText[0] != '\t' {
			break
		}
		content := itemText
		itemText = strings.TrimSpace(itemText)
		itemLoc := p.locAt(i)
		trimmedLeft := strings.TrimLeft(content, " \t")
		valueCol := itemLoc.Column + 1 + (len(content) - len(trimmedLeft))
		value, next, err := p.parseSequenceItemValue(i, baseIndent, itemText, itemLoc, valueCol)
		if err != nil {
			return nil, next, err
		}
		i = next
		node.Items = append(node.Items, value)
	}
	return node, i, nil
}

func (p *yamlParser) parseSequenceBlockItem(i, baseIndent int, itemLoc SourceLocation) (Node, int, error) {
	i++
	i = p.skipBlank(i)
	if i >= len(p.lines) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: itemLoc}, i, nil
	}
	next := p.lines[i]
	if next.indent <= baseIndent {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: itemLoc}, i, nil
	}
	// A document marker after a sequence item with no inline value means the
	// item has a null value; do not absorb the marker (M-35).
	if isDocMarkerLine(next) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: itemLoc}, i, nil
	}
	if err := p.enterBlock(); err != nil {
		return nil, i, err
	}
	value, nextIdx, err := p.parseBlock(i, next.indent)
	p.leaveBlock()
	if err != nil {
		return nil, nextIdx, err
	}
	setNodeLoc(value, itemLoc)
	return value, nextIdx, nil
}

func (p *yamlParser) parseSequenceMappingItem(i, baseIndent int, itemText string, itemLoc SourceLocation, valueCol int) (*MapNode, int, error) {
	keyLoc := SourceLocation{File: p.file, Line: itemLoc.Line, Column: valueCol}
	itemMap, next, err := p.parseSingleMapping(itemText, i, baseIndent, keyLoc)
	if err != nil {
		return nil, i, err
	}
	i = next
	if itemMap.Entries[0].Value == nil {
		i, err = p.fillSequenceMappingFirstValue(i, baseIndent, itemMap, itemLoc)
		if err != nil {
			return nil, i, err
		}
	}
	i, err = p.mergeSequenceMappingEntries(i, baseIndent, itemMap)
	if err != nil {
		return nil, i, err
	}
	return itemMap, i, nil
}

func (p *yamlParser) fillSequenceMappingFirstValue(i, baseIndent int, itemMap *MapNode, itemLoc SourceLocation) (int, error) {
	i = p.skipBlank(i)
	if i < len(p.lines) && p.lines[i].indent > baseIndent {
		if err := p.enterBlock(); err != nil {
			return i, err
		}
		val, next, err := p.parseBlock(i, p.lines[i].indent)
		p.leaveBlock()
		if err != nil {
			return i, err
		}
		setNodeLoc(val, itemLoc)
		itemMap.Entries[0].Value = val
		return next, nil
	}
	itemMap.Entries[0].Value = &ScalarNode{Value: nil, Raw: "null", SourceLocation: itemLoc}
	return i, nil
}

func (p *yamlParser) mergeSequenceMappingEntries(i, baseIndent int, itemMap *MapNode) (int, error) {
	for {
		j := p.skipBlank(i)
		if j >= len(p.lines) || p.lines[j].indent <= baseIndent || !isMappingLine(p.lines[j].raw[p.lines[j].indent:]) {
			break
		}
		// Account for the recursion the same way fillSequenceMappingFirstValue
		// does: parseBlock can recurse arbitrarily deep, so it must be wrapped
		// in enterBlock/leaveBlock or the maxNestingDepth guard is bypassed
		// (L-79).
		if err := p.enterBlock(); err != nil {
			return i, err
		}
		extra, next, err := p.parseBlock(j, p.lines[j].indent)
		p.leaveBlock()
		if err != nil {
			return i, err
		}
		if extraMap, ok := extra.(*MapNode); ok {
			itemMap.Entries = append(itemMap.Entries, extraMap.Entries...)
		}
		i = next
	}
	return i, nil
}

// setNodeLoc overrides the source location of a single node while preserving
// its file name. It does not recurse into children.
func setNodeLoc(n Node, loc SourceLocation) {
	if n == nil {
		return
	}
	file := loc.File
	switch x := n.(type) {
	case *MapNode:
		x.SourceLocation = SourceLocation{File: file, Line: loc.Line, Column: loc.Column}
	case *SequenceNode:
		x.SourceLocation = SourceLocation{File: file, Line: loc.Line, Column: loc.Column}
	case *ScalarNode:
		x.SourceLocation = SourceLocation{File: file, Line: loc.Line, Column: loc.Column}
	}
}

// shiftNodeLineAndColumn replaces the source line of a node and all of its
// descendants with baseLoc.Line and shifts each column by baseLoc.Column-1 so
// that relative column offsets from an inner parse (e.g. loadJSON for a YAML
// inline flow collection) are preserved.
func shiftNodeLineAndColumn(n Node, baseLoc SourceLocation) error {
	if n == nil {
		return nil
	}
	if err := shiftSingleNodeLoc(n, baseLoc); err != nil {
		return err
	}
	switch x := n.(type) {
	case *MapNode:
		for _, e := range x.Entries {
			if err := shiftNodeLineAndColumn(e.Key, baseLoc); err != nil {
				return err
			}
			if err := shiftNodeLineAndColumn(e.Value, baseLoc); err != nil {
				return err
			}
		}
	case *SequenceNode:
		for _, item := range x.Items {
			if err := shiftNodeLineAndColumn(item, baseLoc); err != nil {
				return err
			}
		}
	}
	return nil
}

func shiftSingleNodeLoc(n Node, baseLoc SourceLocation) error {
	switch x := n.(type) {
	case *MapNode:
		x.SourceLocation = SourceLocation{File: baseLoc.File, Line: baseLoc.Line, Column: x.Column + baseLoc.Column - 1}
	case *SequenceNode:
		x.SourceLocation = SourceLocation{File: baseLoc.File, Line: baseLoc.Line, Column: x.Column + baseLoc.Column - 1}
	case *ScalarNode:
		x.SourceLocation = SourceLocation{File: baseLoc.File, Line: baseLoc.Line, Column: x.Column + baseLoc.Column - 1}
	default:
		return fmt.Errorf("unsupported node type %T for column shifting", n)
	}
	return nil
}

// isMappingLine reports whether text is a YAML mapping entry (contains an
// unquoted colon followed by a space or end-of-string).
func isMappingLine(text string) bool {
	_, _, _, ok := splitMapping(text)
	return ok
}

// parseSingleMapping parses a single inline mapping entry such as "k: v" into a
// MapNode. The value is left nil when the entry ends with a bare colon.
// keyLoc is the location of the first character of the key in the source line.
// It returns the index of the first line not consumed by the entry so that a
// block scalar or multi-line plain scalar value can advance the caller past its
// content lines.
func (p *yamlParser) parseSingleMapping(text string, i, baseIndent int, keyLoc SourceLocation) (*MapNode, int, error) {
	key, rawValue, colonIdx, ok := splitMapping(text)
	if !ok {
		return nil, i, fmt.Errorf("%s:%d:%d: not a mapping entry", p.file, keyLoc.Line, keyLoc.Column)
	}
	// Strip surrounding quotes from the key. Without this, a quoted inline key
	// such as `"$ref": "..."` — common in OpenAPI specs for $ref-only sequence
	// items like `- "$ref": "#/components/parameters/gist-id"` — keeps its quote
	// characters in the ScalarNode value, so downstream key comparisons ("$ref"
	// vs $ref) never match and the reference is silently dropped. parseMapping
	// and parseExplicitKey already unquote; parseSingleMapping must too, since it
	// parses the first mapping entry of a sequence item and of an explicit-key
	// value (L-101).
	key, keyErr := unquoteYAMLKey(key)
	if keyErr != nil {
		return nil, i, p.errorAt(i, keyErr.Error())
	}
	key = strings.TrimSpace(key)
	keyNode := &ScalarNode{Value: key, Raw: key, SourceLocation: keyLoc}
	itemMap := &MapNode{SourceLocation: keyLoc, Entries: []MapEntry{{Key: keyNode}}}
	if rawValue == "" {
		return itemMap, i + 1, nil
	}
	// Compute the exact 1-based column where the value text begins.
	afterColon := skipAfterColon(text, colonIdx)
	valueCol := keyLoc.Column + afterColon
	valueLoc := SourceLocation{File: p.file, Line: keyLoc.Line, Column: valueCol}
	// Version-scalar special case, top-level only — see parseMappingValue (L-75).
	if (key == "openapi" || key == "swagger") && p.depth == 0 {
		itemMap.Entries[0].Value = p.parseVersionScalar(rawValue, valueLoc)
		return itemMap, i + 1, nil
	}
	value, next, err := p.parseValueAfterColon(i, baseIndent, rawValue, valueLoc)
	if err != nil {
		return nil, i, err
	}
	itemMap.Entries[0].Value = value
	return itemMap, next, nil
}

// skipAfterColon returns the 0-based offset in text of the first non-space,
// non-tab character after the colon that sits at colonIdx. It centralizes the
// whitespace-skipping logic used when computing the exact source column of a
// YAML mapping value.
func skipAfterColon(text string, colonIdx int) int {
	afterColon := colonIdx + 1
	for afterColon < len(text) && (text[afterColon] == ' ' || text[afterColon] == '\t') {
		afterColon++
	}
	return afterColon
}

func (p *yamlParser) parseScalarBlock(i, baseIndent int) (*ScalarNode, int, error) {
	loc := p.locAt(i)
	var parts []string
	for i < len(p.lines) {
		i = p.skipBlank(i)
		if i >= len(p.lines) {
			break
		}
		line := p.lines[i]
		if baseIndent >= 0 && line.indent < baseIndent {
			break
		}
		if baseIndent >= 0 && line.indent > baseIndent {
			return nil, i, p.errorAt(i, "wrong indentation in scalar block")
		}
		// A document marker terminates the scalar block (M-35).
		if isDocMarkerLine(line) {
			break
		}
		parts = append(parts, strings.TrimSpace(line.raw[line.indent:]))
		i++
	}
	raw := strings.Join(parts, "\n")
	val, err := inferScalar(raw)
	if err != nil {
		return nil, i, err
	}
	return &ScalarNode{Value: val, Raw: raw, SourceLocation: loc}, i, nil
}

// parseVersionScalar parses the value of an `openapi:` or `swagger:` key without
// applying YAML type inference, so that unquoted values such as `3.0.0` remain
// strings rather than becoming floats. Surrounding YAML quotes are stripped
// so that `openapi: "3.0.0"` produces the same string value as `openapi: 3.0.0`.
func (p *yamlParser) parseVersionScalar(valueText string, loc SourceLocation) *ScalarNode {
	text := stripYAMLComment(valueText)
	trimmed := strings.TrimSpace(text)
	val := trimmed
	if len(trimmed) >= 2 && trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'' {
		val = unescapeYAMLSingleQuoted(trimmed[1 : len(trimmed)-1])
	} else if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		if unq, err := unescapeYAMLDoubleQuoted(trimmed[1 : len(trimmed)-1]); err == nil {
			val = unq
		}
	}
	return &ScalarNode{Value: val, Raw: val, SourceLocation: loc}
}

func (p *yamlParser) parseInlineValue(valueText string, loc SourceLocation) (Node, error) {
	text := stripYAMLComment(valueText)
	trimmed := strings.TrimSpace(text)

	// Block scalar indicators are only valid as the value of a mapping key with
	// following indented lines. If we see them inline without a nested block,
	// treat them as plain strings.
	if trimmed == "" {
		return &ScalarNode{Value: "", Raw: "", SourceLocation: loc}, nil
	}

	// Try inline flow collections first. YAML flow style is close enough to JSON
	// for the common OpenAPI cases when keys are quoted. Continue the current
	// nesting depth so a deeply-nested inline collection cannot blow the stack.
	if (trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']') ||
		(trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') {
		node, err := loadJSONWithDepth(p.file, []byte(trimmed), p.depth)
		if err == nil {
			// Preserve the original YAML source line and the relative column
			// offsets produced by the JSON parser.
			if err := shiftNodeLineAndColumn(node, loc); err != nil {
				return nil, err
			}
			return node, nil
		}
		if errors.Is(err, ErrMaxNestingDepth) {
			return nil, err
		}
		// JSON rejected the flow value (e.g. unquoted keys like { type: string });
		// fall back to the YAML flow parser before treating it as a plain scalar.
		if node, ok, ferr := parseYAMLFlow(p.file, trimmed, p.depth); ok {
			if ferr != nil {
				return nil, ferr
			}
			if err := shiftNodeLineAndColumn(node, loc); err != nil {
				return nil, err
			}
			return node, nil
		}
		// Otherwise fall back to scalar parsing for flow-style values that are
		// not valid JSON or YAML flow (e.g. a description of "{foo}").
	}

	val, err := inferScalar(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%s:%d:%d: invalid scalar %q: %w", p.file, loc.Line, loc.Column, trimmed, err)
	}
	return &ScalarNode{Value: val, Raw: trimmed, SourceLocation: loc}, nil
}

// parseValueAfterColon parses the value that follows a mapping key or a
// sequence item marker on line i (at the parent block's baseIndent). It
// handles block scalars (| and >), inline values, and multi-line plain scalars,
// returning the value node together with the index of the first line that is
// not part of the value.
func (p *yamlParser) parseValueAfterColon(i, baseIndent int, rawValue string, valueLoc SourceLocation) (Node, int, error) {
	if style, chomping, indent, ok := parseBlockScalarHeader(rawValue); ok {
		node, next := p.parseBlockScalarContent(i, baseIndent, style, chomping, indent, valueLoc)
		return node, next, nil
	}
	trimmedValue := strings.TrimSpace(stripYAMLComment(rawValue))
	if anchor, rest, ok := splitAnchorIndicator(trimmedValue); ok {
		return p.parseAnchoredValue(i, baseIndent, anchor, rest, valueLoc)
	}
	if alias, ok := splitAliasIndicator(trimmedValue); ok {
		return p.resolveAlias(i, alias, valueLoc)
	}
	if len(trimmedValue) >= 1 && (trimmedValue[0] == '"' || trimmedValue[0] == '\'') && !isCompleteQuotedScalar(trimmedValue) {
		// A quoted scalar that is not terminated on this line may continue across
		// following lines — at any indentation — until the closing quote appears
		// (YAML multi-line quoted scalars, §7.3.2).
		return p.parseQuotedScalarContinuation(i, trimmedValue, valueLoc)
	}
	value, err := p.parseInlineValue(rawValue, valueLoc)
	if err != nil {
		return nil, i, err
	}
	// A plain (unquoted, non-flow) scalar value continues across following lines
	// that are more indented than the parent block, folding single line breaks
	// to spaces (YAML multi-line plain scalars, §7.3.3).
	s, isScalar := value.(*ScalarNode)
	if isScalar && !isQuotedOrFlow(rawValue) {
		if strVal, isStr := s.Value.(string); isStr {
			next, folded := p.foldPlainScalarContinuation(i+1, baseIndent, strVal)
			if next > i+1 {
				s.Value = folded
				s.Raw = folded
				return s, next, nil
			}
		}
	}
	return value, i + 1, nil
}

// isQuotedOrFlow reports whether a YAML scalar value is written in a form that
// cannot be continued across lines: a single/double quoted string or an inline
// flow collection ([...] / {...}).
func isQuotedOrFlow(text string) bool {
	trimmed := strings.TrimSpace(stripYAMLComment(text))
	if trimmed == "" {
		return true
	}
	switch trimmed[0] {
	case '\'', '"', '[', '{':
		return true
	}
	return false
}

// parseBlockScalarHeader parses a YAML block scalar header such as `|`, `>-`,
// `|2`, `|-`, or `> # comment`. It returns the style ('|' for literal, '>' for
// folded), the chomping indicator (0 for clip, '+' for keep, '-' for strip),
// and the explicit content indentation (0 when it is auto-detected). ok is
// false when text is not a valid block scalar header, in which case callers
// treat the value as a plain scalar.
func parseBlockScalarHeader(text string) (style, chomping byte, indent int, ok bool) {
	trimmed := strings.TrimSpace(stripYAMLComment(text))
	if trimmed == "" || (trimmed[0] != '|' && trimmed[0] != '>') {
		return 0, 0, 0, false
	}
	style = trimmed[0]
	rest := trimmed[1:]
	chompingSet := false
	indentSet := false
	for rest != "" {
		c := rest[0]
		switch {
		case c == '+' || c == '-':
			if chompingSet {
				return 0, 0, 0, false
			}
			chomping = c
			chompingSet = true
			rest = rest[1:]
		case c >= '0' && c <= '9':
			// YAML allows a single indentation indicator digit; reject runs like
			// "|12" instead of guessing.
			if indentSet {
				return 0, 0, 0, false
			}
			indent = int(c - '0')
			indentSet = true
			rest = rest[1:]
		default:
			return 0, 0, 0, false
		}
	}
	return style, chomping, indent, true
}

// parseBlockScalarContent consumes the content lines of a block scalar whose
// header is on line i at the given base indentation. It applies the literal (|)
// or folded (>) line semantics and the header's chomping indicator, and returns
// the scalar node together with the index of the first line after the block.
func (p *yamlParser) parseBlockScalarContent(i, baseIndent int, style, chomping byte, explicitIndent int, loc SourceLocation) (*ScalarNode, int) {
	contentIndent := 0
	if explicitIndent > 0 {
		contentIndent = baseIndent + explicitIndent
	}

	k := i + 1
	var lines []string

	if contentIndent == 0 {
		// Auto-detect the content indentation from the first non-blank line that
		// is more indented than the parent block. Document markers (--- / ...)
		// are not special inside a block scalar: they are literal content when
		// more indented than the parent and only terminate the document when
		// they sit at the parent's own indentation (handled by the indent
		// checks here and by the enclosing block parser).
		j := k
		for j < len(p.lines) {
			l := p.lines[j]
			if strings.TrimSpace(l.raw) == "" {
				j++
				continue
			}
			if l.indent <= baseIndent {
				break
			}
			contentIndent = l.indent
			break
		}
		if contentIndent == 0 {
			// No content lines: the block scalar is empty.
			val := blockScalarValue(style, chomping, nil)
			return &ScalarNode{Value: val, Raw: val, SourceLocation: loc}, i + 1
		}
	}

	for k < len(p.lines) {
		l := p.lines[k]
		if strings.TrimSpace(l.raw) == "" {
			// Blank lines belong to the block regardless of their indentation;
			// trailing ones are removed by the chomping indicator.
			lines = append(lines, "")
			k++
			continue
		}
		if l.indent < contentIndent {
			break
		}
		raw := l.raw
		if len(raw) >= contentIndent {
			raw = raw[contentIndent:]
		}
		lines = append(lines, raw)
		k++
	}

	val := blockScalarValue(style, chomping, lines)
	return &ScalarNode{Value: val, Raw: val, SourceLocation: loc}, k
}

// blockScalarValue assembles the final scalar value from the content lines of a
// literal (|) or folded (>) block scalar, applying the YAML folding and
// chomping rules. Content lines already have the content indentation removed;
// blank lines are represented by empty strings.
func blockScalarValue(style, chomping byte, lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var body string
	if style == '>' {
		body = foldBlockLines(lines)
	} else {
		body = strings.Join(lines, "\n")
	}
	// The physical text ends every content line (including the last) with a line
	// break; chomping then decides how many trailing breaks survive.
	physical := body + "\n"
	switch chomping {
	case '-':
		return strings.TrimRight(physical, "\n")
	case '+':
		return physical
	default: // clip: exactly one trailing line break whenever there is content.
		return strings.TrimRight(physical, "\n") + "\n"
	}
}

// foldBlockLines applies the folding rule for folded (>) block scalars: a line
// break between two adjacent non-empty lines that are not more indented than
// the block becomes a single space; breaks adjacent to empty lines or to
// more-indented lines are preserved as line breaks.
func foldBlockLines(lines []string) string {
	var sb strings.Builder
	moreIndented := false
	blankPending := false
	for _, line := range lines {
		if line == "" {
			// An empty line forces the surrounding breaks to be preserved so the
			// next non-empty line starts on a fresh line.
			blankPending = sb.Len() > 0
			continue
		}
		leadingSpace := line[0] == ' ' || line[0] == '\t'
		if sb.Len() > 0 {
			switch {
			case blankPending:
				sb.WriteByte('\n')
			case moreIndented || leadingSpace:
				sb.WriteByte('\n')
			default:
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(line)
		moreIndented = leadingSpace
		blankPending = false
	}
	if blankPending {
		sb.WriteByte('\n')
	}
	return sb.String()
}

// foldPlainScalarContinuation extends an inline plain scalar with following
// lines that are more indented than the parent block, folding single line
// breaks to spaces and blank-line-separated paragraphs to newlines (YAML
// multi-line plain scalars, §7.3.3). It returns the folded value and the index
// of the first line that is not part of the scalar. A following line that
// starts a nested mapping or sequence is not a continuation, so such input
// still fails the caller's indentation check instead of being silently
// absorbed.
// isCompleteQuotedScalar reports whether text is a quoted scalar ('…' or "…")
// that is fully terminated on a single line. Unterminated quoted scalars are
// handled by parseQuotedScalarContinuation, which extends them across following
// more-indented lines per YAML's multi-line quoted scalar rule.
func isCompleteQuotedScalar(text string) bool {
	if len(text) < 2 {
		return false
	}
	quote := text[0]
	if quote != '"' && quote != '\'' {
		return false
	}
	if text[len(text)-1] != quote {
		return false
	}
	if quote == '"' {
		_, err := unescapeYAMLDoubleQuoted(text[1 : len(text)-1])
		return err == nil
	}
	return true
}

// quotedScalarRaw assembles the continuation parts collected so far and reports
// whether the quoted string is terminated, returning the raw scalar text
// (surrounding quotes included) up to and including the closing quote together
// with whatever follows the closing quote on the terminating line (so the caller
// can reject trailing content that libyaml would treat as a structural error).
// Parts are the trimmed content of each line; a trailing escape that is itself
// unterminated (e.g. a lone backslash) keeps the string open.
func quotedScalarRaw(parts []string, quote byte) (raw, trailing string, ok bool) {
	text := strings.Join(parts, "\n")
	if text == "" || text[0] != quote {
		return "", "", false
	}
	i := 1
	for i < len(text) {
		c := text[i]
		if quote == '"' {
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' {
				return text[:i+1], text[i+1:], true
			}
		} else if c == '\'' {
			if i+1 < len(text) && text[i+1] == '\'' {
				i += 2 // escaped quote (''); keep scanning
				continue
			}
			return text[:i+1], text[i+1:], true
		}
		i++
	}
	return "", "", false
}

// endsWithEscapeBackslash reports whether s ends with an odd number of
// backslashes, i.e. whether its final backslash is itself an escape rather than
// an escaped backslash. Only a trailing escape backslash triggers YAML's
// backslash line-continuation in a double-quoted scalar.
func endsWithEscapeBackslash(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// foldQuotedInterior folds line breaks inside a multi-line quoted scalar: a
// single line break between two non-blank lines becomes a space; breaks
// adjacent to blank lines become newlines (paragraph separation). For
// double-quoted scalars a backslash at the end of a line escapes the following
// line break (YAML line continuation): the backslash and break are dropped
// entirely, no space is inserted.
func foldQuotedInterior(s string, doubleQuoted bool) string {
	lines := strings.Split(s, "\n")
	var sb strings.Builder
	blankPending := false
	cont := false // previous line ended with an escape backslash
	for _, line := range lines {
		if line == "" {
			if sb.Len() > 0 {
				blankPending = true
			}
			cont = false
			continue
		}
		// Drop the trailing escape backslash that continued the previous line.
		if cont && sb.Len() > 0 {
			out := sb.String()
			sb.Reset()
			sb.WriteString(out[:len(out)-1])
		}
		if sb.Len() > 0 && !cont {
			if blankPending {
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(line)
		blankPending = false
		cont = doubleQuoted && endsWithEscapeBackslash(line)
	}
	if blankPending {
		sb.WriteByte('\n')
	}
	return sb.String()
}

// decodeQuotedScalar strips the surrounding quotes of a quoted scalar, folds
// its interior line breaks, and unescapes the content per the quote style.
func decodeQuotedScalar(raw string, quote byte) (string, error) {
	if len(raw) < 2 || raw[0] != quote || raw[len(raw)-1] != quote {
		return "", fmt.Errorf("malformed quoted scalar")
	}
	folded := foldQuotedInterior(raw[1:len(raw)-1], quote == '"')
	if quote == '"' {
		return unescapeYAMLDoubleQuoted(folded)
	}
	return unescapeYAMLSingleQuoted(folded), nil
}

// parseQuotedScalarContinuation extends an unterminated quoted scalar across
// the following more-indented lines. Line content is folded into the scalar
// until the closing quote is found; folding single breaks to spaces and breaks
// adjacent to blank lines to newlines.
func (p *yamlParser) parseQuotedScalarContinuation(i int, first string, loc SourceLocation) (Node, int, error) {
	quote := first[0]
	quoteName := map[byte]string{'"': "double", '\'': "single"}[quote]
	parts := []string{first}
	k := i + 1 // the scalar's own line is already in parts; scan continuation lines
	nextRaw := ""
	terminated := false
	for !terminated {
		if k >= len(p.lines) || isDocMarkerLine(p.lines[k]) {
			return nil, k, p.errorAt(i, fmt.Sprintf("unterminated %s-quoted string (missing closing %c)", quoteName, quote))
		}
		line := p.lines[k]
		trimmed := strings.TrimSpace(line.raw[line.indent:])
		if trimmed == "" {
			// A blank line inside a multi-line quoted scalar is content, not a
			// terminator: it contributes an empty line to the folded value (M-37).
			parts = append(parts, "")
			k++
			continue
		}
		// Continuation lines of a quoted scalar fold into the string at any
		// indentation — real specs dedent them below the key (the GigaVUE-FM
		// openapi.fm.yaml aligns them with the enclosing sequence item) and both
		// PyYAML and libyaml accept that, matching YAML 1.2 §7.3.2. Only the
		// closing quote ends the string, so indentation is not a structural
		// signal here; a string that never closes fails loud at EOF below.
		parts = append(parts, trimmed)
		if raw, trailing, ok := quotedScalarRaw(parts, quote); ok {
			// Anything after the closing quote on the terminating line must be a
			// comment or whitespace. libyaml rejects trailing content (e.g. a
			// sibling key whose quote prematurely closed the string) rather than
			// silently dropping it; fail loud the same way.
			if rest := strings.TrimSpace(stripYAMLComment(trailing)); rest != "" {
				return nil, k, p.errorAt(k, fmt.Sprintf("unexpected content %q after closing %s quote", rest, quoteName))
			}
			nextRaw = raw
			terminated = true
		}
		k++
	}
	val, err := decodeQuotedScalar(nextRaw, quote)
	if err != nil {
		return nil, k, p.errorAt(i, fmt.Sprintf("invalid %s-quoted scalar: %v", quoteName, err))
	}
	return &ScalarNode{Value: val, Raw: nextRaw, SourceLocation: loc}, k, nil
}

// isIndicatorNameChar reports whether c may appear in a YAML anchor or alias
// name. Flow indicators and whitespace terminate the name.
func isIndicatorNameChar(c byte) bool {
	switch c {
	case ' ', '\t', '[', ']', '{', '}', ',', '#':
		return false
	}
	return true
}

// splitIndicatorName splits the leading anchor/alias name off s (the text after
// the '&' or '*' indicator). The name runs to the first whitespace or flow
// indicator; rest holds the remainder (including any leading separator).
func splitIndicatorName(s string) (name, rest string, ok bool) {
	i := 0
	for i < len(s) && isIndicatorNameChar(s[i]) {
		i++
	}
	if i == 0 {
		return "", "", false
	}
	return s[:i], s[i:], true
}

// splitAnchorIndicator parses a YAML anchor indicator ("&name") at the start of
// a value. ok is false when text does not begin with an anchor. rest is the
// remainder of the value after the anchor (empty for the common "key: &name"
// block-anchor form).
func splitAnchorIndicator(text string) (name, rest string, ok bool) {
	if len(text) < 2 || text[0] != '&' {
		return "", "", false
	}
	return splitIndicatorName(text[1:])
}

// splitAliasIndicator parses a YAML alias indicator ("*name") at the start of a
// value. ok is false when text does not begin with an alias.
func splitAliasIndicator(text string) (name string, ok bool) {
	if len(text) < 2 || text[0] != '*' {
		return "", false
	}
	name, rest, ok := splitIndicatorName(text[1:])
	if ok && strings.TrimSpace(rest) != "" {
		// An alias must be the entire value; trailing content means this is not
		// an alias reference (e.g. a plain scalar beginning with '*').
		return "", false
	}
	return name, ok
}

// parseAnchoredValue parses the value named by a YAML anchor: either the block
// following the "&name" on its own lines ("key: &name") or an inline value on
// the same line ("key: &name value"). The parsed node is recorded under the
// anchor name so later aliases can resolve to it.
func (p *yamlParser) parseAnchoredValue(i, baseIndent int, anchor, rest string, loc SourceLocation) (Node, int, error) {
	var value Node
	var next int
	var err error
	if strings.TrimSpace(rest) == "" {
		value, next, err = p.parseMappingBlockValue(i, baseIndent, loc)
	} else {
		value, next, err = p.parseValueAfterColon(i, baseIndent, strings.TrimSpace(rest), loc)
	}
	if err != nil {
		return nil, next, err
	}
	p.anchors[anchor] = value
	return value, next, nil
}

// resolveAlias returns a deep copy of the node named by a YAML alias ("*name").
// The copy's location is set to the alias use site so diagnostics point at the
// reference rather than the anchor definition. An unresolvable alias fails loud
// rather than silently producing a null value. The alias occupies its line, so
// the returned index advances past it (i+1) to keep block loops from spinning.
func (p *yamlParser) resolveAlias(i int, name string, loc SourceLocation) (Node, int, error) {
	ref, ok := p.anchors[name]
	if !ok {
		return nil, i, p.errorAt(i, fmt.Sprintf("unknown YAML alias *%s (no matching &%s anchor)", name, name))
	}
	clone := cloneNode(ref)
	setNodeLoc(clone, loc)
	return clone, i + 1, nil
}

// cloneNode returns a deep copy of a parsed node tree so an aliased node never
// shares mutable state with its anchor definition.
func cloneNode(n Node) Node {
	switch t := n.(type) {
	case *ScalarNode:
		c := *t
		return &c
	case *SequenceNode:
		c := &SequenceNode{SourceLocation: t.SourceLocation}
		for _, item := range t.Items {
			c.Items = append(c.Items, cloneNode(item))
		}
		return c
	case *MapNode:
		c := &MapNode{SourceLocation: t.SourceLocation}
		for _, e := range t.Entries {
			// Keys are always scalar nodes, so copy the key by value rather than
			// routing it back through cloneNode and the Node interface.
			key := *e.Key
			c.Entries = append(c.Entries, MapEntry{Key: &key, Value: cloneNode(e.Value)})
		}
		return c
	default:
		return n
	}
}

func (p *yamlParser) foldPlainScalarContinuation(i, baseIndent int, first string) (int, string) {
	var sb strings.Builder
	sb.WriteString(first)
	k := i
	blankPending := false
	for k < len(p.lines) {
		line := p.lines[k]
		trimmed := strings.TrimSpace(line.raw)
		if isDocMarkerLine(line) {
			break
		}
		if trimmed == "" {
			blankPending = sb.Len() > 0
			k++
			continue
		}
		if strings.HasPrefix(trimmed, "#") || line.indent <= baseIndent {
			break
		}
		// A more-indented continuation line is part of the plain scalar, even
		// when it begins with "- ": once an inline plain scalar value has
		// started, the key's value is a scalar and cannot have block children, so
		// a "-" at the start of a continuation line is literal content, not a
		// block sequence indicator (YAML 1.2 §7.3.3; indicators are only honored
		// at a node start, and we are mid-scalar). yaml.v3/PyYAML agree, folding
		// such lines into the scalar. A line that itself looks like a mapping
		// entry ("k: v") does end the scalar: that is a structural indicator and
		// is rejected by reference parsers as "mapping values are not allowed in
		// this context". isMappingLine (not a bare strings.Contains ":") is used
		// so that colons inside URLs or other text ("https://…") do not end the
		// scalar: only a colon followed by whitespace or end-of-line is a mapping
		// separator.
		if isMappingLine(trimmed) {
			break
		}
		if blankPending {
			sb.WriteByte('\n')
			blankPending = false
		} else {
			sb.WriteByte(' ')
		}
		sb.WriteString(strings.TrimSpace(line.raw[line.indent:]))
		k++
	}
	return k, sb.String()
}

func (p *yamlParser) locAt(i int) SourceLocation {
	if i >= len(p.lines) {
		return SourceLocation{File: p.file}
	}
	line := p.lines[i]
	return SourceLocation{File: p.file, Line: line.lineNo, Column: line.column}
}

func (p *yamlParser) errorAt(i int, msg string) error {
	loc := p.locAt(i)
	if loc.Line < 1 {
		return fmt.Errorf("%s: %s", p.file, msg)
	}
	return fmt.Errorf("%s:%d:%d: %s", p.file, loc.Line, loc.Column, msg)
}

// isValidYAMLKey reports whether a parsed key is a valid block mapping key.
// Quoted keys may contain any character. Unquoted keys may not contain a colon
// followed by whitespace or end-of-line per YAML 1.2 §7.3.3.
func isValidYAMLKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) < 2 {
		return trimmed != ""
	}
	// Quoted keys are accepted as-is; the quotes will be stripped later.
	if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') ||
		(trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
		return true
	}
	// For unquoted keys, reject any colon at all. Since splitMapping already
	// removed the separator, any remaining colon came from inside the key.
	return !strings.Contains(trimmed, ":")
}

// unquoteYAMLKey removes surrounding quotes from a quoted YAML key and rejects
// malformed quoting. It returns the key unchanged for unquoted keys.
func unquoteYAMLKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) >= 2 {
		if trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
			return unescapeYAMLDoubleQuoted(trimmed[1 : len(trimmed)-1])
		}
		if trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'' {
			return unescapeYAMLSingleQuoted(trimmed[1 : len(trimmed)-1]), nil
		}
	}
	return trimmed, nil
}

// splitMapping splits "key: value" at the first unquoted colon that is a valid
// YAML mapping separator. Per YAML 1.2 §7.3.3, a plain scalar key may not contain
// a colon followed by a space or end-of-line. Therefore an unquoted colon is
// only accepted as the separator when the key part before the colon is quoted
// or when the colon is followed by whitespace or end-of-string. Colons inside
// quoted strings are ignored.
//
// The returned colonIdx is the 0-based index of the separator colon in text,
// which callers use to compute the exact source column of the value.
// atScalarStart reports whether the quote at position i begins a scalar token
// (start of the string or after whitespace) rather than appearing mid-token,
// where it is a literal character (e.g. the apostrophe in "Let's"). Quotes only
// delimit a scalar when they begin one.
func atScalarStart(text string, i int) bool {
	return i == 0 || text[i-1] == ' ' || text[i-1] == '\t'
}

func splitMapping(text string) (key, value string, colonIdx int, ok bool) {
	inDouble := false
	inSingle := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case '"':
			if inDouble {
				inDouble = false
			} else if !inSingle && atScalarStart(text, i) {
				inDouble = true
			}
		case '\'':
			if inSingle {
				inSingle = false
			} else if !inDouble && atScalarStart(text, i) {
				inSingle = true
			}
		case ':':
			if inDouble || inSingle {
				continue
			}
			// An unquoted colon is only a separator when followed by whitespace or
			// end-of-string. This matches YAML 1.2 §7.3.3 for plain scalar keys.
			if i+1 < len(text) && text[i+1] != ' ' && text[i+1] != '\t' {
				continue
			}
			keyPart := strings.TrimSpace(text[:i])
			return keyPart, strings.TrimSpace(text[i+1:]), i, true
		}
	}
	return "", "", 0, false
}

func stripYAMLComment(text string) string {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			// Only a quote at a scalar start opens a quoted section; a quote
			// mid-token is a literal character (e.g. "Let's").
			if !atScalarStart(text, i) {
				continue
			}
			i++
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' && i+1 < len(text) {
					i += 2
				} else {
					i++
				}
			}
		case '\'':
			if !atScalarStart(text, i) {
				continue
			}
			i++
			for i < len(text) && text[i] != '\'' {
				i++
			}
		case '#':
			// YAML 1.2 requires a separation space before a comment start: a
			// '#' that follows a non-space character is part of the scalar, not
			// a comment. Without this check, "Use C#" parses as "Use C" and a
			// URL fragment such as "https://x.com/a#frag" is silently dropped
			// (H-16). A '#' at the very start of the value is a comment.
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' {
				return text[:i]
			}
		}
	}
	return text
}

// inferScalar converts a raw scalar token into a typed value.
// Supported types: null, bool, number (float64), string.
func inferScalar(raw string) (any, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s, nil
	}

	// Quoted strings are kept as strings.
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return unescapeYAMLSingleQuoted(s[1 : len(s)-1]), nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeYAMLDoubleQuoted(s[1 : len(s)-1])
	}

	// A leading quote without a matching trailing quote is malformed.
	if s[0] == '\'' {
		return nil, fmt.Errorf("unterminated single-quoted string")
	}
	if s[0] == '"' {
		return nil, fmt.Errorf("unterminated double-quoted string")
	}

	lower := strings.ToLower(s)
	switch lower {
	case "null", "~":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}

	// Integers and floats are parsed as float64 so the AST is JSON-compatible.
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		// strconv.ParseFloat accepts "NaN", "Inf", "+Inf", "-Inf" (and
		// case-insensitive variants). JSON cannot represent NaN or Inf, so
		// storing such a float64 in the AST would make a later json.Marshal fail
		// with a confusing "unsupported value" error far from the source. Treat
		// these tokens as strings instead so the AST stays JSON-serializable
		// (L-76).
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return s, nil
		}
		return n, nil
	}

	return s, nil
}

// unescapeYAMLSingleQuoted interprets the doubled-quote escape used inside YAML
// single-quoted strings (YAML 1.2 §7.3.2). Each occurrence of two adjacent single
// quotes is collapsed to a single quote. The operation cannot fail, so it
// returns only a string; the prior error return was a dead branch every caller
// discarded (L-78).
func unescapeYAMLSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "''", "'")
}

// unescapeYAMLDoubleQuoted interprets the escape sequences used inside YAML
// double-quoted strings. It supports the common subset needed for OpenAPI files.
func unescapeYAMLDoubleQuoted(s string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("invalid escape at end of quoted string")
		}
		c := s[i+1]
		if c == 'x' || c == 'u' || c == 'U' {
			next, err := appendNumericEscape(&sb, s, c, i)
			if err != nil {
				return "", err
			}
			i = next
			continue
		}
		switch c {
		case '0':
			sb.WriteByte(0)
		case 'a':
			sb.WriteByte(0x07)
		case 'b':
			sb.WriteByte(0x08)
		case 't':
			sb.WriteByte('\t')
		case '\t':
			sb.WriteByte('\t')
		case 'n':
			sb.WriteByte('\n')
		case 'v':
			sb.WriteByte(0x0B)
		case 'f':
			sb.WriteByte(0x0C)
		case 'r':
			sb.WriteByte('\r')
		case 'e':
			sb.WriteByte(0x1B)
		case ' ':
			sb.WriteByte(' ')
		case '"':
			sb.WriteByte('"')
		case '/':
			sb.WriteByte('/')
		case '\\':
			sb.WriteByte('\\')
		case 'N':
			sb.WriteRune('\u0085') // NEL, next line
		case '_':
			sb.WriteRune('\u00A0') // non-breaking space
		case 'L':
			sb.WriteRune('\u2028') // line separator
		case 'P':
			sb.WriteRune('\u2029') // paragraph separator
		default:
			return "", fmt.Errorf("invalid escape sequence %q", "\\"+string(c))
		}
		i += 2
	}
	return sb.String(), nil
}

// appendNumericEscape decodes a \xXX, \uXXXX, or \UXXXXXXXX escape starting at
// the backslash s[i] and appends the decoded rune(s) to sb. kind is 'x', 'u',
// or 'U'. It returns the index of the first byte after the escape and any
// error. Hex parsing and surrogate handling live here to keep
// unescapeYAMLDoubleQuoted under the gocognit threshold.
func appendNumericEscape(sb *strings.Builder, s string, kind byte, i int) (int, error) {
	digits := 0
	switch kind {
	case 'x':
		digits = 2
	case 'u':
		digits = 4
	case 'U':
		digits = 8
	}
	// s[i] is '\\', s[i+1] is kind, hex digits start at i+2.
	if i+2+digits > len(s) {
		return 0, fmt.Errorf("invalid \\%c escape", kind)
	}
	code, ok := parseHexN(s[i+2:i+2+digits], digits)
	if !ok {
		return 0, fmt.Errorf("invalid \\%c escape", kind)
	}
	if code > 0x10FFFF {
		return 0, fmt.Errorf("invalid \\%c escape", kind)
	}
	if kind == 'u' && isUTF16HighSurrogate(code) {
		// Combine a following \uXXXX low surrogate into one supplementary code
		// point instead of two replacement chars (M-34).
		if i+12 <= len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
			if low, lowOK := parseHex4(s[i+8 : i+12]); lowOK && isUTF16LowSurrogate(low) {
				sb.WriteRune(combineUTF16Surrogates(code, low))
				return i + 12, nil
			}
		}
		sb.WriteRune('\uFFFD')
		return i + 2 + digits, nil
	}
	if kind == 'u' && isUTF16LowSurrogate(code) {
		sb.WriteRune('\uFFFD')
		return i + 2 + digits, nil
	}
	// code was validated as <= 0x10FFFF above and is not a surrogate, so it is
	// always a valid rune; the bound check keeps this conversion safe (G115).
	sb.WriteRune(rune(code))
	return i + 2 + digits, nil
}
