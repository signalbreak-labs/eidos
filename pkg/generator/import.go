package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
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

	// used tracks parsed-import local variable names across every attribute so
	// two attributes whose names PascalCase identically (e.g. pet_id and petId
	// via overrides) cannot emit duplicate `importX, err :=` declarations, which
	// would not compile (N-26).
	used := make(map[string]bool)

	if parsed.simple {
		return importSetStmts(r, parsed.attrs[0], astgen.Selector(astgen.Ident("req"), "ID"), used)
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
			importSetStmts(r, attr, astgen.IndexExpr(astgen.Ident(partsVar), astgen.IntLit(i)), used)...,
		)
	}

	return stmts
}

// importVarName returns a unique Go identifier for a parsed-import local
// variable derived from an attribute name. The base name is "import" plus the
// attribute's PascalCase form; when a second attribute PascalCase to the same
// identifier (e.g. pet_id and petId via overrides), a numeric suffix
// disambiguates it so the generated import block does not declare the same
// variable twice (N-26).
func importVarName(attr string, used map[string]bool) string {
	base := "import" + naming.SanitizeGoIdentifier(naming.PascalCase(attr))
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	used[name] = true
	return name
}

// importAttributeType returns the schema primitive type of an import target
// attribute. Unknown attribute names default to TypeString so an import ID
// segment is stored verbatim rather than parsed into a type the attribute does
// not have.
func importAttributeType(r ir.ResourceIR, attr string) ir.PrimitiveType {
	for _, a := range r.Schema.Attributes {
		if a.Name == attr {
			return a.Schema.Type
		}
	}
	return ir.TypeString
}

// importNeedsParsing reports whether any import target attribute has a
// non-string primitive type that requires the string ID segment to be parsed
// with strconv before it can be stored.
func importNeedsParsing(r ir.ResourceIR, attrs []string) bool {
	for _, attr := range attrs {
		switch importAttributeType(r, attr) {
		case ir.TypeInt, ir.TypeFloat, ir.TypeBool:
			return true
		}
	}
	return false
}

// importSetStmts emits the statements that store one import ID segment into a
// single Terraform attribute. String attributes receive the segment verbatim;
// int, float, and bool attributes parse it first, because storing a Go string
// into an Int64/Float64/Bool attribute makes the framework's SetAttribute fail
// with a tftypes conversion error ("can't unmarshal tftypes.String into
// *big.Float"). Dynamic and null attributes store the segment verbatim.
func importSetStmts(r ir.ResourceIR, attr string, segment ast.Expr, used map[string]bool) []ast.Stmt {
	typ := importAttributeType(r, attr)
	var parseFn string
	var extraArgs []ast.Expr
	var errPhrase string
	switch typ {
	case ir.TypeInt:
		parseFn, extraArgs, errPhrase = "ParseInt", []ast.Expr{astgen.IntLit(10), astgen.IntLit(64)}, "an integer"
	case ir.TypeFloat:
		parseFn, extraArgs, errPhrase = "ParseFloat", []ast.Expr{astgen.IntLit(64)}, "a number"
	case ir.TypeBool:
		parseFn, errPhrase = "ParseBool", "a boolean"
	}
	if parseFn == "" {
		return []ast.Stmt{setAttributeStmt(attr, segment)}
	}

	varName := importVarName(attr, used)
	callArgs := append([]ast.Expr{segment}, extraArgs...)
	// Parse, error guard, and the SetAttribute append: three statements.
	stmts := make([]ast.Stmt, 0, 3)
	stmts = append(stmts,
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident(varName), astgen.Ident("err")},
			[]ast.Expr{astgen.Call(astgen.QualExpr("strconv", parseFn), callArgs...)},
			token.DEFINE,
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.Block(
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
					astgen.Lit("Error importing "+resourceTypeName(r)),
					astgen.Call(
						astgen.QualExpr("fmt", "Sprintf"),
						astgen.Lit(fmt.Sprintf("Could not parse import identifier %%q as %s: %%s", errPhrase)),
						segment,
						astgen.Ident("err"),
					),
				)),
				astgen.Return(),
			),
		),
	)
	return append(stmts, setAttributeStmt(attr, astgen.Ident(varName)))
}

// setAttributeStmt emits a resp.State.SetAttribute diagnostic append for the
// given attribute and value expression.
func setAttributeStmt(attr string, value ast.Expr) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(
		astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
		astgen.Ellipsis(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "SetAttribute"),
			astgen.Ident("ctx"),
			astgen.Call(astgen.QualExpr("path", "Root"), astgen.Lit(attr)),
			value,
		),
		)))
}

// importPartsVar returns a local variable name for the split import identifier
// parts. The name is derived from the resource name to avoid collisions with
// generated attribute, parameter, or model field names. If the resource name is
// empty, a generic prefix is used so the variable never resolves to the bare
// "ImportIDParts" suffix.
func importPartsVar(r ir.ResourceIR) string {
	name := naming.CamelCase(r.Name)
	if name == "" {
		name = "resource"
	}
	// Sanitize so a digit-leading resource name (e.g. "2fa") cannot produce the
	// invalid local variable "2faImportIDParts" (M-10).
	return naming.SanitizeGoIdentifier(name) + "ImportIDParts"
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
