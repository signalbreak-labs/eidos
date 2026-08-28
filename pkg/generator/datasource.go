package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ErrUnknownDatasourceBlockNesting is the sentinel returned (via panic, then
// recovered into an ErrorFile by the per-file wrapper) when a data source
// block has a NestingMode the generator does not recognize. Data source blocks
// fail closed — like action blocks (ErrUnknownActionBlockNesting) — rather
// than silently degrading to SingleNestedBlock, so an unexpected IR shape is
// surfaced instead of producing a wrong schema (L-32).
var ErrUnknownDatasourceBlockNesting = errors.New("unknown data source block nesting mode")

// DataSourceFile returns the generated internal/provider/data_source_<name>.go
// file for a Terraform plugin-framework data source built from the supplied
// DataSourceIR. clientImport is the import path of the generated internal/client
// package, used when the data source Read is wired to the API client.
func DataSourceFile(ds ir.DataSourceIR, clientImport string) File {
	path := path.Join("internal", "provider", fmt.Sprintf("data_source_%s.go", naming.SnakeCase(ds.Name)))
	file, err := renderEntitySafely(func() (*ast.File, error) {
		return generateDataSourceFile(ds, clientImport), nil
	})
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// DataSourceFiles returns the generated data source files for every DataSourceIR
// in the provider. Files are emitted in the order the data sources are supplied.
// clientImport is the import path of the generated internal/client package.
func DataSourceFiles(dataSources []ir.DataSourceIR, clientImport string) []File {
	files := make([]File, 0, len(dataSources))
	for _, ds := range dataSources {
		files = append(files, DataSourceFile(ds, clientImport))
	}
	return files
}

// dataSourceAPIModelName returns the generated API model struct name for a data source.
func dataSourceAPIModelName(ds ir.DataSourceIR) string {
	return dataSourceStructName(ds) + "Model"
}

// dataSourceTypeName returns the Terraform data source type name. It prefers
// DataSourceIR.TypeName and falls back to a snake_cased data source name so
// generated type names are always valid Terraform identifiers.
func dataSourceTypeName(ds ir.DataSourceIR) string {
	if strings.TrimSpace(ds.TypeName) != "" {
		return strings.TrimSpace(ds.TypeName)
	}
	return typeNameFallback(ds.Name)
}

// generateDataSourceFile builds the *ast.File for internal/provider/data_source_<name>.go.
// clientImport is the import path of the generated internal/client package, used
// when the data source Read is wired to the API client.
func generateDataSourceFile(ds ir.DataSourceIR, clientImport string) *ast.File {
	f := astgen.NewFile("provider")

	structName := dataSourceStructName(ds)
	modelName := structName + "Model"
	typeName := dataSourceTypeName(ds)
	wiring := planDataSourceWiring(ds)

	// Interface assertion.
	f.AddComment("Compile-time interface assertion.")
	assertSpecs := []*ast.ValueSpec{astgen.VarSpec(
		"_",
		astgen.QualExpr("datasource", "DataSource"),
		astgen.Call(
			astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
			astgen.Nil(),
		),
	)}
	if wiring.wired {
		assertSpecs = append(assertSpecs, astgen.VarSpec(
			"_",
			astgen.QualExpr("datasource", "DataSourceWithConfigure"),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		))
	}
	f.AddDecl(astgen.VarDeclGen(assertSpecs...))

	// Data source struct. Wired data sources carry the API client supplied by
	// the provider's Configure method via the framework provider-data mechanism.
	f.AddCommentf("%s is the generated Terraform data source implementation.", structName)
	if wiring.wired {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
			astgen.Field("client", astgen.StarExpr(astgen.QualExpr("client", "Client")), ""),
		)))
	} else {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType()))
	}

	// Model struct.
	f.AddCommentf("%s describes the data source state shape.", modelName)
	modelFields := make([]*ast.Field, 0, len(ds.Schema.Attributes)+len(ds.Schema.Blocks))
	for _, attr := range ds.Schema.Attributes {
		if schema.SkipAttrForModel(attr) {
			continue
		}
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(attr.Name),
			modelFieldType(attr),
			modelFieldTags(attr),
		))
	}
	for _, block := range ds.Schema.Blocks {
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(block.Name),
			blockModelFieldType(block),
			fmt.Sprintf("tfsdk:%q", block.Name),
		))
	}
	f.AddDecl(astgen.TypeDecl(modelName, astgen.StructType(modelFields...)))

	// New constructor.
	f.AddCommentf("New%s returns a new instance of the generated data source.", structName)
	f.AddDecl(astgen.FuncDeclFull(
		"New"+structName,
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("datasource", "DataSource"), "")),
		astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName))))),
	))

	// Metadata method.
	f.AddComment("Metadata returns the data source type name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "d", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("datasource", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("datasource", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(typeName)},
			token.ASSIGN,
		)),
	))

	// Schema method.
	f.AddComment("Schema returns the data source schema.")
	schemaValues := dataSourceSchemaValues(ds)
	f.AddDecl(astgen.MethodDecl(
		"Schema", "d", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("datasource", "SchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("datasource", "SchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("schema", "Schema"), schemaValues...)},
			token.ASSIGN,
		)),
	))

	// Read method. Wired data sources call the read endpoint and store the API
	// response as state; data sources without a resolvable read mapping keep the
	// honest scaffold body. The extracted *Remote helper is emitted alongside
	// Read so the request/response logic is unit-testable without a tfsdk.Config.
	f.AddComment("Read fetches remote state into the data source model.")
	readBody, helperComment, helperDecl := dataSourceReadPlan(ds, wiring, modelName, structName)
	f.AddDecl(astgen.MethodDecl(
		"Read", "d", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("datasource", "ReadRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("datasource", "ReadResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(readBody...),
	))
	if helperDecl != nil {
		f.AddComment(helperComment)
		f.AddDecl(helperDecl)
	}

	// Configure method. Wired data sources implement DataSourceWithConfigure to
	// receive the API client constructed by the provider's Configure method.
	if wiring.wired {
		f.AddComment("Configure stores the API client supplied by the provider.")
		f.AddDecl(dataSourceConfigureDecl(structName))
	}

	f.AddImport("context", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/datasource", "datasource")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/datasource/schema", "schema")
	// The model struct references types.* for every attribute and block field.
	// Auto-inferred data sources with an empty schema produce an empty model and
	// must not import types, or the import is unused and the provider does not
	// compile.
	if len(ds.Schema.Attributes) > 0 || len(ds.Schema.Blocks) > 0 {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	if wiring.wired {
		// Wired Read bodies build and send HTTP requests through the generated
		// client and decode JSON responses. Data source reads have no request
		// body. A list data source fetches pages via client.ListAllPages
		// (url.Values) and decodes each page with a json.Decoder + UseNumber
		// (so numeric item fields map to Int64/Number attributes, not float64),
		// importing bytes for bytes.NewReader; a single-object read decodes one
		// body with io.EOF handling, so it imports io instead. The two paths are
		// mutually exclusive, so bytes and io are never imported together.
		f.AddImport(clientImport, "client")
		f.AddImports("encoding/json", "fmt", "net/http")
		if wiring.list {
			f.AddImport("bytes", "")
		} else {
			f.AddImport("io", "")
		}
		if wiring.needsURL {
			f.AddImport("net/url", "")
		}
		if wiring.needsStrings {
			f.AddImport("strings", "")
		}
		if wiring.needsStrconv {
			f.AddImport("strconv", "")
		}
	}
	// Data source attributes emit per-attribute validators only for a
	// discriminated union (the DiscriminatorValidator, D2); otherwise the
	// schema/validator package is only referenced by block size validators
	// ([]validator.List/Set). Gate the import on those conditions to avoid
	// "imported and not used".
	if objectSchemaNeedsBlockSizeValidators(ds.Schema) || schema.ObjectSchemaHasDiscriminatedUnion(ds.Schema) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	}
	needsList, needsSet := blockValidatorPackageImports(ds.Schema)
	if needsList {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/listvalidator", "listvalidator")
	}
	if needsSet {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/setvalidator", "setvalidator")
	}

	return f.AST()
}

// dataSourceReadPlan resolves the Read method body and the extracted *Remote
// helper declaration (plus its doc comment) for a data source. Unwired data
// sources keep the honest scaffold body and return a nil helper so no extracted
// method is emitted. Single-object and list data sources use distinct helper
// methods (readRemote / readListRemote). Factoring this out of
// generateDataSourceFile keeps that function's cognitive complexity bounded.
func dataSourceReadPlan(ds ir.DataSourceIR, wiring dataSourceWiringPlan, modelName, structName string) (readBody []ast.Stmt, helperComment string, helperDecl *ast.FuncDecl) {
	readBody = scaffoldDataSourceReadBody(modelName)
	if !wiring.wired {
		return readBody, "", nil
	}
	if wiring.list {
		return wiredListDataSourceReadBody(modelName),
			"readListRemote performs the paginated read HTTP exchange and decodes the response array into config. Extracted from Read so the request/response logic is unit-testable without a tfsdk.Config.",
			wiredListDataSourceReadHelperDecl(ds, wiring.read, modelName, structName)
	}
	return wiredDataSourceReadBody(wiring.read, modelName),
		"readRemote performs the read HTTP exchange and decodes the response into config. Extracted from Read so the request/response logic is unit-testable without a tfsdk.Config.",
		wiredDataSourceReadHelperDecl(ds, wiring.read, modelName, structName)
}

// scaffoldDataSourceReadBody returns the honest scaffold Read body used when the
// data source's read mapping is not resolvable or the response is not a single
// object: it decodes the config, reports that Read is not wired, and still
// stores the config so the generated provider compiles into a runnable scaffold.
func scaffoldDataSourceReadBody(modelName string) []ast.Stmt {
	return []ast.Stmt{
		astgen.VarDecl("config", modelName, nil),
		astgen.AssignSingle(
			astgen.Ident("diags"),
			astgen.Call(
				astgen.Selector(astgen.Selector(astgen.Ident("req"), "Config"), "Get"),
				astgen.Ident("ctx"),
				astgen.UnaryPtr(astgen.Ident("config")),
			),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "Append"),
			astgen.Ellipsis(astgen.Ident("diags")),
		)),
		astgen.If(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "HasError")),
			astgen.Return(),
		),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit("Read is not wired to a remote API endpoint."),
		)),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "State"), "Set"),
			astgen.Ident("ctx"),
			astgen.UnaryPtr(astgen.Ident("config")),
		)),
	}
}

// dataSourceSchemaValues builds the []ast.Expr key/value elements for datasource/schema.Schema{...}.
func dataSourceSchemaValues(ds ir.DataSourceIR) []ast.Expr {
	elems := []ast.Expr{}
	if v := litOrOmit(ds.Description); v != nil {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", v))
	}

	// A data source whose source operation is deprecated carries a deprecation
	// message on the schema so practitioners see it in plan output (M-10).
	if v := litOrOmit(ds.DeprecationMessage); v != nil {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", v))
	}

	attrs := ds.Schema.Attributes
	blocks := ds.Schema.Blocks

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				datasourceAttributeExpr(attr),
			))
		}
		elems = append(elems, astgen.KeyValue("Attributes", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Attribute")),
			attrElems...,
		)))
	}

	if len(blocks) > 0 {
		blockElems := make([]ast.Expr, 0, len(blocks))
		for _, block := range blocks {
			blockElems = append(blockElems, astgen.KeyValueExpr(
				astgen.Lit(block.Name),
				datasourceBlockExpr(block, ""),
			))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Block")),
			blockElems...,
		)))
	}

	return elems
}

// datasourceAttributeExpr returns an ast expression for a datasource/schema Attribute.
// It panics if the IR attribute cannot be mapped to a supported data source schema
// attribute so that unsupported shapes fail closed instead of silently falling
// back to a string attribute.
func datasourceAttributeExpr(attr ir.AttributeIR) ast.Expr {
	return datasourceAttributeExprWithPath(attr, "")
}

// datasourceAttributeExprWithPath returns an ast expression for a datasource/schema
// Attribute, tracking the dotted parent path so that unsupported nested attributes
// can be reported with their full location.
func datasourceAttributeExprWithPath(attr ir.AttributeIR, parentPath string) ast.Expr {
	path := fullAttrPath(parentPath, attr.Name)
	expr := dataSourceFrameworkAttributeExpr(attr, path)
	if expr == nil {
		// A nested attribute that cannot be represented (e.g. a nested
		// collection) is dropped by the nested map builder; a top-level
		// attribute should never be nil because the framework expr falls back
		// to DynamicAttribute (G2).
		if parentPath == "" {
			panic(fmt.Sprintf("unsupported data source attribute %q: schema has no recognizable type or nested shape", path))
		}
		return nil
	}
	return expr
}

// dataSourceFrameworkAttributeExpr maps an IR attribute to a Terraform Plugin
// Framework datasource/schema attribute expression. attrPath is the dotted path to
// the current attribute and is propagated to nested attribute maps.
func dataSourceFrameworkAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	s := attr.Schema

	// Collection types.
	if s.Collection != nil {
		if expr := dataSourceCollectionAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Primitive types.
	if s.Type != "" {
		if expr := dataSourcePrimitiveAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework datasource schema has no first-class union attribute.
	// When a schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := datasourceAttributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", dataSourceNestedAttributesMapFromSchema(*merged, attrPath)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), datasourceAttributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := datasourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", dataSourceNestedAttributesMapFromSchema(s, attrPath)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
	}

	// Unrepresentable shapes (e.g. a nested collection such as a List of Map of
	// Dynamic) cannot map to a framework attribute. At the top level a
	// DynamicAttribute is valid and honest; nested inside a collection it would
	// be rejected by the framework, so the nested map builder drops it (G2).
	if strings.Contains(attrPath, ".") {
		return nil
	}
	return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), datasourceAttributeValues(attr, nil)...)
}

// dataSourceCollectionAttributeExpr maps a collection-typed attribute to its
// framework attribute, or nil when the shape falls through to the
// primitive/union/unrepresentable handling below (G12).
func dataSourceCollectionAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	elem := schema.DynamicUnionElement(attr.Schema.Collection.ElementType)
	// A collection whose element is dynamic/null cannot be represented as a
	// framework collection (List{ElementType: DynamicType} is rejected by the
	// framework); treat it as an unrepresentable shape (G12).
	if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull {
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), datasourceAttributeValues(attr, nil)...)
	}
	// A collection whose element is an object (or nested collection) that
	// contains a dynamic at any depth cannot be rendered as a typed framework
	// collection: the terraform-plugin-framework rejects any collection whose
	// element type contains a dynamic (fwtype.ContainsCollectionWithDynamic).
	// Emit the whole collection as a DynamicAttribute, per the framework's own
	// guidance. This is valid in an object-or-top-level context; when this
	// collection is itself nested inside another collection's element, the
	// enclosing collection's ContainsNestedDynamic check has already promoted that
	// ancestor, so this emission is never reached inside a collection.
	if schema.ContainsNestedDynamic(elem) {
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), datasourceAttributeValues(attr, nil)...)
	}
	switch attr.Schema.Collection.Kind {
	case ir.List:
		return dataSourceListElementAttributeExpr(attr, elem, attrPath, "List")
	case ir.Set:
		return dataSourceListElementAttributeExpr(attr, elem, attrPath, "Set")
	case ir.Map:
		return dataSourceMapElementAttributeExpr(attr, elem, attrPath)
	}
	return nil
}

// dataSourceListElementAttributeExpr maps a List/Set element to its framework
// attribute (List*Attribute or Set*Attribute).
func dataSourceListElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, kind string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := datasourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"Attribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := datasourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", dataSourceNestedAttributesMapFromSchema(elem, attrPath)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"NestedAttribute"), d...)
	}
	return nil
}

// dataSourceMapElementAttributeExpr maps a Map element to its framework
// attribute (MapAttribute or MapNestedAttribute).
func dataSourceMapElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := datasourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := datasourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", dataSourceNestedAttributesMapFromSchema(elem, attrPath)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapNestedAttribute"), d...)
	}
	return nil
}

// dataSourcePrimitiveAttributeExpr maps a primitive-typed attribute to its
// framework attribute, or nil when the type is not a recognized primitive.
func dataSourcePrimitiveAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	switch attr.Schema.Type {
	case ir.TypeString:
		return astgen.CompositeLit(astgen.QualExpr("schema", "StringAttribute"), datasourceAttributeValues(attr, nil)...)
	case ir.TypeInt:
		return astgen.CompositeLit(astgen.QualExpr("schema", "Int64Attribute"), datasourceAttributeValues(attr, nil)...)
	case ir.TypeFloat:
		return astgen.CompositeLit(astgen.QualExpr("schema", "Float64Attribute"), datasourceAttributeValues(attr, nil)...)
	case ir.TypeBool:
		return astgen.CompositeLit(astgen.QualExpr("schema", "BoolAttribute"), datasourceAttributeValues(attr, nil)...)
	case ir.TypeDynamic, ir.TypeNull:
		// A Dynamic/Null attribute is only valid at the top level; nested inside a
		// collection it is rejected by the framework, so the nested map builder
		// drops it (G12, M-13).
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), datasourceAttributeValues(attr, nil)...)
	}
	return nil
}

// datasourceBlockExpr returns an ast expression for a datasource/schema Block.
// parentPath is the dotted path of the enclosing attribute and is propagated to
// the block's nested attribute maps so panics can report the full dotted location.
func datasourceBlockExpr(block ir.BlockIR, parentPath string) ast.Expr {
	var kind string
	path := fullAttrPath(parentPath, block.Name)
	attrs := dataSourceNestedAttributesMap(block.Schema, path)

	var elems []ast.Expr
	switch block.NestingMode {
	case ir.NestingList:
		kind = "ListNestedBlock"
		elems = append(elems, astgen.KeyValue("NestedObject", astgen.CompositeLit(
			astgen.QualExpr("schema", "NestedBlockObject"),
			astgen.KeyValue("Attributes", attrs),
		)))
		if exprs := blockSizeValidatorExprs(block, "List", "listvalidator"); len(exprs) > 0 {
			elems = append(elems, astgen.KeyValueExpr(
				astgen.Ident("Validators"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("validator", "List")), exprs...),
			))
		}
	case ir.NestingSet:
		kind = "SetNestedBlock"
		elems = append(elems, astgen.KeyValue("NestedObject", astgen.CompositeLit(
			astgen.QualExpr("schema", "NestedBlockObject"),
			astgen.KeyValue("Attributes", attrs),
		)))
		if exprs := blockSizeValidatorExprs(block, "Set", "setvalidator"); len(exprs) > 0 {
			elems = append(elems, astgen.KeyValueExpr(
				astgen.Ident("Validators"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("validator", "Set")), exprs...),
			))
		}
	case ir.NestingSingle:
		kind = "SingleNestedBlock"
		elems = append(elems, astgen.KeyValue("Attributes", attrs))
	default:
		panic(fmt.Errorf("%w: %q for block %q", ErrUnknownDatasourceBlockNesting, block.NestingMode, block.Name))
	}

	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	}

	return astgen.CompositeLit(astgen.QualExpr("schema", kind), elems...)
}

// datasourceAttributeValues builds the common field dictionary for a data source
// schema attribute. Data source attributes default to Computed when no
// Required/Optional/Computed flag is set.
func datasourceAttributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
	// Resolve a Required+Computed conflict before emitting flags so the render
	// never produces a framework-invalid schema (N-25).
	attr = normalizeAttributeFlags(attr)

	elems := []ast.Expr{}

	if attr.MarkdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.MarkdownDescription)))
	} else if attr.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.Description)))
	}

	hasComputed := attr.Computed
	hasRequired := attr.Required
	hasOptional := attr.Optional

	if hasComputed {
		elems = append(elems, astgen.KeyValue("Computed", astgen.BoolLit(true)))
	}
	if hasRequired {
		elems = append(elems, astgen.KeyValue("Required", astgen.BoolLit(true)))
	}
	if hasOptional {
		elems = append(elems, astgen.KeyValue("Optional", astgen.BoolLit(true)))
	}
	if !hasComputed && !hasRequired && !hasOptional {
		elems = append(elems, astgen.KeyValue("Computed", astgen.BoolLit(true)))
	}

	if attr.Sensitive {
		elems = append(elems, astgen.KeyValue("Sensitive", astgen.BoolLit(true)))
	}
	if attr.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(attr.DeprecationMessage)))
	} else if attr.Deprecated {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit("Deprecated")))
	}

	return append(elems, extra...)
}

// dataSourceNestedAttributesMap returns map[string]schema.Attribute{...} for the given
// object schema. parentPath is the dotted path of the enclosing attribute or block.
func dataSourceNestedAttributesMap(s ir.ObjectSchemaIR, parentPath string) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		expr := datasourceAttributeExprWithPath(attr, parentPath)
		if expr == nil {
			// Nested attribute is unrepresentable (e.g. a nested collection);
			// drop it from the schema rather than emitting a framework-invalid
			// DynamicAttribute inside a collection (G2).
			continue
		}
		elems = append(elems, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			expr,
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Attribute")),
		elems...,
	)
}

// dataSourceNestedAttributesMapFromSchema converts a SchemaIR object-like value
// to a nested attributes map expression for data source schemas. parentPath is the
// dotted path of the enclosing attribute and is propagated to nested attribute panics.
//
// Blocks nested inside object-typed attributes are dropped: the Terraform
// plugin-framework NestedAttributeObject type only supports Attributes, not
// Blocks (M-14). This is a known limitation; see PROJECT_DESIGN.md and
// CLAUDE.md "Current limitations". A spec that declares blocks under an object
// attribute will not round-trip those blocks into the generated data source.
func dataSourceNestedAttributesMapFromSchema(s ir.SchemaIR, parentPath string) ast.Expr {
	return dataSourceNestedAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes}, parentPath)
}
