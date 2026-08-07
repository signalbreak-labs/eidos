package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"log"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// importStateBody returns the statements that implement a resource's
// ImportState method. It supports both simple import IDs (assigned to a single
// identifier attribute) and composite import IDs parsed from
// ResourceIR.ImportIDFormat.
//
// Malformed import formats are rejected by validateImportIDFormat at generation
// time, so the runtime diagnostic here is a last-resort guard for schemas that
// bypass the normal ResourceIR validation path.
func importStateBody(r ir.ResourceIR) []ast.Stmt {
	parsed, err := parseImportIDFormat(r.ImportIDFormat, r.IDAttribute)
	if err != nil {
		return []ast.Stmt{
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
				astgen.Lit("Invalid Import Format"),
				astgen.Lit(fmt.Sprintf("Unable to parse import id format %q: %v.", r.ImportIDFormat, err)),
			)),
		}
	}

	if parsed.simple {
		return []ast.Stmt{
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
				astgen.Ellipsis(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "SetAttribute"),
					astgen.Ident("ctx"),
					astgen.Call(
						astgen.QualExpr("path", "Root"),
						astgen.Lit(parsed.attrs[0]),
					),
					astgen.Selector(astgen.Ident("req"), "ID"),
				)),
			)),
		}
	}

	partsVar := importPartsVar(r)
	stmts := []ast.Stmt{
		astgen.AssignSingle(
			astgen.Ident(partsVar),
			astgen.Call(
				astgen.QualExpr("strings", "Split"),
				astgen.Selector(astgen.Ident("req"), "ID"),
				astgen.Lit(parsed.delimiter),
			),
		),
		astgen.If(
			astgen.NotEqual(
				astgen.Call(astgen.Ident("len"), astgen.Ident(partsVar)),
				astgen.IntLit(len(parsed.attrs)),
			),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
					astgen.Lit("Unexpected Import Identifier"),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit(fmt.Sprintf("Expected import identifier with format %q. Got %%q.", r.ImportIDFormat)),
						astgen.Selector(astgen.Ident("req"), "ID"),
					),
				)),
				astgen.Return(),
			),
		),
	}

	for i, attr := range parsed.attrs {
		stmts = append(stmts,
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
				astgen.Ellipsis(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "SetAttribute"),
					astgen.Ident("ctx"),
					astgen.Call(
						astgen.QualExpr("path", "Root"),
						astgen.Lit(attr),
					),
					astgen.IndexExpr(astgen.Ident(partsVar), astgen.IntLit(i)),
				)),
			)),
		)
	}

	return stmts
}

// importPartsVar returns a local variable name for the split import identifier
// parts. The name is derived from the resource name to avoid collisions with
// generated attribute, parameter, or model field names. If the resource name is
// empty, a generic prefix is used so the variable never resolves to the bare
// "ImportIDParts" suffix.
func importPartsVar(r ir.ResourceIR) string {
	name := camelCase(r.Name)
	if name == "" {
		name = "resource"
	}
	return name + "ImportIDParts"
}

// importFormat holds the result of parsing an import ID format string.
type importFormat struct {
	attrs     []string
	delimiter string
	simple    bool
	// warning is a non-empty human-readable note when parsing made a silent
	// decision the user should know about, such as a single-attribute format
	// whose brace name was overridden by id_attribute (L-44). It is surfaced by
	// the caller rather than returned as an error because the parse still
	// succeeds.
	warning string
}

// parseImportIDFormat extracts the target Terraform attribute names and the
// delimiter from an import ID format string.
//
// A simple format contains a single attribute and the whole import ID is stored
// in that attribute. Composite formats contain multiple attributes separated by
// a delimiter; the import ID is split on that delimiter and each segment is
// stored in the corresponding attribute.
//
// Brace-enclosed names such as "{petId}" are always treated as attributes. When
// the format contains no braces, every bare word token is treated as an
// attribute. This lets formats derived from URI templates such as
// "/pets/{petId}" be parsed as a simple ID even though they contain static path
// segments.
//
// Attribute names and delimiters are treated as ASCII; non-ASCII characters
// are considered delimiter/whitespace characters.
func parseImportIDFormat(format, idAttribute string) (importFormat, error) {
	format = strings.TrimSpace(format)
	if format == "" {
		attr := idAttribute
		if attr == "" {
			attr = "id"
		}
		return importFormat{attrs: []string{attr}, simple: true}, nil
	}

	attrs, starts, ends := parseImportIDTokens(format)

	if len(attrs) == 0 {
		return importFormat{}, errors.New("import id format contains no attributes")
	}
	if len(attrs) == 1 {
		attr := attrs[0]
		var warning string
		if idAttribute != "" {
			if idAttribute != attr {
				// The format names one attribute (e.g. {petId}) but id_attribute
				// overrides it (e.g. "id"). This is intentional behavior, but
				// previously silent; surface it so a config mistake here is
				// visible rather than invisibly changing the import target
				// attribute (L-44).
				warning = fmt.Sprintf(
					"import id format %q names attribute %q, but id_attribute %q overrides it; importing into %q",
					format, attr, idAttribute, idAttribute,
				)
			}
			attr = idAttribute
		}
		return importFormat{attrs: []string{attr}, simple: true, warning: warning}, nil
	}

	// Composite formats must be written with brace-enclosed attributes, e.g.
	// "{project_id}:{resource_id}". The inter-attribute segment is the literal
	// text between the closing brace of one attribute and the opening brace of the
	// next. All gaps must use the same delimiter.
	gap := format[ends[0]:starts[1]]
	if len(gap) < 2 || gap[0] != '}' || gap[len(gap)-1] != '{' {
		return importFormat{}, errors.New("composite import id format requires brace-enclosed attributes with a delimiter, e.g. {attr1}:{attr2}")
	}
	delimiter := gap[1 : len(gap)-1]
	if delimiter == "" {
		return importFormat{}, errors.New("composite import id format requires a non-empty delimiter")
	}

	want := "}" + delimiter + "{"
	for i := 1; i < len(attrs)-1; i++ {
		gap := format[ends[i]:starts[i+1]]
		if gap != want {
			return importFormat{}, fmt.Errorf("composite import id format uses inconsistent delimiters: %q and %q", want, gap)
		}
	}

	return importFormat{attrs: attrs, delimiter: delimiter, simple: false}, nil
}

// parseImportIDTokens scans an import ID format string and returns the attribute
// names and their start/end byte indices. Brace-enclosed names are always
// attributes; bare words are only attributes when the format contains no braces.
func parseImportIDTokens(format string) (attrs []string, starts, ends []int) {
	hasBraces := strings.ContainsRune(format, '{') || strings.ContainsRune(format, '}')

	var tokenStart = -1
	inBrace := false

	for i, r := range format {
		switch {
		case r == '{':
			inBrace = true
			tokenStart = i + 1
		case r == '}':
			inBrace = false
			if tokenStart >= 0 && tokenStart < i {
				attrs = append(attrs, format[tokenStart:i])
				starts = append(starts, tokenStart)
				ends = append(ends, i)
			}
			tokenStart = -1
		case inBrace:
			// Token content is captured by its start/end indices.
		case isImportIDTokenRune(r):
			if tokenStart < 0 {
				tokenStart = i
			}
		default:
			if tokenStart >= 0 {
				if !hasBraces {
					attrs = append(attrs, format[tokenStart:i])
					starts = append(starts, tokenStart)
					ends = append(ends, i)
				}
				tokenStart = -1
			}
		}
	}
	if tokenStart >= 0 && !hasBraces {
		attrs = append(attrs, format[tokenStart:])
		starts = append(starts, tokenStart)
		ends = append(ends, len(format))
	}

	return attrs, starts, ends
}

// validateImportIDFormat checks that a managed resource's import ID format can be
// parsed at generation time. This validation is resource-specific: only managed
// resources generate an ImportState method, so data sources, ephemeral resources,
// list resources, and actions do not run this check. Malformed formats are
// surfaced as generator errors rather than runtime diagnostics in the generated
// provider.
func validateImportIDFormat(r ir.ResourceIR) error {
	parsed, err := parseImportIDFormat(r.ImportIDFormat, r.IDAttribute)
	if err != nil {
		return fmt.Errorf("resource %q has invalid ImportIDFormat %q: %w", r.Name, r.ImportIDFormat, err)
	}
	// Surface the otherwise-invisible single-attribute override as a log warning
	// so a config mistake (e.g. {petId} with id_attribute "id") is not silently
	// changing the import target attribute (L-44).
	if parsed.warning != "" {
		log.Printf("resource %q: %s", r.Name, parsed.warning)
	}
	return nil
}

// isImportIDTokenRune reports whether r is a valid ASCII character in an import
// ID attribute token. Non-ASCII characters are treated as delimiters or
// whitespace, matching the parser's documented ASCII-only behavior.
func isImportIDTokenRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}
