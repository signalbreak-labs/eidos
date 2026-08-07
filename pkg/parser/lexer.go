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
//   - YAML anchors/aliases (& and *), block scalars (| and >), and merge keys
//     (<<) are not supported.
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
}

func loadYAML(file string, data []byte) (Node, error) {
	p := &yamlParser{file: file}
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
	value, err := p.parseInlineValue(rawValue, valueLoc)
	if err != nil {
		return nil, i, err
	}
	return value, i + 1, nil
}

func (p *yamlParser) parseMappingBlockValue(i, baseIndent int, keyLoc SourceLocation) (Node, int, error) {
	i++
	i = p.skipBlank(i)
	if i >= len(p.lines) {
		return &ScalarNode{Value: nil, Raw: "null", SourceLocation: keyLoc}, i, nil
	}
	next := p.lines[i]
	if next.indent <= baseIndent {
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
	valueLoc := SourceLocation{File: p.file, Line: itemLoc.Line, Column: valueCol}
	value, err := p.parseInlineValue(itemText, valueLoc)
	if err != nil {
		return nil, i, err
	}
	return value, i + 1, nil
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
	itemMap, err := p.parseSingleMapping(itemText, keyLoc)
	if err != nil {
		return nil, i, err
	}
	i++
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
func (p *yamlParser) parseSingleMapping(text string, keyLoc SourceLocation) (*MapNode, error) {
	key, rawValue, colonIdx, ok := splitMapping(text)
	if !ok {
		return nil, fmt.Errorf("%s:%d:%d: not a mapping entry", p.file, keyLoc.Line, keyLoc.Column)
	}
	key = strings.TrimSpace(key)
	keyNode := &ScalarNode{Value: key, Raw: key, SourceLocation: keyLoc}
	itemMap := &MapNode{SourceLocation: keyLoc, Entries: []MapEntry{{Key: keyNode}}}
	if rawValue == "" {
		return itemMap, nil
	}
	// Compute the exact 1-based column where the value text begins.
	afterColon := skipAfterColon(text, colonIdx)
	valueCol := keyLoc.Column + afterColon
	var value Node
	var err error
	// Version-scalar special case, top-level only — see parseMappingValue (L-75).
	if (key == "openapi" || key == "swagger") && p.depth == 0 {
		value = p.parseVersionScalar(rawValue, SourceLocation{File: p.file, Line: keyLoc.Line, Column: valueCol})
	} else {
		value, err = p.parseInlineValue(rawValue, SourceLocation{File: p.file, Line: keyLoc.Line, Column: valueCol})
		if err != nil {
			return nil, err
		}
	}
	itemMap.Entries[0].Value = value
	return itemMap, nil
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
		// Otherwise fall back to scalar parsing for flow-style values that are
		// not valid JSON (e.g. unquoted keys).
	}

	val, err := inferScalar(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%s:%d:%d: invalid scalar %q: %w", p.file, loc.Line, loc.Column, trimmed, err)
	}
	return &ScalarNode{Value: val, Raw: trimmed, SourceLocation: loc}, nil
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
func splitMapping(text string) (key, value string, colonIdx int, ok bool) {
	inDouble := false
	inSingle := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
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
			i++
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' && i+1 < len(text) {
					i += 2
				} else {
					i++
				}
			}
		case '\'':
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
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			sb.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("invalid escape at end of quoted string")
		}
		c := s[i+1]
		switch c {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'u':
			if i+5 >= len(s) {
				return "", fmt.Errorf("invalid unicode escape")
			}
			code, ok := parseHex4(s[i+2 : i+6])
			if !ok {
				return "", fmt.Errorf("invalid unicode escape")
			}
			if code > 0x10FFFF {
				return "", fmt.Errorf("invalid unicode escape")
			}
			// extra counts the hex digits consumed beyond the leading \u, so
			// the shared i++ below advances past the final hex digit.
			extra := 4
			switch {
			case isUTF16HighSurrogate(code):
				// Combine a following \uXXXX low surrogate into one
				// supplementary code point instead of two replacement chars
				// (M-34).
				if i+12 <= len(s) && s[i+6] == '\\' && s[i+7] == 'u' {
					if low, lowOK := parseHex4(s[i+8 : i+12]); lowOK && isUTF16LowSurrogate(low) {
						sb.WriteRune(combineUTF16Surrogates(code, low))
						extra = 10
						break
					}
				}
				sb.WriteRune('\uFFFD')
			case isUTF16LowSurrogate(code):
				sb.WriteRune('\uFFFD')
			default:
				sb.WriteRune(rune(code))
			}
			i += extra
		default:
			return "", fmt.Errorf("invalid escape sequence %q", "\\"+string(c))
		}
		i++
	}
	return sb.String(), nil
}
