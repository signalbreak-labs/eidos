package generator

import (
	"go/ast"
	"go/token"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
)

// MainGoFile returns the generated main.go file for a Terraform provider built
// from the supplied BuildConfig. The generated entry point serves protocol v6
// via providerserver.Serve by default, and falls back to tf5server.Serve when
// the --protocol-version flag is set to 5.
func MainGoFile(cfg BuildConfig) File {
	return GoCodeAST("main.go", generateMainGoFile(cfg))
}

// generateMainGoFile builds the *ast.File for the generated provider main.go
// using the standard-library go/ast package via pkg/generator/astgen.
func generateMainGoFile(cfg BuildConfig) *ast.File {
	providerImport := cfg.modulePath() + "/internal/provider"
	address := cfg.sourceAddress()

	f := astgen.NewFile("main")
	f.AddImport("context", "")
	f.AddImport("flag", "")
	f.AddImport("fmt", "")
	f.AddImport("log", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/providerserver", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-go/tfprotov5/tf5server", "")
	f.AddImport(providerImport, "provider")

	f.AddComment("main is the executable entry point for the generated Terraform provider.")
	f.AddDecl(versionVarDecl())

	mainBody := astgen.Block(
		astgen.VarDecl("debug", "bool", nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("flag"), "BoolVar"),
			astgen.Unary(token.AND, astgen.Ident("debug")),
			astgen.Lit("debug"),
			astgen.BoolLit(false),
			astgen.Lit("Set to true to run the provider with support for debuggers like delve"),
		)),
		astgen.VarDecl("protocolVersion", "int", nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("flag"), "IntVar"),
			astgen.Unary(token.AND, astgen.Ident("protocolVersion")),
			astgen.Lit("protocol-version"),
			astgen.IntLit(6),
			astgen.Lit("Terraform plugin protocol version to serve (5 or 6)"),
		)),
		astgen.VarDecl("printVersion", "bool", nil),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("flag"), "BoolVar"),
			astgen.Unary(token.AND, astgen.Ident("printVersion")),
			astgen.Lit("version"),
			astgen.BoolLit(false),
			astgen.Lit("Print version information and exit"),
		)),
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("flag"), "Parse"))),
		astgen.If(
			astgen.Ident("printVersion"),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("fmt"), "Printf"),
				astgen.Lit("version=%s\ncommit=%s\ndate=%s\n"),
				astgen.Ident("version"),
				astgen.Ident("commit"),
				astgen.Ident("date"),
			)),
			astgen.Return(),
		),
		astgen.AssignSingle(astgen.Ident("address"), astgen.Lit(address)),
		astgen.If(
			astgen.Equal(astgen.Ident("protocolVersion"), astgen.IntLit(5)),
			astgen.AssignSingle(
				astgen.Ident("err"),
				astgen.Call(
					astgen.Selector(astgen.Ident("tf5server"), "Serve"),
					astgen.Ident("address"),
					astgen.Call(
						astgen.Selector(astgen.Ident("providerserver"), "NewProtocol5"),
						astgen.Call(astgen.Selector(astgen.Ident("provider"), "New")),
					),
				),
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("log"), "Fatal"),
					astgen.Ident("err"),
				)),
			),
			astgen.Return(),
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("protocolVersion"), astgen.IntLit(6)),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("log"), "Printf"),
				astgen.Lit("warning: unsupported --protocol-version %d; defaulting to protocol version 6"),
				astgen.Ident("protocolVersion"),
			)),
		),
		astgen.AssignSingle(
			astgen.Ident("opts"),
			astgen.CompositeLit(
				astgen.Selector(astgen.Ident("providerserver"), "ServeOpts"),
				astgen.KeyValue("Address", astgen.Ident("address")),
				astgen.KeyValue("Debug", astgen.Ident("debug")),
			),
		),
		astgen.AssignSingle(
			astgen.Ident("err"),
			astgen.Call(
				astgen.Selector(astgen.Ident("providerserver"), "Serve"),
				astgen.Call(astgen.Selector(astgen.Ident("context"), "Background")),
				astgen.Selector(astgen.Ident("provider"), "New"),
				astgen.Ident("opts"),
			),
		),
		astgen.If(
			astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("log"), "Fatal"),
				astgen.Ident("err"),
			)),
		),
	)
	f.AddDecl(astgen.FuncDecl("main", mainBody))

	return f.AST()
}
