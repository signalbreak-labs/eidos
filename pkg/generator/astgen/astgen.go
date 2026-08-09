// Package astgen provides a minimal standard-library-only helper for generating
// Go source files using go/ast, go/token, and go/format. It is the sole Go-source
// emission path for the generator after the jennifer-to-AST migration.
package astgen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"math"
	"sort"
	"strings"
)

// File is a lightweight AST-based Go source file builder.
type File struct {
	Package string
	imports map[string]*string
	Decls   []ast.Decl
	pending []string
}

// NewFile creates a new AST file for the given package name.
func NewFile(pkg string) *File {
	return &File{
		Package: pkg,
		imports: make(map[string]*string),
	}
}

// AddImport registers an import path. If alias is non-empty, the import is
// rendered with that alias.
func (f *File) AddImport(path, alias string) {
	var a *string
	if alias != "" {
		a = &alias
	}
	f.imports[path] = a
}

// AddImports registers multiple unaliased import paths in one call.
func (f *File) AddImports(paths ...string) {
	for _, path := range paths {
		f.AddImport(path, "")
	}
}

// AddDecl appends a declaration to the file. Any pending comments added since
// the previous declaration are attached as the new declaration's doc comment.
func (f *File) AddDecl(decl ast.Decl) {
	f.attachPendingComments(decl)
	f.Decls = append(f.Decls, decl)
}

func (f *File) attachPendingComments(decl ast.Decl) {
	if len(f.pending) == 0 {
		return
	}
	cg := commentGroup(f.pending)
	switch d := decl.(type) {
	case *ast.GenDecl:
		d.Doc = cg
		// Only consume the pending comments once they are actually attached to a
		// decl that supports a doc comment (L-58: clearing f.pending for an
		// unsupported decl type would silently drop the queued comments).
		f.pending = nil
	case *ast.FuncDecl:
		d.Doc = cg
		f.pending = nil
	}
	// For any other decl type, leave f.pending intact so the comments attach to
	// the next supported declaration instead of being silently dropped.
}

// AddComment appends a declaration-level comment that will be rendered above
// the next declaration. Multiple AddComment calls before the next AddDecl are
// concatenated so no comment is shifted onto a later declaration.
func (f *File) AddComment(lines ...string) {
	f.pending = append(f.pending, lines...)
}

// AddCommentf appends a single-line declaration-level comment.
func (f *File) AddCommentf(formatStr string, args ...any) {
	f.AddComment(fmt.Sprintf(formatStr, args...))
}

// AST returns the assembled *ast.File. It includes the generated import
// declaration when imports have been registered.
func (f *File) AST() *ast.File {
	decls := f.Decls
	if len(f.imports) > 0 {
		decls = append(f.renderImportDecls(), decls...)
	}
	return &ast.File{
		Package: token.NoPos,
		Name:    &ast.Ident{Name: f.Package},
		Decls:   decls,
	}
}

// Render formats the file source using go/format and returns the bytes.
func (f *File) Render() ([]byte, error) {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f.AST()); err != nil {
		return nil, fmt.Errorf("format ast: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderExpr formats a single expression by wrapping it in a synthetic var
// declaration, formatting the file, and returning the expression source. It is
// useful for unit tests that assert against the rendered form of an expression
// produced by the AST builder.
func RenderExpr(expr ast.Expr) ([]byte, error) {
	f := NewFile("x")
	f.AddDecl(VarDeclGen(VarSpec("_", nil, expr)))
	b, err := f.Render()
	if err != nil {
		return nil, err
	}
	s := string(b)
	s = strings.TrimPrefix(s, "package x\n\nvar _ = ")
	s = strings.TrimSuffix(s, "\n")
	return []byte(s), nil
}

// isStdlibImport reports whether path is a Go standard-library import path.
//
// The heuristic is "no dot ⇒ stdlib": standard-library paths contain no dot
// while third-party module paths always do. Its known limitation is that a
// dot-less internal module path (rare, but legal) would be misfiled as
// stdlib. The generator always emits fully-qualified third-party paths (which
// contain a dot), so this does not arise for generated output (L-59).
func isStdlibImport(path string) bool {
	return !strings.Contains(path, ".")
}

// renderImportDecls renders the registered imports as one or two import
// declarations. Standard-library and third-party imports are emitted as
// separate import blocks so the two groups are visually separated by a blank
// line, matching the grouping goimports produces. go/format does not insert a
// blank line between specs inside a single import block, so a single combined
// block would render the groups concatenated with no separation (L-59).
func (f *File) renderImportDecls() []ast.Decl {
	var stdlib, thirdParty []string
	for path := range f.imports {
		if isStdlibImport(path) {
			stdlib = append(stdlib, path)
		} else {
			thirdParty = append(thirdParty, path)
		}
	}
	sort.Strings(stdlib)
	sort.Strings(thirdParty)

	var decls []ast.Decl
	if len(stdlib) > 0 {
		decls = append(decls, f.importBlock(stdlib))
	}
	if len(thirdParty) > 0 {
		decls = append(decls, f.importBlock(thirdParty))
	}
	return decls
}

// importBlock builds a single factored import declaration for the supplied
// (already-sorted) paths.
func (f *File) importBlock(paths []string) ast.Decl {
	specs := make([]ast.Spec, 0, len(paths))
	for _, path := range paths {
		spec := &ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", path)},
		}
		if alias := f.imports[path]; alias != nil {
			spec.Name = &ast.Ident{Name: *alias}
		}
		specs = append(specs, spec)
	}
	return &ast.GenDecl{Tok: token.IMPORT, Specs: specs}
}

func commentGroup(lines []string) *ast.CommentGroup {
	list := make([]*ast.Comment, 0, len(lines))
	for _, line := range lines {
		for _, sub := range strings.Split(line, "\n") {
			list = append(list, &ast.Comment{Text: "// " + sub})
		}
	}
	return &ast.CommentGroup{List: list}
}

// Ident returns an *ast.Ident for the given name.
func Ident(name string) *ast.Ident { return &ast.Ident{Name: name} }

// Selector returns a selector expression x.Name.
func Selector(x ast.Expr, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: x, Sel: Ident(name)}
}

// QualExpr returns a selector expression pkg.Name. It does not register an
// import; callers must register imports explicitly with File.AddImport.
func QualExpr(pkg, name string) ast.Expr {
	return Selector(Ident(pkg), name)
}

// Call returns a function call expression with the supplied arguments. If the
// final argument is an *ast.Ellipsis, it is interpreted as a variadic call and
// the Ellipsis field of the CallExpr is set.
//
// An Ellipsis argument must be the final argument: Go requires the variadic
// parameter to be last, so f(a..., b) is not parseable. A non-final Ellipsis
// panics here rather than emitting unparseable code downstream (L-57: the prior
// implementation silently kept any later arguments, producing f(a..., b)).
func Call(fn ast.Expr, args ...ast.Expr) *ast.CallExpr {
	call := &ast.CallExpr{Fun: fn, Args: args}
	for i, arg := range args {
		e, ok := arg.(*ast.Ellipsis)
		if !ok {
			continue
		}
		if i != len(args)-1 {
			panic(fmt.Sprintf("astgen.Call: Ellipsis argument at position %d must be the final argument (got %d total)", i, len(args)))
		}
		call.Args[i] = e.Elt
		call.Ellipsis = token.Pos(1)
		break
	}
	return call
}

// Lit returns a basic literal for an untyped string value.
func Lit(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", s)}
}

// BasicLit returns a basic literal with the given kind and raw value.
func BasicLit(kind token.Token, value string) *ast.BasicLit {
	return &ast.BasicLit{Kind: kind, Value: value}
}

// IntLit returns a basic literal for an integer value.
func IntLit(v int) *ast.BasicLit {
	return BasicLit(token.INT, fmt.Sprintf("%d", v))
}

// FloatLit returns an expression for a floating-point value. The rendered
// value matches the vendored jennifer formatter, which appends ".0" to literal
// values that would otherwise look like integers.
//
// Non-finite values (NaN, +Inf, -Inf) have no Go floating-point literal syntax,
// so they are emitted as math.NaN() / math.Inf(±1) call expressions instead of
// the invalid identifiers fmt.Sprintf("%#v", v) would produce ("NaN", "+Inf").
// Callers that feed non-finite values must ensure the generated file imports
// "math"; spec-supplied bounds are filtered before reaching here (see
// float64ValidatorExprs) so this branch is a defensive guard.
func FloatLit(v float64) ast.Expr {
	if math.IsNaN(v) {
		return Call(QualExpr("math", "NaN"))
	}
	if math.IsInf(v, 1) {
		return Call(QualExpr("math", "Inf"), IntLit(1))
	}
	if math.IsInf(v, -1) {
		return Call(QualExpr("math", "Inf"), IntLit(-1))
	}
	s := fmt.Sprintf("%#v", v)
	if !strings.Contains(s, ".") && !strings.Contains(s, "e") && !strings.Contains(s, "E") {
		s += ".0"
	}
	return BasicLit(token.FLOAT, s)
}

// BoolLit returns an identifier expression for true or false.
func BoolLit(v bool) *ast.Ident {
	if v {
		return Ident("true")
	}
	return Ident("false")
}

// Nil returns a nil identifier expression.
func Nil() *ast.Ident { return Ident("nil") }

// Parens returns a parenthesized expression.
func Parens(x ast.Expr) ast.Expr {
	return &ast.ParenExpr{X: x}
}

// StarExpr returns a pointer/dereference expression *x.
func StarExpr(x ast.Expr) *ast.StarExpr {
	return &ast.StarExpr{X: x}
}

// TypeAssertExpr returns a type assertion expression x.(typ).
func TypeAssertExpr(x, typ ast.Expr) *ast.TypeAssertExpr {
	return &ast.TypeAssertExpr{X: x, Type: typ}
}

// IndexExpr returns an index expression x[idx].
func IndexExpr(x, idx ast.Expr) *ast.IndexExpr {
	return &ast.IndexExpr{X: x, Index: idx}
}

// SliceExpr returns a slice expression. All bounds are optional; passing nil
// produces a full slice x[:].
func SliceExpr(x, low, high ast.Expr) *ast.SliceExpr {
	return &ast.SliceExpr{X: x, Low: low, High: high}
}

// Ellipsis returns a variadic argument expression args..., suitable as a
// CallExpr argument.
func Ellipsis(x ast.Expr) *ast.Ellipsis {
	return &ast.Ellipsis{Elt: x}
}

// Assign returns a short variable declaration statement.
func Assign(lhs, rhs []ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Lhs: lhs, Rhs: rhs, Tok: token.DEFINE}
}

// AssignSingle returns a short variable declaration for a single lhs/rhs pair.
func AssignSingle(lhs, rhs ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{lhs}, Rhs: []ast.Expr{rhs}, Tok: token.DEFINE}
}

// AssignStmt returns a general assignment statement with the given token.
func AssignStmt(lhs, rhs []ast.Expr, tok token.Token) *ast.AssignStmt {
	return &ast.AssignStmt{Lhs: lhs, Rhs: rhs, Tok: tok}
}

// VarDecl returns a var declaration statement for a single identifier. If value
// is nil, the declaration omits an initializer (e.g. "var debug bool"). The
// result is a statement for use inside a block.
func VarDecl(name, typ string, value ast.Expr) ast.Stmt {
	spec := &ast.ValueSpec{
		Names: []*ast.Ident{Ident(name)},
		Type:  Ident(typ),
	}
	if value != nil {
		spec.Values = []ast.Expr{value}
	}
	return &ast.DeclStmt{Decl: &ast.GenDecl{
		Tok:   token.VAR,
		Specs: []ast.Spec{spec},
	}}
}

// VarSpec returns a top-level *ast.ValueSpec suitable for VarDeclGen or
// ConstDecl.
func VarSpec(name string, typ, value ast.Expr) *ast.ValueSpec {
	spec := &ast.ValueSpec{
		Names: []*ast.Ident{Ident(name)},
		Type:  typ,
	}
	if value != nil {
		spec.Values = []ast.Expr{value}
	}
	return spec
}

// VarDeclGen returns a top-level var ( ... ) declaration from the supplied
// specs. A single spec renders as a bare "var x T = v" declaration.
func VarDeclGen(specs ...*ast.ValueSpec) *ast.GenDecl {
	out := make([]ast.Spec, len(specs))
	for i, s := range specs {
		out[i] = s
	}
	return &ast.GenDecl{Tok: token.VAR, Specs: out}
}

// VarGroup returns a var (...) declaration with string-typed names and values.
func VarGroup(pairs ...[2]string) *ast.GenDecl {
	specs := make([]ast.Spec, 0, len(pairs))
	for _, p := range pairs {
		specs = append(specs, &ast.ValueSpec{
			Names:  []*ast.Ident{Ident(p[0])},
			Type:   Ident("string"),
			Values: []ast.Expr{Lit(p[1])},
		})
	}
	return &ast.GenDecl{Tok: token.VAR, Specs: specs}
}

// ConstDecl returns a top-level const declaration from the supplied specs.
func ConstDecl(specs ...*ast.ValueSpec) *ast.GenDecl {
	out := make([]ast.Spec, len(specs))
	for i, s := range specs {
		out[i] = s
	}
	return &ast.GenDecl{Tok: token.CONST, Specs: out}
}

// ExprStmt wraps an expression in a statement.
func ExprStmt(expr ast.Expr) *ast.ExprStmt {
	return &ast.ExprStmt{X: expr}
}

// Return returns a return statement with the given results.
func Return(results ...ast.Expr) *ast.ReturnStmt {
	return &ast.ReturnStmt{Results: results}
}

// Break returns a break statement.
func Break() *ast.BranchStmt {
	return &ast.BranchStmt{Tok: token.BREAK}
}

// Continue returns a continue statement.
func Continue() *ast.BranchStmt {
	return &ast.BranchStmt{Tok: token.CONTINUE}
}

// Defer returns a defer statement wrapping the supplied call expression.
func Defer(call *ast.CallExpr) *ast.DeferStmt {
	return &ast.DeferStmt{Call: call}
}

// If returns an if statement with the given condition and body.
func If(cond ast.Expr, body ...ast.Stmt) *ast.IfStmt {
	return &ast.IfStmt{Cond: cond, Body: &ast.BlockStmt{List: body}}
}

// IfElse returns an if statement with the given condition, then block, and
// else block.
func IfElse(cond ast.Expr, then, els *ast.BlockStmt) *ast.IfStmt {
	return &ast.IfStmt{Cond: cond, Body: then, Else: els}
}

// IfElseIf returns an if statement whose else branch is another if statement.
func IfElseIf(cond ast.Expr, then *ast.BlockStmt, els *ast.IfStmt) *ast.IfStmt {
	return &ast.IfStmt{Cond: cond, Body: then, Else: els}
}

// Block returns a block statement containing the given statements.
func Block(stmts ...ast.Stmt) *ast.BlockStmt {
	return &ast.BlockStmt{List: stmts}
}

// ForStmt returns a C-style for statement.
func ForStmt(init ast.Stmt, cond ast.Expr, post ast.Stmt, body *ast.BlockStmt) *ast.ForStmt {
	return &ast.ForStmt{Init: init, Cond: cond, Post: post, Body: body}
}

// RangeStmt returns a range statement. Pass nil for key or value when omitted.
// tok is token.DEFINE for := or token.ASSIGN for =.
func RangeStmt(key, value ast.Expr, tok token.Token, x ast.Expr, body *ast.BlockStmt) *ast.RangeStmt {
	return &ast.RangeStmt{Key: key, Value: value, Tok: tok, X: x, Body: body}
}

// SwitchStmt returns a switch statement over the given tag expression with a
// body containing case clauses.
func SwitchStmt(tag ast.Expr, body *ast.BlockStmt) *ast.SwitchStmt {
	return &ast.SwitchStmt{Tag: tag, Body: body}
}

// CaseClause returns a case clause with the given case expressions and an
// empty body. Add statements to the Body field.
func CaseClause(cases ...ast.Expr) *ast.CaseClause {
	return &ast.CaseClause{List: cases, Body: []ast.Stmt{}}
}

// TypeSwitchStmt returns a type switch statement.
func TypeSwitchStmt(init ast.Stmt, body *ast.BlockStmt) *ast.TypeSwitchStmt {
	return &ast.TypeSwitchStmt{Init: init, Body: body}
}

// DeclStmt wraps a declaration in a statement.
func DeclStmt(decl ast.Decl) *ast.DeclStmt {
	return &ast.DeclStmt{Decl: decl}
}

// LabeledStmt returns a labeled statement.
func LabeledStmt(label string, stmt ast.Stmt) *ast.LabeledStmt {
	return &ast.LabeledStmt{Label: Ident(label), Stmt: stmt}
}

// FuncDecl returns a simple function declaration without parameters or results.
func FuncDecl(name string, body *ast.BlockStmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: Ident(name),
		Type: &ast.FuncType{},
		Body: body,
	}
}

// FuncDeclFull returns a function declaration with parameters and results.
func FuncDeclFull(name string, params, results *ast.FieldList, body *ast.BlockStmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: Ident(name),
		Type: FuncType(params, results),
		Body: body,
	}
}

// FuncType returns a function type with the given parameters and results.
func FuncType(params, results *ast.FieldList) *ast.FuncType {
	return &ast.FuncType{Params: params, Results: results}
}

// FuncLit returns an anonymous function literal.
func FuncLit(typ *ast.FuncType, body *ast.BlockStmt) *ast.FuncLit {
	return &ast.FuncLit{Type: typ, Body: body}
}

// Params returns a parameter field list.
func Params(fields ...*ast.Field) *ast.FieldList {
	return &ast.FieldList{List: fields}
}

// Results returns a result field list.
func Results(fields ...*ast.Field) *ast.FieldList {
	return &ast.FieldList{List: fields}
}

// MethodDecl returns a method declaration.
func MethodDecl(name, recvName string, recvType ast.Expr, params, results *ast.FieldList, body *ast.BlockStmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: Ident(name),
		Recv: FieldGroup(Field(recvName, recvType, "")),
		Type: FuncType(params, results),
		Body: body,
	}
}

// TypeDecl returns a top-level type declaration.
func TypeDecl(name string, typ ast.Expr) *ast.GenDecl {
	return &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{Name: Ident(name), Type: typ},
		},
	}
}

// StructType returns a struct type with the given fields.
func StructType(fields ...*ast.Field) *ast.StructType {
	return &ast.StructType{Fields: FieldGroup(fields...)}
}

// Field returns a field with the given type and optional struct tag. If name
// is empty, the field has no name (used for anonymous results or parameters).
func Field(name string, typ ast.Expr, tag string) *ast.Field {
	f := &ast.Field{Type: typ}
	if name != "" {
		f.Names = []*ast.Ident{Ident(name)}
	}
	if tag != "" {
		f.Tag = &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("`%s`", tag)}
	}
	return f
}

// FieldGroup returns a field list containing the supplied fields.
func FieldGroup(fields ...*ast.Field) *ast.FieldList {
	return &ast.FieldList{List: fields}
}

// MapType returns a map type map[key]value.
func MapType(key, value ast.Expr) *ast.MapType {
	return &ast.MapType{Key: key, Value: value}
}

// SliceType returns a slice type []elem.
func SliceType(elem ast.Expr) *ast.ArrayType {
	return &ast.ArrayType{Elt: elem}
}

// ArrayType returns an array type [length]elem.
func ArrayType(length, elem ast.Expr) *ast.ArrayType {
	return &ast.ArrayType{Len: length, Elt: elem}
}

// InterfaceType returns an interface type with the given methods. An empty
// method set renders as interface{}.
//
// Methods is always a non-nil (possibly empty) FieldList: go/printer
// dereferences x.Methods.List unconditionally when rendering an interface, so a
// nil Methods panics go/format instead of rendering interface{} (L-60).
func InterfaceType(methods ...*ast.Field) *ast.InterfaceType {
	return &ast.InterfaceType{Methods: FieldGroup(methods...)}
}

// CompositeLit returns a composite literal for the given type and key/value
// elements.
func CompositeLit(typ ast.Expr, elems ...ast.Expr) *ast.CompositeLit {
	return &ast.CompositeLit{Type: typ, Elts: elems}
}

// KeyValue returns a key:value expression with an identifier key.
func KeyValue(key string, value ast.Expr) *ast.KeyValueExpr {
	return &ast.KeyValueExpr{Key: Ident(key), Value: value}
}

// KeyValueExpr returns a key:value expression. Key may be any expression.
func KeyValueExpr(key, value ast.Expr) *ast.KeyValueExpr {
	return &ast.KeyValueExpr{Key: key, Value: value}
}

// Binary returns a binary expression with the given operator.
func Binary(x ast.Expr, op token.Token, y ast.Expr) *ast.BinaryExpr {
	return &ast.BinaryExpr{X: x, Op: op, Y: y}
}

// NotEqual returns x != y.
func NotEqual(x, y ast.Expr) *ast.BinaryExpr {
	return Binary(x, token.NEQ, y)
}

// Equal returns x == y.
func Equal(x, y ast.Expr) *ast.BinaryExpr {
	return Binary(x, token.EQL, y)
}

// Unary returns a unary expression with the given operator.
func Unary(op token.Token, x ast.Expr) *ast.UnaryExpr {
	return &ast.UnaryExpr{Op: op, X: x}
}

// UnaryPtr returns the address-of expression &x.
func UnaryPtr(x ast.Expr) *ast.UnaryExpr {
	return Unary(token.AND, x)
}

// Inc returns an increment statement x++.
func Inc(x ast.Expr) *ast.IncDecStmt {
	return &ast.IncDecStmt{X: x, Tok: token.INC}
}

// Dec returns a decrement statement x--.
func Dec(x ast.Expr) *ast.IncDecStmt {
	return &ast.IncDecStmt{X: x, Tok: token.DEC}
}
