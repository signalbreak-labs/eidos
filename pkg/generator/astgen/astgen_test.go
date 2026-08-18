package astgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strings"
	"testing"
)

func TestFile_Render(t *testing.T) {
	f := NewFile("main")
	f.AddImport("fmt", "")
	f.AddImport("log", "")
	f.AddComment("main is the entry point.")
	f.AddDecl(VarGroup(
		[2]string{"version", "dev"},
		[2]string{"commit", "none"},
	))
	f.AddDecl(FuncDecl("main", Block(
		ExprStmt(Call(Selector(Ident("fmt"), "Println"), Lit("hello"))),
	)))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)

	for _, want := range []string{
		"package main",
		"import (",
		"\"fmt\"",
		"\"log\"",
		"// main is the entry point.",
		"var (",
		"version string = \"dev\"",
		"commit  string = \"none\"",
		"func main()",
		"fmt.Println(\"hello\")",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

// TestAST_DanglingPendingCommentPanics locks in the N-33 contract: a comment
// queued with AddComment but never attached to a declaration (the file ends, or
// only non-commentable decls follow) must panic at AST() instead of being
// silently dropped from the emitted file. The panic surfaces as a render error
// through the render-recovery wrappers, giving the generator author a signal
// the comment was omitted.
func TestAST_DanglingPendingCommentPanics(t *testing.T) {
	f := NewFile("x")
	f.AddComment("this comment is never attached")
	// No AddDecl follows: the pending comment would be dropped.

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected AST() to panic on a dangling pending comment, got nil")
		}
		msg, ok := rec.(string)
		if !ok || !strings.Contains(msg, "never attached to a declaration") {
			t.Fatalf("expected panic to name the dangling comment, got: %v", rec)
		}
		if !strings.Contains(msg, "this comment is never attached") {
			t.Fatalf("expected panic to quote the dangling comment, got: %v", rec)
		}
	}()
	_ = f.AST()
}

// TestAST_CommentAttachesToNextDecl confirms the normal path still works: a
// comment queued before a declaration attaches as its doc comment and leaves
// nothing pending, so AST() does not panic.
func TestAST_CommentAttachesToNextDecl(t *testing.T) {
	f := NewFile("x")
	f.AddComment("doc for the var")
	decl := VarGroup([2]string{"v", "1"})
	f.AddDecl(decl)
	if len(f.pending) != 0 {
		t.Fatalf("expected the comment to attach to the decl, got %d pending", len(f.pending))
	}
	if decl.Doc == nil {
		t.Fatal("expected the var decl to carry the queued comment as its doc comment")
	}
	// Rendering must not panic on a properly-attached comment.
	if _, err := f.Render(); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

func TestVarDecl(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDecl("main", Block(
		VarDecl("debug", "bool", nil),
		VarDecl("count", "int", IntLit(7)),
	)))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		"var debug bool",
		"var count int = 7",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "var debug bool = nil") {
		t.Errorf("rendered nil-init var declaration\n%s", got)
	}
}

func TestFile_Render_Alias(t *testing.T) {
	f := NewFile("main")
	f.AddImport("github.com/example/foo", "foo")
	f.AddDecl(FuncDecl("main", Block()))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "foo \"github.com/example/foo\"") {
		t.Errorf("rendered source missing aliased import, got:\n%s", got)
	}
}

// TestIsStdlibImport locks in the N-36 classification: a path is stdlib only
// when its first segment is a known standard-library root. A dot-less internal
// module path such as "providers/foo" must be classified as third-party, not
// stdlib — the old "no dot anywhere ⇒ stdlib" rule misfiled it (L-59).
func TestIsStdlibImport(t *testing.T) {
	stdlib := []string{
		"fmt", "net/http", "encoding/json", "os", "context", "strings",
		"go/token", "runtime", "text/template",
	}
	for _, p := range stdlib {
		if !isStdlibImport(p) {
			t.Errorf("isStdlibImport(%q) = false, want true", p)
		}
	}

	thirdParty := []string{
		"github.com/example/foo",
		"providers/foo", // dot-less internal module path: third-party (N-36)
		"k8s.io/api/core/v1",
		"gopkg.in/yaml.v3",
		"example.com",
	}
	for _, p := range thirdParty {
		if isStdlibImport(p) {
			t.Errorf("isStdlibImport(%q) = true, want false", p)
		}
	}
}

func TestFile_Render_ImportGrouping(t *testing.T) {
	f := NewFile("main")
	f.AddImports("fmt", "net/http")
	f.AddImport("github.com/example/foo", "")
	f.AddDecl(FuncDecl("main", Block()))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)

	stdlibIdx := strings.Index(got, "\"fmt\"")
	thirdPartyIdx := strings.Index(got, "\"github.com/example/foo\"")
	if stdlibIdx == -1 || thirdPartyIdx == -1 {
		t.Fatalf("missing imports in:\n%s", got)
	}
	if stdlibIdx > thirdPartyIdx {
		t.Errorf("stdlib import should precede third-party import:\n%s", got)
	}
}

func TestTypeAndStruct(t *testing.T) {
	f := NewFile("provider")
	f.AddDecl(TypeDecl("Widget", StructType(
		Field("ID", Ident("string"), `tfsdk:"id"`),
		Field("Count", Ident("int"), `tfsdk:"count"`),
	)))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"type Widget struct",
		"ID    string `tfsdk:\"id\"`",
		"Count int    `tfsdk:\"count\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestMethodDecl(t *testing.T) {
	f := NewFile("provider")
	f.AddImport("context", "")
	f.AddDecl(MethodDecl(
		"Metadata", "r", StarExpr(Ident("Widget")),
		Params(Field("_", QualExpr("context", "Context"), "")),
		Params(Field("resp", StarExpr(Ident("Response")), "")),
		Block(
			AssignStmt(
				[]ast.Expr{Selector(Ident("resp"), "Name")},
				[]ast.Expr{Lit("widget")},
				token.ASSIGN,
			),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "func (r *Widget) Metadata(_ context.Context) (resp *Response)") {
		t.Errorf("rendered source missing method signature\n%s", got)
	}
}

func TestFuncDeclFull(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"add",
		Params(Field("a", Ident("int"), ""), Field("b", Ident("int"), "")),
		Params(Field("", Ident("int"), "")),
		Block(Return(Binary(Ident("a"), token.ADD, Ident("b")))),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "func add(a int, b int) int") {
		t.Errorf("rendered source missing function signature\n%s", got)
	}
}

func TestVarAndConstDecls(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(VarDeclGen(
		VarSpec("debug", Ident("bool"), BoolLit(false)),
		VarSpec("count", Ident("int"), IntLit(42)),
	))
	f.AddDecl(ConstDecl(
		VarSpec("greeting", Ident("string"), Lit("hello")),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"var (",
		"debug bool = false",
		"count int  = 42",
		"const greeting string = \"hello\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestExpressions(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"exprs",
		Params(),
		Params(),
		Block(
			AssignSingle(Ident("p"), Parens(Ident("x"))),
			AssignSingle(Ident("q"), UnaryPtr(Ident("x"))),
			AssignSingle(Ident("r"), StarExpr(Ident("x"))),
			AssignSingle(Ident("s"), IndexExpr(Ident("arr"), IntLit(0))),
			AssignSingle(Ident("t"), SliceExpr(Ident("arr"), nil, nil)),
			AssignSingle(Ident("u"), TypeAssertExpr(Ident("x"), Ident("string"))),
			AssignSingle(Ident("v"), Call(Ident("f"), Ellipsis(Ident("args")))),
			AssignSingle(Ident("w"), FloatLit(3.14)),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"p := (x)",
		"q := &x",
		"r := *x",
		"s := arr[0]",
		"t := arr[:]",
		"u := x.(string)",
		"v := f(args...)",
		"w := 3.14",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

// TestFloatLit_NonFinite verifies that non-finite floats render as valid Go
// expressions (math.NaN / math.Inf) rather than the invalid identifiers that
// fmt.Sprintf("%#v") would produce (M-8).
func TestFloatLit_NonFinite(t *testing.T) {
	cases := []struct {
		name string
		v    float64
		want string
	}{
		{"nan", math.NaN(), "math.NaN()"},
		{"pos_inf", math.Inf(1), "math.Inf(1)"},
		{"neg_inf", math.Inf(-1), "math.Inf(-1)"},
		{"finite", 3.14, "3.14"},
		{"finite_int_like", 5.0, "5.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFile("main")
			f.AddImport("math", "")
			f.AddDecl(FuncDeclFull(
				"expr",
				Params(),
				Params(),
				Block(AssignSingle(Ident("x"), FloatLit(tc.v))),
			))
			b, err := f.Render()
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			got := string(b)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("rendered source missing %q\n%s", tc.want, got)
			}
			assertParses(t, got)
		})
	}
}

func TestCompositeAndCollections(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"collections",
		Params(),
		Params(),
		Block(
			AssignSingle(Ident("m"), CompositeLit(MapType(Ident("string"), Ident("int")),
				KeyValue("a", IntLit(1)),
				KeyValueExpr(Lit("b"), IntLit(2)),
			)),
			AssignSingle(Ident("s"), CompositeLit(SliceType(Ident("int")),
				IntLit(1), IntLit(2),
			)),
			AssignSingle(Ident("a"), CompositeLit(ArrayType(IntLit(2), Ident("int")),
				IntLit(1), IntLit(2),
			)),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"m := map[string]int{a: 1, \"b\": 2}",
		"s := []int{1, 2}",
		"a := [2]int{1, 2}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestControlFlow(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"ctrl",
		Params(),
		Params(),
		Block(
			ForStmt(
				AssignSingle(Ident("i"), IntLit(0)),
				Binary(Ident("i"), token.LSS, IntLit(10)),
				Inc(Ident("i")),
				Block(
					If(Binary(Ident("i"), token.EQL, IntLit(5)), Break()),
					If(Binary(Ident("i"), token.EQL, IntLit(3)), Continue()),
				),
			),
			RangeStmt(nil, nil, token.ILLEGAL, Ident("xs"), Block()),
			RangeStmt(Ident("i"), nil, token.DEFINE, Ident("ys"), Block()),
			RangeStmt(Ident("k"), Ident("v"), token.DEFINE, Ident("zs"), Block()),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"for i := 0; i < 10; i++",
		"break",
		"continue",
		"for range xs",
		"for i := range ys",
		"for k, v := range zs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestSwitchAndCase(t *testing.T) {
	f := NewFile("main")
	cases := []*ast.CaseClause{
		CaseClause(Lit("a")),
		CaseClause(Lit("b")),
	}
	cases[0].Body = []ast.Stmt{Return(IntLit(1))}
	cases[1].Body = []ast.Stmt{Return(IntLit(2))}
	body := Block(cases[0], cases[1])

	f.AddDecl(FuncDeclFull(
		"sw",
		Params(Field("x", Ident("string"), "")),
		Params(Field("", Ident("int"), "")),
		Block(
			SwitchStmt(Ident("x"), body),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"switch x {",
		"case \"a\":",
		"return 1",
		"case \"b\":",
		"return 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestIfElse(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"branch",
		Params(Field("x", Ident("int"), "")),
		Params(),
		Block(
			IfElse(
				Binary(Ident("x"), token.GTR, IntLit(0)),
				Block(ExprStmt(Call(Ident("positive")))),
				Block(ExprStmt(Call(Ident("nonPositive")))),
			),
			IfElseIf(
				Binary(Ident("x"), token.LSS, IntLit(0)),
				Block(ExprStmt(Call(Ident("negative")))),
				If(Ident("zero"), ExprStmt(Call(Ident("zero")))),
			),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	if !strings.Contains(got, "if x > 0 {") {
		t.Errorf("missing if\n%s", got)
	}
	if !strings.Contains(got, "} else {") {
		t.Errorf("missing else\n%s", got)
	}
}

func TestFuncLitAndInterface(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"lits",
		Params(),
		Params(),
		Block(
			AssignSingle(Ident("fn"), FuncLit(
				FuncType(Params(), Results(Field("", Ident("int"), ""))),
				Block(Return(IntLit(1))),
			)),
			AssignSingle(Ident("empty"), Ident("any")),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"fn := func() int {",
		"return 1",
		"empty := any",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestDeferAndLabeledAndDeclStmt(t *testing.T) {
	f := NewFile("main")
	f.AddDecl(FuncDeclFull(
		"misc",
		Params(),
		Params(),
		Block(
			Defer(Call(Ident("cleanup"))),
			LabeledStmt("loop", ForStmt(nil, BoolLit(true), nil, Block(Break()))),
			DeclStmt(VarDeclGen(VarSpec("x", Ident("int"), IntLit(1)))),
		),
	))

	b, err := f.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := string(b)
	assertParses(t, got)
	for _, want := range []string{
		"defer cleanup()",
		"loop:",
		"var x int = 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered source missing %q\n%s", want, got)
		}
	}
}

func TestRenderExpr(t *testing.T) {
	expr := Call(Selector(Ident("fmt"), "Println"), Lit("hello"))
	b, err := RenderExpr(expr)
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	got := string(b)
	if got != "fmt.Println(\"hello\")" {
		t.Errorf("RenderExpr = %q, want %q", got, "fmt.Println(\"hello\")")
	}
}

func TestRenderExpr_Multiline(t *testing.T) {
	expr := CompositeLit(MapType(Ident("string"), Ident("int")),
		KeyValueExpr(Lit("a"), IntLit(1)),
	)
	b, err := RenderExpr(expr)
	if err != nil {
		t.Fatalf("RenderExpr: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "map[string]int{") {
		t.Errorf("RenderExpr missing map literal header: %q", got)
	}
	if !strings.Contains(got, `"a": 1`) {
		t.Errorf("RenderExpr missing map entry: %q", got)
	}
}

func assertParses(t *testing.T, src string) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), "test.go", src, parser.AllErrors); err != nil {
		t.Errorf("rendered source does not parse: %v\n%s", err, src)
	}
}
