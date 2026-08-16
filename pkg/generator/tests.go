package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ProviderTestFile returns the generated internal/provider/provider_test.go file
// containing unit tests for the generated provider implementation.
func ProviderTestFile(pir ir.ProviderIR) File {
	path := filepath.Join("internal", "provider", "provider_test.go")
	return GoCodeAST(path, generateProviderTestFile(pir))
}

// generateProviderTestFile builds the *ast.File for internal/provider/provider_test.go.
func generateProviderTestFile(pir ir.ProviderIR) *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/provider", "tfframeworkprovider")

	f.AddComment("TestProviderSchemaValidation verifies that the generated provider schema is valid.")
	f.AddDecl(astgen.FuncDeclFull("TestProviderSchemaValidation",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("p"), astgen.Call(astgen.Ident("New"))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkprovider", "SchemaResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("p"), "Schema"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkprovider", "SchemaRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.AssignSingle(astgen.Ident("diags"), astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Schema"), "ValidateImplementation"),
				astgen.Call(astgen.QualExpr("context", "Background")),
			)),
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("schema validation failed: %s"),
					astgen.Ident("diags"),
				)),
			),
		),
	))

	f.AddComment("TestProviderMetadata verifies that the generated provider reports the expected type name.")
	f.AddDecl(astgen.FuncDeclFull("TestProviderMetadata",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("p"), astgen.Call(astgen.Ident("New"))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkprovider", "MetadataResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("p"), "Metadata"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkprovider", "MetadataRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.If(
				astgen.NotEqual(astgen.Selector(astgen.Ident("resp"), "TypeName"), astgen.Lit(providerTypeName(pir))),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("TypeName = %q, want %q"),
					astgen.Selector(astgen.Ident("resp"), "TypeName"),
					astgen.Lit(providerTypeName(pir)),
				)),
			),
		),
	))

	return f.AST()
}

// ResourceTestFile returns the generated internal/provider/resource_<name>_test.go
// file containing unit tests for a single managed resource.
func ResourceTestFile(r ir.ResourceIR) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("resource_%s_test.go", naming.SnakeCase(r.Name)))
	return GoCodeAST(path, generateResourceTestFile(r))
}

// generateResourceTestFile builds the *ast.File for resource_<name>_test.go.
func generateResourceTestFile(r ir.ResourceIR) *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource", "tfframeworkresource")

	structName := resourceStructName(r)

	f.AddCommentf("Test%sSchemaValidation verifies that the generated resource schema is valid.", structName)
	f.AddDecl(astgen.FuncDeclFull("Test"+structName+"SchemaValidation",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("r"), astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName)))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkresource", "SchemaResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("r"), "Schema"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkresource", "SchemaRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.AssignSingle(astgen.Ident("diags"), astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Schema"), "ValidateImplementation"),
				astgen.Call(astgen.QualExpr("context", "Background")),
			)),
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("schema validation failed: %s"),
					astgen.Ident("diags"),
				)),
			),
		),
	))

	f.AddCommentf("Test%sMetadata verifies that the generated resource reports the expected type name.", structName)
	f.AddDecl(astgen.FuncDeclFull("Test"+structName+"Metadata",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("r"), astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName)))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkresource", "MetadataResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("r"), "Metadata"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkresource", "MetadataRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.If(
				astgen.NotEqual(astgen.Selector(astgen.Ident("resp"), "TypeName"), astgen.Lit(resourceTypeName(r))),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("TypeName = %q, want %q"),
					astgen.Selector(astgen.Ident("resp"), "TypeName"),
					astgen.Lit(resourceTypeName(r)),
				)),
			),
		),
	))

	return f.AST()
}

// ResourceTestFiles returns the generated resource test files for every
// ResourceIR in the provider. Files are emitted in the order supplied.
func ResourceTestFiles(resources []ir.ResourceIR) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		files = append(files, ResourceTestFile(r))
	}
	return files
}

// DataSourceTestFile returns the generated internal/provider/data_source_<name>_test.go
// file containing unit tests for a single data source.
func DataSourceTestFile(ds ir.DataSourceIR) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("data_source_%s_test.go", naming.SnakeCase(ds.Name)))
	return GoCodeAST(path, generateDataSourceTestFile(ds))
}

// generateDataSourceTestFile builds the *ast.File for data_source_<name>_test.go.
func generateDataSourceTestFile(ds ir.DataSourceIR) *ast.File {
	f := astgen.NewFile("provider")
	f.AddImport("context", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/datasource", "tfframeworkdatasource")

	structName := dataSourceStructName(ds)

	f.AddCommentf("Test%sSchemaValidation verifies that the generated data source schema is valid.", structName)
	f.AddDecl(astgen.FuncDeclFull("Test"+structName+"SchemaValidation",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("d"), astgen.Call(astgen.Ident("New"+structName))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkdatasource", "SchemaResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("d"), "Schema"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkdatasource", "SchemaRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.AssignSingle(astgen.Ident("diags"), astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Schema"), "ValidateImplementation"),
				astgen.Call(astgen.QualExpr("context", "Background")),
			)),
			astgen.If(
				astgen.Call(astgen.Selector(astgen.Ident("diags"), "HasError")),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("schema validation failed: %s"),
					astgen.Ident("diags"),
				)),
			),
		),
	))

	f.AddCommentf("Test%sMetadata verifies that the generated data source reports the expected type name.", structName)
	f.AddDecl(astgen.FuncDeclFull("Test"+structName+"Metadata",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("d"), astgen.Call(astgen.Ident("New"+structName))),
			astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec("resp", astgen.QualExpr("tfframeworkdatasource", "MetadataResponse"), nil))),
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Ident("d"), "Metadata"),
				astgen.Call(astgen.QualExpr("context", "Background")),
				astgen.CompositeLit(astgen.QualExpr("tfframeworkdatasource", "MetadataRequest")),
				astgen.UnaryPtr(astgen.Ident("resp")),
			)),
			astgen.If(
				astgen.NotEqual(astgen.Selector(astgen.Ident("resp"), "TypeName"), astgen.Lit(dataSourceTypeName(ds))),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("TypeName = %q, want %q"),
					astgen.Selector(astgen.Ident("resp"), "TypeName"),
					astgen.Lit(dataSourceTypeName(ds)),
				)),
			),
		),
	))

	return f.AST()
}

// DataSourceTestFiles returns the generated data source test files for every
// DataSourceIR in the provider. Files are emitted in the order supplied.
func DataSourceTestFiles(dataSources []ir.DataSourceIR) []File {
	files := make([]File, 0, len(dataSources))
	for _, ds := range dataSources {
		files = append(files, DataSourceTestFile(ds))
	}
	return files
}

// MapperTestFile returns the generated internal/protocol/value_mappers_test.go
// file containing unit tests for the value mapper functions. The providerImport
// argument is the canonical import path for the generated provider package.
func MapperTestFile(resources []ir.ResourceIR, providerImport string) File {
	path := filepath.Join("internal", "protocol", "value_mappers_test.go")
	return GoCodeAST(path, generateMapperTestFile(resources, providerImport))
}

// generateMapperTestFile builds the *ast.File for internal/protocol/value_mappers_test.go.
func generateMapperTestFile(resources []ir.ResourceIR, providerImport string) *ast.File {
	f := astgen.NewFile("protocol")
	f.AddImport("testing", "")
	f.AddImport("reflect", "")
	f.AddImport(providerImport, "provider")

	seen := make(map[string]struct{})
	for _, r := range resources {
		generateMapperModelTests(f, schema.ResourceAPIModelName(r), r.Schema, seen)
	}

	return f.AST()
}

// DataSourceMapperTestFile returns the generated internal/protocol/data_source_value_mappers_test.go
// file containing unit tests for the data source value mapper functions. It should
// only be emitted alongside generated data source value mappers.
func DataSourceMapperTestFile(dataSources []ir.DataSourceIR, providerImport string) File {
	path := filepath.Join("internal", "protocol", "data_source_value_mappers_test.go")
	return GoCodeAST(path, generateDataSourceMapperTestFile(dataSources, providerImport))
}

// generateDataSourceMapperTestFile builds the *ast.File for internal/protocol/data_source_value_mappers_test.go.
func generateDataSourceMapperTestFile(dataSources []ir.DataSourceIR, providerImport string) *ast.File {
	f := astgen.NewFile("protocol")
	f.AddImport("testing", "")
	f.AddImport("reflect", "")
	f.AddImport(providerImport, "provider")

	seen := make(map[string]struct{})
	for _, ds := range dataSources {
		generateMapperModelTests(f, dataSourceAPIModelName(ds), ds.Schema, seen)
	}

	return f.AST()
}

// generateMapperModelTests emits type and round-trip tests for a single model
// and its attribute-scoped nested children. The seen map prevents duplicate
// test functions when the same nested type is reachable through multiple paths.
func generateMapperModelTests(f *astgen.File, modelName string, obj ir.ObjectSchemaIR, seen map[string]struct{}) {
	if _, ok := seen[modelName]; ok {
		return
	}
	seen[modelName] = struct{}{}

	typeFuncName := modelName + "Type"
	fromValueFuncName := modelName + "FromValue"
	toValueFuncName := modelName + "ToValue"

	f.AddCommentf("Test%sType verifies that the generated %sType function returns an object type.", modelName, modelName)
	f.AddDecl(astgen.FuncDeclFull("Test"+modelName+"Type",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("typ"), astgen.Call(astgen.Ident(typeFuncName))),
			astgen.If(
				astgen.Equal(astgen.Ident("typ"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatal"),
					astgen.Lit("Type() returned nil"),
				)),
			),
		),
	))

	f.AddCommentf("Test%sRoundTrip verifies that %sToValue and %sFromValue are inverses for an empty model.", modelName, modelName, modelName)
	f.AddDecl(astgen.FuncDeclFull("Test"+modelName+"RoundTrip",
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		astgen.Params(),
		astgen.Block(
			astgen.AssignSingle(astgen.Ident("original"), astgen.CompositeLit(astgen.QualExpr("provider", modelName))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("v"), astgen.Ident("err")},
				[]ast.Expr{astgen.Call(astgen.Ident(toValueFuncName), astgen.Ident("original"))},
				token.DEFINE,
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("ToValue error: %v"),
					astgen.Ident("err"),
				)),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("got"), astgen.Ident("err")},
				[]ast.Expr{astgen.Call(astgen.Ident(fromValueFuncName), astgen.Ident("v"))},
				token.DEFINE,
			),
			astgen.If(
				astgen.NotEqual(astgen.Ident("err"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("FromValue error: %v"),
					astgen.Ident("err"),
				)),
			),
			astgen.If(
				astgen.Unary(token.NOT, astgen.Call(
					astgen.QualExpr("reflect", "DeepEqual"),
					astgen.Ident("got"),
					astgen.Ident("original"),
				)),
				astgen.ExprStmt(astgen.Call(
					astgen.Selector(astgen.Ident("t"), "Fatalf"),
					astgen.Lit("round-trip mismatch:\n got: %+v\nwant: %+v"),
					astgen.Ident("got"),
					astgen.Ident("original"),
				)),
			),
		),
	))

	// Recursively emit tests for attribute-scoped nested children. Block-scoped
	// nested mappers are not covered by this recursion.
	scope := schema.ResolveFieldNames(obj.Attributes)
	for _, attr := range obj.Attributes {
		childSchema, childName := schema.MapperChildSchema(scope, modelName, attr)
		if childSchema == nil {
			continue
		}
		generateMapperModelTests(f, childName, *childSchema, seen)
	}
}

// TestFiles returns the complete set of generated unit-test files for a
// provider. It includes provider_test.go, one resource_<name>_test.go per
// resource and one data_source_<name>_test.go per data source.
func TestFiles(pir ir.ProviderIR, cfg BuildConfig) []File {
	clientImport := cfg.modulePath() + "/internal/client"
	files := make([]File, 0, 1+len(pir.Resources)+len(pir.DataSources))
	files = append(files, ProviderTestFile(pir))
	files = append(files, ResourceTestFiles(pir.Resources)...)
	files = append(files, ResourceAcceptanceTestFiles(pir, cfg)...)
	files = append(files, DataSourceTestFiles(pir.DataSources)...)
	// Coverage tests exercise the extracted *Remote helper methods directly
	// against an httptest mock, covering happy and unhappy HTTP paths without
	// TF_ACC. The shared helpers file is emitted only when at least one
	// coverage test file is produced, so its helpers are never left unused
	// (staticcheck U1000).
	coverage := ResourceCoverageTestFiles(pir.Resources, clientImport)
	dsCoverage := DataSourceCoverageTestFiles(pir.DataSources, clientImport)
	actCoverage := ActionCoverageTestFiles(pir.Actions, clientImport)
	ephCoverage := EphemeralCoverageTestFiles(pir.EphemeralResources, clientImport)
	listCoverage := ListCoverageTestFiles(pir.ListResources, clientImport)
	if len(coverage) > 0 || len(dsCoverage) > 0 || len(actCoverage) > 0 || len(ephCoverage) > 0 || len(listCoverage) > 0 {
		files = append(files, SharedTestHelpersFile(clientImport))
		files = append(files, coverage...)
		files = append(files, dsCoverage...)
		files = append(files, actCoverage...)
		files = append(files, ephCoverage...)
		files = append(files, listCoverage...)
	}
	return files
}
