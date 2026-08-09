package astgen

import (
	"go/ast"
	"strings"
	"testing"
)

// TestAddCommentf asserts AddCommentf renders a formatted single-line comment
// above the next declaration (it delegates to AddComment).
func TestAddCommentf(t *testing.T) {
	f := NewFile("main")
	f.AddCommentf("counter starts at %d.", 1)
	f.AddDecl(FuncDecl("count", Block()))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "// counter starts at 1.") {
		t.Errorf("rendered source missing formatted comment\n%s", got)
	}
}

// TestNil asserts Nil renders the nil identifier.
func TestNil(t *testing.T) {
	b, err := RenderExpr(Nil())
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if got := string(b); got != "nil" {
		t.Errorf("Nil() = %q, want %q", got, "nil")
	}
}

// TestAssign asserts Assign builds a multi-lhs short variable declaration.
func TestAssign(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDecl("swap", Block(
		Assign(
			[]ast.Expr{Ident("a"), Ident("b")},
			[]ast.Expr{Ident("b"), Ident("a")},
		),
	)))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "a, b := b, a") {
		t.Errorf("rendered source missing multi-assign\n%s", got)
	}
}

// TestTypeSwitchStmt asserts a type switch with a type-assertion init, concrete
// cases, and a default case renders.
func TestTypeSwitchStmt(t *testing.T) {
	// The RHS of a type switch init is x.(type), whose Type field is the nil
	// interface (not the nil identifier).
	typeAssert := &ast.TypeAssertExpr{X: Ident("x")}
	cases := []ast.Stmt{
		&ast.CaseClause{
			List: []ast.Expr{Ident("string"), Ident("int")},
			Body: []ast.Stmt{Return(Ident("v"))},
		},
		&ast.CaseClause{Body: []ast.Stmt{Return(Lit("unknown"))}}, // default
	}

	// The TypeSwitchStmt builder takes the init and body; the type-assertion
	// assignment itself is set by the caller via the Assign field (Init stays
	// nil so the printer renders a bare switch header).
	ts := TypeSwitchStmt(nil, Block(cases...))
	ts.Assign = AssignSingle(Ident("v"), typeAssert)

	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"kind",
		Params(Field("x", Ident("any"), "")),
		Results(Field("", Ident("string"), "")),
		Block(ts),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"switch v := x.(type) {",
		"case string, int:",
		"return v",
		"default:",
		`return "unknown"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

// TestInterfaceType asserts InterfaceType renders empty as interface{} and with
// methods as a named interface.
func TestInterfaceType(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(TypeDecl("Empty", InterfaceType()))
	f.AddDecl(TypeDecl("Named", InterfaceType(
		Field("String", FuncType(Params(), Results(Field("", Ident("string"), ""))), ""),
	)))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	// go/printer renders an empty interface over two lines when built from the
	// AST with no source positions; it is semantically interface{}.
	for _, want := range []string{
		"type Empty interface {",
		"type Named interface {",
		"String() string",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

// TestNotEqual asserts NotEqual renders x != y.
func TestNotEqual(t *testing.T) {
	b, err := RenderExpr(NotEqual(Ident("a"), Ident("b")))
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if got := string(b); got != "a != b" {
		t.Errorf("NotEqual = %q, want %q", got, "a != b")
	}
}

// TestEqual asserts Equal renders x == y.
func TestEqual(t *testing.T) {
	b, err := RenderExpr(Equal(Ident("a"), Ident("b")))
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	if got := string(b); got != "a == b" {
		t.Errorf("Equal = %q, want %q", got, "a == b")
	}
}

// TestDec asserts Dec renders x-- as a statement.
func TestDec(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDecl("tick", Block(Dec(Ident("count")))))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "count--") {
		t.Errorf("rendered source missing decrement\n%s", got)
	}
}

// TestCall_Variadic asserts Call with a final Ellipsis renders f(args...), and
// a non-final Ellipsis panics (a caller programming error that must not emit
// unparseable code).
func TestCall_Variadic(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"callit",
		Params(Field("f", Ident("any"), ""), Field("args", SliceType(Ident("string")), "")),
		Params(),
		Block(ExprStmt(Call(Ident("f"), Ellipsis(Ident("args"))))),
	))
	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "f(args...)") {
		t.Errorf("rendered source missing variadic call\n%s", got)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-final Ellipsis argument")
		}
	}()
	Call(Ident("f"), Ellipsis(Ident("a")), Ident("b"))
}
