package generator

import (
	"go/ast"
	"go/token"
	"strconv"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
)

// ETagHelpersFile returns internal/provider/etag.go: the private-state and
// response-header helpers the wired CRUD bodies of ETag-tracking resources
// call (see resource_etag.go). It is emitted only when AnyResourceTracksETag.
func ETagHelpersFile() File {
	return GoCodeAST("internal/provider/etag.go", generateETagHelpersFile())
}

// ETagHelpersTestFile returns internal/provider/etag_test.go, the unit tests
// for the etag.go helpers.
func ETagHelpersTestFile() File {
	return GoCodeAST("internal/provider/etag_test.go", generateETagHelpersTestFile())
}

func diagsAppendWarning(diagsVar, summary, detail string) ast.Stmt {
	return astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident(diagsVar), "AddWarning"), astgen.Lit(summary), astgen.Lit(detail)))
}

func ctxKeyParams(extra ...*ast.Field) *ast.FieldList {
	fields := make([]*ast.Field, 0, 2+len(extra))
	fields = append(fields,
		astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
		astgen.Field("key", astgen.Ident("string"), ""),
	)
	return astgen.Params(append(fields, extra...)...)
}

func generateETagHelpersFile() *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("encoding/json", "")
	f.AddImport("fmt", "")
	f.AddImport("net/http", "")
	f.AddImport("strings", "")
	f.AddImport("unicode/utf8", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/diag", "")

	diagType := astgen.QualExpr("diag", "Diagnostics")
	key := astgen.Ident("etagPrivateStateKey")

	f.AddComment("etagPrivateStateKey is the private state key holding a resource's last known ETag. The ETag lives in private state, not the schema, so it never appears in plans or causes diffs.")
	f.AddDecl(astgen.ConstDecl(astgen.VarSpec("etagPrivateStateKey", nil, astgen.Lit("etag"))))

	f.AddComment("etagPrivateStateReader is satisfied by the framework's resource private state (req.Private).")
	f.AddDecl(astgen.TypeDecl("etagPrivateStateReader", astgen.InterfaceType(
		astgen.Field("GetKey", astgen.FuncType(ctxKeyParams(), astgen.Results(
			astgen.Field("", astgen.SliceType(astgen.Ident("byte")), ""),
			astgen.Field("", diagType, ""),
		)), ""),
	)))
	f.AddComment("etagPrivateStateWriter is satisfied by the framework's resource private state (resp.Private).")
	f.AddDecl(astgen.TypeDecl("etagPrivateStateWriter", astgen.InterfaceType(
		astgen.Field("SetKey", astgen.FuncType(
			ctxKeyParams(astgen.Field("value", astgen.SliceType(astgen.Ident("byte")), "")),
			astgen.Results(astgen.Field("", diagType, "")),
		), ""),
	)))

	// privateETag
	f.AddComment("privateETag returns the ETag stored in private state, or \"\" when none is stored (a new resource, or state written before ETags were tracked). An unreadable value is ignored with a warning rather than sent.")
	f.AddDecl(astgen.FuncDeclFull("privateETag",
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("p", astgen.Ident("etagPrivateStateReader"), ""),
		),
		astgen.Results(astgen.Field("", astgen.Ident("string"), ""), astgen.Field("", diagType, "")),
		astgen.Block(
			astgen.Assign([]ast.Expr{astgen.Ident("raw"), astgen.Ident("diags")}, []ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("p"), "GetKey"), astgen.Ident("ctx"), key)}),
			astgen.If(
				astgen.Binary(
					astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
					token.LOR,
					astgen.Equal(astgen.Call(astgen.Ident("len"), astgen.Ident("raw")), astgen.IntLit(0)),
				),
				astgen.Return(astgen.Lit(""), astgen.Ident("diags")),
			),
			astgen.VarDecl("etag", "string", nil),
			&ast.IfStmt{
				Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(astgen.QualExpr("json", "Unmarshal"), astgen.Ident("raw"), astgen.UnaryPtr(astgen.Ident("etag")))),
				Cond: astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				Body: astgen.Block(
					astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("diags"), "AddWarning"),
						astgen.Lit("Ignoring unreadable stored ETag"),
						astgen.Call(astgen.QualExpr("fmt", "Sprintf"),
							astgen.Lit("The ETag stored in this resource's private state could not be decoded (%s), so the request is sent without it."),
							astgen.Ident("err")),
					)),
					astgen.Return(astgen.Lit(""), astgen.Ident("diags")),
				),
			},
			astgen.Return(astgen.Ident("etag"), astgen.Ident("diags")),
		),
	))

	// setPrivateETag
	f.AddComment("setPrivateETag stores etag in private state. An empty etag (the response carried none) removes the stored value, so a stale ETag is never sent back.")
	f.AddDecl(astgen.FuncDeclFull("setPrivateETag",
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("p", astgen.Ident("etagPrivateStateWriter"), ""),
			astgen.Field("etag", astgen.Ident("string"), ""),
		),
		astgen.Results(astgen.Field("", diagType, "")),
		astgen.Block(
			astgen.If(
				astgen.Equal(astgen.Ident("etag"), astgen.Lit("")),
				astgen.Return(astgen.Call(astgen.Selector(astgen.Ident("p"), "SetKey"), astgen.Ident("ctx"), key, astgen.Nil())),
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Call(astgen.QualExpr("utf8", "ValidString"), astgen.Ident("etag"))),
				astgen.AssignSingle(astgen.Ident("diags"), astgen.Call(astgen.Selector(astgen.Ident("p"), "SetKey"), astgen.Ident("ctx"), key, astgen.Nil())),
				diagsAppendWarning("diags", "ETag not stored", "The API returned an ETag that is not valid UTF-8, which Terraform private state cannot hold, so the next conditional request is sent without it."),
				astgen.Return(astgen.Ident("diags")),
			),
			astgen.Assign([]ast.Expr{astgen.Ident("raw"), astgen.Ident("err")}, []ast.Expr{astgen.Call(astgen.QualExpr("json", "Marshal"), astgen.Ident("etag"))}),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.VarDecl("diags", "diag.Diagnostics", nil),
				astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("diags"), "AddError"),
					astgen.Lit("Could not store ETag"),
					astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("Encoding the ETag for private state failed: %s"), astgen.Ident("err")),
				)),
				astgen.Return(astgen.Ident("diags")),
			),
			astgen.Return(astgen.Call(astgen.Selector(astgen.Ident("p"), "SetKey"), astgen.Ident("ctx"), key, astgen.Ident("raw"))),
		),
	))

	// responseETag
	f.AddComment("responseETag returns the response's ETag header verbatim (weak W/\"...\" validators included; the API decides how to compare them), or \"\" when absent.")
	f.AddDecl(astgen.FuncDeclFull("responseETag",
		astgen.Params(astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("http", "Response")), "")),
		astgen.Results(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(
			astgen.If(astgen.Equal(astgen.Ident("resp"), astgen.Nil()), astgen.Return(astgen.Lit(""))),
			astgen.Return(astgen.Call(astgen.QualExpr("strings", "TrimSpace"),
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Header"), "Get"), astgen.Lit("ETag")))),
		),
	))
	return f.AST()
}

func generateETagHelpersTestFile() *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("net/http", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/diag", "")

	tT := astgen.StarExpr(astgen.QualExpr("testing", "T"))
	ctx := astgen.Call(astgen.QualExpr("context", "Background"))
	fatalf := func(format string, args ...ast.Expr) ast.Stmt {
		return astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Fatalf"), append([]ast.Expr{astgen.Lit(format)}, args...)...))
	}
	newStore := astgen.AssignSingle(astgen.Ident("store"), astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident("fakeETagPrivateState"),
		astgen.KeyValue("data", astgen.CompositeLit(astgen.MapType(astgen.Ident("string"), astgen.SliceType(astgen.Ident("byte"))))))))
	setCall := func(v ast.Expr) ast.Expr {
		return astgen.Call(astgen.Ident("setPrivateETag"), ctx, astgen.Ident("store"), v)
	}
	getAssign := astgen.Assign([]ast.Expr{astgen.Ident("got"), astgen.Ident("diags")}, []ast.Expr{astgen.Call(astgen.Ident("privateETag"), ctx, astgen.Ident("store"))})
	requireClean := func(expr ast.Expr) ast.Stmt {
		return astgen.If(astgen.Call(astgen.Selector(expr, "HasError")), fatalf("unexpected diagnostics: %v", expr))
	}
	warnCount := func(diagsExpr ast.Expr, want int) ast.Stmt {
		return astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Selector(diagsExpr, "WarningsCount")), astgen.IntLit(want)),
			fatalf("warnings = %v, want "+strconv.Itoa(want), diagsExpr),
		)
	}
	testFn := func(name, doc string, body ...ast.Stmt) {
		f.AddComment(name + " " + doc)
		f.AddDecl(astgen.FuncDeclFull(name, astgen.Params(astgen.Field("t", tT, "")), astgen.Results(), astgen.Block(body...)))
	}

	f.AddComment("fakeETagPrivateState is an in-memory stand-in for the framework's resource private state.")
	f.AddDecl(astgen.TypeDecl("fakeETagPrivateState", astgen.StructType(
		astgen.Field("data", astgen.MapType(astgen.Ident("string"), astgen.SliceType(astgen.Ident("byte"))), ""),
	)))
	f.AddDecl(astgen.MethodDecl("GetKey", "f", astgen.StarExpr(astgen.Ident("fakeETagPrivateState")),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), ""), astgen.Field("key", astgen.Ident("string"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.Ident("byte")), ""), astgen.Field("", astgen.QualExpr("diag", "Diagnostics"), "")),
		astgen.Block(astgen.Return(astgen.IndexExpr(astgen.Selector(astgen.Ident("f"), "data"), astgen.Ident("key")), astgen.Nil())),
	))
	f.AddDecl(astgen.MethodDecl("SetKey", "f", astgen.StarExpr(astgen.Ident("fakeETagPrivateState")),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), ""), astgen.Field("key", astgen.Ident("string"), ""), astgen.Field("value", astgen.SliceType(astgen.Ident("byte")), "")),
		astgen.Results(astgen.Field("", astgen.QualExpr("diag", "Diagnostics"), "")),
		astgen.Block(
			astgen.If(
				astgen.Equal(astgen.Call(astgen.Ident("len"), astgen.Ident("value")), astgen.IntLit(0)),
				astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Selector(astgen.Ident("f"), "data"), astgen.Ident("key"))),
				astgen.Return(astgen.Nil()),
			),
			astgen.AssignStmt([]ast.Expr{astgen.IndexExpr(astgen.Selector(astgen.Ident("f"), "data"), astgen.Ident("key"))}, []ast.Expr{astgen.Ident("value")}, token.ASSIGN),
			astgen.Return(astgen.Nil()),
		),
	))

	weak := `W/"v1"`
	testFn("TestPrivateETag_RoundTrip", "stores an ETag (weak validators included) and reads it back verbatim.",
		newStore,
		requireClean(setCall(astgen.Lit(weak))),
		getAssign,
		requireClean(astgen.Ident("diags")),
		astgen.If(astgen.NotEqual(astgen.Ident("got"), astgen.Lit(weak)), fatalf("privateETag = %q, want %q", astgen.Ident("got"), astgen.Lit(weak))),
	)
	testFn("TestPrivateETag_EmptyClears", "removes the stored ETag when the response carried none, so a stale ETag is never sent.",
		newStore,
		requireClean(setCall(astgen.Lit(`"v1"`))),
		requireClean(setCall(astgen.Lit(""))),
		astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("store"), "data")), astgen.IntLit(0)),
			fatalf("store = %v, want empty", astgen.Selector(astgen.Ident("store"), "data")),
		),
		getAssign,
		astgen.If(
			astgen.Binary(astgen.NotEqual(astgen.Ident("got"), astgen.Lit("")), token.LOR, astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError"))),
			fatalf("privateETag = %q, %v; want empty and no errors", astgen.Ident("got"), astgen.Ident("diags")),
		),
	)
	testFn("TestPrivateETag_Unreadable", "ignores a value that is not a JSON string, with a warning.",
		newStore,
		astgen.AssignStmt([]ast.Expr{astgen.IndexExpr(astgen.Selector(astgen.Ident("store"), "data"), astgen.Ident("etagPrivateStateKey"))}, []ast.Expr{astgen.Call(astgen.SliceType(astgen.Ident("byte")), astgen.Lit("{"))}, token.ASSIGN),
		getAssign,
		astgen.If(astgen.NotEqual(astgen.Ident("got"), astgen.Lit("")), fatalf("privateETag = %q, want empty", astgen.Ident("got"))),
		warnCount(astgen.Ident("diags"), 1),
	)
	testFn("TestSetPrivateETag_InvalidUTF8", "does not store an ETag private state cannot hold, with a warning.",
		newStore,
		astgen.AssignSingle(astgen.Ident("diags"), setCall(astgen.Lit("\xff"))),
		warnCount(astgen.Ident("diags"), 1),
		astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Ident("len"), astgen.Selector(astgen.Ident("store"), "data")), astgen.IntLit(0)),
			fatalf("store = %v, want empty", astgen.Selector(astgen.Ident("store"), "data")),
		),
	)
	testFn("TestResponseETag", "reads the ETag header and tolerates a nil response.",
		astgen.AssignSingle(astgen.Ident("resp"), astgen.UnaryPtr(astgen.CompositeLit(astgen.QualExpr("http", "Response"),
			astgen.KeyValue("Header", astgen.CompositeLit(astgen.QualExpr("http", "Header")))))),
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Header"), "Set"), astgen.Lit("ETag"), astgen.Lit(` "v2" `))),
		astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Ident("responseETag"), astgen.Ident("resp")), astgen.Lit(`"v2"`)),
			fatalf("responseETag = %q", astgen.Call(astgen.Ident("responseETag"), astgen.Ident("resp"))),
		),
		astgen.If(
			astgen.NotEqual(astgen.Call(astgen.Ident("responseETag"), astgen.Nil()), astgen.Lit("")),
			fatalf("responseETag(nil) should be empty"),
		),
	)
	return f.AST()
}
