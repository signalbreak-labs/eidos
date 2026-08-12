package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// listResourceSetFallbackComment is the warning emitted when a Set collection
// is downgraded to a List attribute because list/schema does not support Set
// types.
const listResourceSetFallbackComment = "Set collections are not supported by list/schema; emitted as a List attribute."

// listResourceSetBlockFallbackComment is the warning emitted when a Set-nested
// block is downgraded to a ListNestedBlock because list/schema does not support
// SetNestedBlock.
const listResourceSetBlockFallbackComment = "Set-nested blocks are not supported by list/schema; emitted as a ListNestedBlock."

// ListResourceFile returns the generated internal/provider/list_<name>.go file
// for a Terraform plugin-framework list resource built from the supplied
// ListResourceIR. clientImport is the import path of the generated
// internal/client package; when it is non-empty and the list mapping resolves
// (planListResourceWiring), the generated List method streams real instances
// from the API instead of the honest scaffold diagnostic.
func ListResourceFile(lr ir.ListResourceIR, clientImport string) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("list_%s.go", naming.SnakeCase(lr.Name)))
	file, err := func() (f *ast.File, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				if recErr, ok := rec.(error); ok {
					err = fmt.Errorf("renderer panic: %w", recErr)
				} else {
					err = fmt.Errorf("renderer panic: %v", rec)
				}
			}
		}()
		f = generateListResourceFile(lr, clientImport)
		return
	}()
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ListResourceFiles returns the generated list resource files for every
// ListResourceIR in the provider. Files are emitted in the order the resources
// are supplied. clientImport is the import path of the generated
// internal/client package (see ListResourceFile).
func ListResourceFiles(listResources []ir.ListResourceIR, clientImport string) []File {
	files := make([]File, 0, len(listResources))
	for _, lr := range listResources {
		files = append(files, ListResourceFile(lr, clientImport))
	}
	return files
}

// listResourceTypeName returns the Terraform list resource type name. It
// prefers ListResourceIR.TypeName and falls back to a snake_cased
// ListResourceIR.Name (via typeNameFallback) so a camelCase operation-derived
// name produces a valid Terraform type name rather than failing framework
// validation (M-19).
func listResourceTypeName(lr ir.ListResourceIR) string {
	if strings.TrimSpace(lr.TypeName) != "" {
		return strings.TrimSpace(lr.TypeName)
	}
	return typeNameFallback(lr.Name)
}

// listResourceTypeDecls emits the compile-time interface assertions, the list
// resource struct (with the API client field when wired), and — for a wired
// list resource whose config schema declares filter attributes — the config
// model struct the wired List body decodes req.Config into.
func listResourceTypeDecls(f *astgen.File, lr ir.ListResourceIR, wiring listResourceWiringPlan, structName, modelName, typeName string, wired bool) {
	// Compile-time interface assertion.
	f.AddComment("Compile-time interface assertion.")
	f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
		"_",
		astgen.QualExpr("list", "ListResource"),
		astgen.Call(
			astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
			astgen.Nil(),
		),
	)))
	if wired {
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("list", "ListResourceWithConfigure"),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)))
	}

	// List resource struct. Wired list resources store the API client handed
	// over by the provider's Configure method.
	f.AddCommentf("%s is the generated Terraform list resource implementation.", structName)
	if wired {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
			astgen.Field("client", astgen.StarExpr(astgen.QualExpr("client", "Client")), ""),
		)))
	} else {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType()))
	}

	// Config model. Scaffolds and attribute-free list resources need no model.
	if wired && wiring.hasConfigModel {
		f.AddCommentf("%s describes the %s list filter configuration shape.", modelName, typeName)
		modelFields := make([]*ast.Field, 0, len(lr.ConfigSchema.Attributes))
		for _, attr := range lr.ConfigSchema.Attributes {
			modelFields = append(modelFields, astgen.Field(
				naming.GoFieldName(attr.Name),
				modelFieldType(attr),
				modelFieldTags(attr),
			))
		}
		f.AddDecl(astgen.TypeDecl(modelName, astgen.StructType(modelFields...)))
	}
}

// listResourceNeedsTypesImport reports whether the list resource's config
// schema renders any expression referencing the terraform-plugin-framework
// types package. List resources have no model struct (unlike resources and
// data sources, whose model fields unconditionally reference types), so
// types.* is referenced only by primitiveAttrType, which
// listResourceFrameworkAttributeExpr calls for a collection (List/Set/Map)
// attribute whose element type is primitive. A list resource whose schema
// contains only primitive attributes, object-nested attributes, or blocks
// therefore does not reference types and must not import it. This mirrors the
// recursive objectSchemaNeedsValidators / schemaIRNeedsValidators pattern so
// the gate matches the exact render decision, including collection elements
// and nested object attributes.
func listResourceNeedsTypesImport(lr ir.ListResourceIR) bool {
	for _, attr := range lr.ConfigSchema.Attributes {
		if schemaReferencesTypes(attr.Schema) {
			return true
		}
	}
	for _, block := range lr.ConfigSchema.Blocks {
		if objectSchemaReferencesTypes(block.Schema) {
			return true
		}
	}
	return false
}

// schemaReferencesTypes reports whether rendering the schema as a list
// resource framework attribute emits a types.* reference. The only such
// reference is primitiveAttrType, called for a collection whose element type
// is primitive; object-like collection elements and object-like schemas
// recurse into their nested attributes, which may themselves contain such a
// collection. Primitive, union, and other non-collection schemas never
// reference types when rendered as list resource attributes.
func schemaReferencesTypes(s ir.SchemaIR) bool {
	if s.Collection != nil {
		elem := schema.DynamicUnionElement(s.Collection.ElementType)
		if schema.IsPrimitiveSchema(elem) {
			return true
		}
		if schema.IsObjectLike(elem) {
			return objectSchemaReferencesTypes(ir.ObjectSchemaIR{
				Attributes: elem.Attributes,
				Blocks:     elem.Blocks,
			})
		}
		return false
	}
	if schema.IsObjectLike(s) {
		return objectSchemaReferencesTypes(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})
	}
	return false
}

// objectSchemaReferencesTypes reports whether any attribute in the object
// schema, when rendered as list resource framework attributes, emits a
// types.* reference (see schemaReferencesTypes). Nested blocks are not
// rendered below the top level — listResourceBlockExpr and
// NestedAttributeObject expose only Attributes, never Blocks — so this walks
// Attributes only; recursing into unrendered nested blocks would over-report
// and import types unused, breaking compilation.
func objectSchemaReferencesTypes(s ir.ObjectSchemaIR) bool {
	for _, attr := range s.Attributes {
		if schemaReferencesTypes(attr.Schema) {
			return true
		}
	}
	return false
}

// generateListResourceFile builds the *ast.File for internal/provider/list_<name>.go.
// clientImport is the import path of the generated internal/client package;
// when non-empty and the list mapping resolves, the generated struct stores
// the API client and the List method streams real instances (F1).
func generateListResourceFile(lr ir.ListResourceIR, clientImport string) *ast.File {
	f := astgen.NewFile("provider")

	structName := listResourceStructName(lr)
	typeName := listResourceTypeName(lr)
	modelName := structName + "Model"

	wiring := planListResourceWiring(lr)
	wired := clientImport != "" && wiring.wired

	listResourceTypeDecls(f, lr, wiring, structName, modelName, typeName, wired)

	// New constructor.
	f.AddCommentf("New%s returns a new instance of the generated list resource.", structName)
	f.AddDecl(astgen.FuncDeclFull(
		"New"+structName,
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("list", "ListResource"), "")),
		astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName))))),
	))

	// Metadata method.
	f.AddComment("Metadata returns the list resource type name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "l", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("resource", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(typeName)},
			token.ASSIGN,
		)),
	))

	// Emit diagnostic comments for Set fallbacks before the config schema method.
	var fallbackComments []string
	for _, attr := range lr.ConfigSchema.Attributes {
		if isSetFallbackAttribute(attr) {
			fallbackComments = append(fallbackComments, listResourceSetFallbackComment)
		}
	}
	for _, block := range lr.ConfigSchema.Blocks {
		if block.NestingMode == ir.NestingSet {
			fallbackComments = append(fallbackComments, listResourceSetBlockFallbackComment)
		}
	}
	if len(fallbackComments) > 0 {
		f.AddComment(fallbackComments...)
	}

	// ListResourceConfigSchema method.
	f.AddComment("ListResourceConfigSchema returns the list resource config schema.")
	schemaValues := listResourceSchemaValues(lr)
	f.AddDecl(astgen.MethodDecl(
		"ListResourceConfigSchema", "l", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("list", "ListResourceSchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("list", "ListResourceSchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("listschema", "Schema"), schemaValues...)},
			token.ASSIGN,
		)),
	))

	// List method. Wired list resources stream real instances fetched through
	// the generated client; list resources without a resolvable list mapping
	// keep the honest scaffold body.
	f.AddComment("List streams matching resource instances for terraform query.")
	f.AddDecl(listMethodDecl(lr, wiring, structName, modelName, wired))

	// Configure method. Wired list resources implement ListResourceWithConfigure
	// to receive the API client constructed by the provider's Configure method.
	if wired {
		f.AddComment("Configure stores the API client supplied by the provider.")
		f.AddDecl(listResourceConfigureDecl(structName))
	}

	f.AddImport("context", "")
	if !wired {
		// Only the scaffold List body references diag (the fatal error
		// diagnostic); the wired body reports through ListResult.Diagnostics.
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/diag", "diag")
	}
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/list", "list")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/list/schema", "listschema")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource", "resource")
	if wired {
		// Wired List bodies fetch pages through the generated client
		// (client.ListAllPages), decode items with encoding/json, and convert
		// them into the identity/resource types via tftypes.ValueFromJSON.
		f.AddImport(clientImport, "client")
		f.AddImports("encoding/json", "fmt", "net/http", "net/url")
		f.AddImport("github.com/hashicorp/terraform-plugin-go/tftypes", "tftypes")
		if wiring.needsStrings {
			f.AddImport("strings", "")
		}
		if wiring.needsStrconv {
			f.AddImport("strconv", "")
		}
	}
	// types.* is referenced by primitiveAttrType for a collection (List/Set/Map)
	// attribute whose element type is primitive (see
	// listResourceNeedsTypesImport), and — for a wired list resource with a
	// config model — by every model field. The gate mirrors the exact render
	// decision so the import is never unused (the §6 latent unused-import bug).
	if listResourceNeedsTypesImport(lr) || (wired && wiring.hasConfigModel) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	// schema/validator is referenced by block size validators
	// ([]validator.List/Set) and by a discriminated union's
	// DiscriminatorValidator ([]validator.Object, D2); listvalidator only by
	// the former. Gate each import on its own condition.
	needsBlockValidators := false
	for _, block := range lr.ConfigSchema.Blocks {
		if block.NestingMode == ir.NestingList || block.NestingMode == ir.NestingSet {
			if block.MinItems != nil || block.MaxItems != nil {
				needsBlockValidators = true
				break
			}
		}
	}
	if needsBlockValidators || schema.ObjectSchemaHasDiscriminatedUnion(lr.ConfigSchema) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	}
	if needsBlockValidators {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/listvalidator", "listvalidator")
	}

	return f.AST()
}

// isSetFallbackAttribute reports whether the attribute's schema is a Set
// collection that must be emitted as a List attribute. The warning comment is
// added by the caller (generateListResourceFile) so it appears near the config
// schema method rather than inside the map literal.
func isSetFallbackAttribute(attr ir.AttributeIR) bool {
	return attr.Schema.Collection != nil && attr.Schema.Collection.Kind == ir.Set
}

// listMethodDecl returns the List method declaration for a list resource: the
// wired streaming body when the list mapping resolved, otherwise the honest
// scaffold body surfacing a fatal error diagnostic through the results stream
// rather than silently returning zero results (M-18).
func listMethodDecl(lr ir.ListResourceIR, wiring listResourceWiringPlan, structName, modelName string, wired bool) *ast.FuncDecl {
	if wired {
		return astgen.MethodDecl(
			"List", "l", astgen.StarExpr(astgen.Ident(structName)),
			astgen.Params(
				astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("req", astgen.QualExpr("list", "ListRequest"), ""),
				astgen.Field("stream", astgen.StarExpr(astgen.QualExpr("list", "ListResultsStream")), ""),
			),
			astgen.Results(),
			astgen.Block(wiredListBody(lr, wiring, modelName)...),
		)
	}
	return astgen.MethodDecl(
		"List", "l", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("list", "ListRequest"), ""),
			astgen.Field("stream", astgen.StarExpr(astgen.QualExpr("list", "ListResultsStream")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("stream"), "Results")},
			[]ast.Expr{astgen.Call(
				astgen.QualExpr("list", "ListResultsStreamDiagnostics"),
				astgen.CompositeLit(
					astgen.QualExpr("diag", "Diagnostics"),
					astgen.Call(
						astgen.QualExpr("diag", "NewErrorDiagnostic"),
						astgen.Lit("Generated provider scaffold"),
						astgen.Lit("List is not wired to a remote API endpoint."),
					),
				),
			)},
			token.ASSIGN,
		)),
	)
}

// listResourceSchemaValues builds the []ast.Expr key/value elements for listschema.Schema{...}.
func listResourceSchemaValues(lr ir.ListResourceIR) []ast.Expr {
	elems := []ast.Expr{}
	if v := litOrOmit(lr.Description); v != nil {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", v))
	}

	attrs := lr.ConfigSchema.Attributes
	blocks := lr.ConfigSchema.Blocks

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				listResourceAttributeExpr(attr, lr.Name),
			))
		}
		elems = append(elems, astgen.KeyValue("Attributes", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("listschema", "Attribute")),
			attrElems...,
		)))
	}

	if len(blocks) > 0 {
		blockElems := make([]ast.Expr, 0, len(blocks))
		for _, block := range blocks {
			blockElems = append(blockElems, astgen.KeyValueExpr(
				astgen.Lit(block.Name),
				listResourceBlockExpr(block, lr.Name),
			))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("listschema", "Block")),
			blockElems...,
		)))
	}

	return elems
}

// listResourceAttributeExpr returns an ast expression for a list/schema
// Attribute. The list schema package does not support Set types, so Set
// collections are emitted as List attributes as a best-effort fallback.
// resourceName is included in panic messages for unsupported attributes so
// operators can locate the offending resource.
func listResourceAttributeExpr(attr ir.AttributeIR, resourceName string) ast.Expr {
	return listResourceAttributeExprWithPath(attr, "", resourceName)
}

// listResourceAttributeExprWithPath returns an ast expression for a list/schema
// Attribute, tracking the dotted parent path so that unsupported nested attributes
// can be reported with their full location. resourceName is included in the panic
// message alongside the path.
func listResourceAttributeExprWithPath(attr ir.AttributeIR, parentPath, resourceName string) ast.Expr {
	path := fullAttrPath(parentPath, attr.Name)
	expr := listResourceFrameworkAttributeExpr(attr, path, resourceName)
	if expr == nil {
		// A nested attribute that cannot be represented (e.g. a nested
		// collection) is dropped by the nested map builder; a top-level
		// attribute should never be nil because the framework expr falls back
		// to DynamicAttribute (G2).
		if parentPath == "" {
			panic(fmt.Sprintf("list resource %q attribute %q: schema has no recognizable type or nested shape", resourceName, path))
		}
		return nil
	}
	return expr
}

// listResourceFrameworkAttributeExpr maps an IR attribute to a Terraform Plugin
// Framework list/schema attribute expression. attrPath is the dotted path to the
// current attribute and is propagated to nested attribute maps. resourceName is
// included in panic messages so unsupported attributes can be traced back to the
// owning list resource. This helper intentionally accepts resourceName while the
// resource and datasource attribute helpers do not, because list (and ephemeral)
// resource panic messages include the resource name.
func listResourceFrameworkAttributeExpr(attr ir.AttributeIR, attrPath, resourceName string) ast.Expr {
	s := attr.Schema

	// Collection types. Set is not supported by list/schema; fallback to List.
	if s.Collection != nil {
		if expr := listResourceCollectionAttributeExpr(attr, attrPath, resourceName); expr != nil {
			return expr
		}
	}

	// Primitive types.
	if s.Type != "" {
		if expr := listResourcePrimitiveAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework list resource schema has no first-class union attribute.
	// When a schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := listResourceAttributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", listResourceNestedAttributesMapFromSchema(*merged, attrPath, resourceName)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("listschema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("listschema", "DynamicAttribute"), listResourceAttributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := listResourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", listResourceNestedAttributesMapFromSchema(s, attrPath, resourceName)),
		})
		return astgen.CompositeLit(astgen.QualExpr("listschema", "SingleNestedAttribute"), d...)
	}

	// Unrepresentable shapes (e.g. a nested collection) cannot map to a
	// framework attribute. At the top level a DynamicAttribute is valid and
	// honest; nested inside a collection it would be rejected by the
	// framework, so the nested map builder drops it (G2).
	if strings.Contains(attrPath, ".") {
		return nil
	}
	return astgen.CompositeLit(astgen.QualExpr("listschema", "DynamicAttribute"), listResourceAttributeValues(attr, nil)...)
}

// listResourceCollectionAttributeExpr maps a collection-typed attribute to its
// framework attribute, or nil when the shape falls through to the
// primitive/union/unrepresentable handling below (G12). Set is not supported by
// list/schema, so it falls back to List.
func listResourceCollectionAttributeExpr(attr ir.AttributeIR, attrPath, resourceName string) ast.Expr {
	elem := schema.DynamicUnionElement(attr.Schema.Collection.ElementType)
	// A collection whose element is dynamic/null cannot be represented as a
	// framework collection (List{ElementType: DynamicType} is rejected by
	// the framework); treat it as an unrepresentable shape (G12).
	if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull {
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("listschema", "DynamicAttribute"), listResourceAttributeValues(attr, nil)...)
	}
	// A collection whose element is an object (or nested collection) that
	// contains a dynamic at any depth cannot be rendered as a typed framework
	// collection: the framework rejects any collection whose element type
	// contains a dynamic (fwtype.ContainsCollectionWithDynamic). Emit the whole
	// collection as a DynamicAttribute, per the framework's own guidance. This is
	// valid in an object-or-top-level context; an enclosing collection's
	// ContainsNestedDynamic check promotes any collection ancestor, so this is
	// never reached inside a collection.
	if schema.ContainsNestedDynamic(elem) {
		return astgen.CompositeLit(astgen.QualExpr("listschema", "DynamicAttribute"), listResourceAttributeValues(attr, nil)...)
	}
	switch attr.Schema.Collection.Kind {
	case ir.List, ir.Set:
		return listResourceListElementAttributeExpr(attr, elem, attrPath, resourceName)
	case ir.Map:
		return listResourceMapElementAttributeExpr(attr, elem, attrPath, resourceName)
	}
	return nil
}

// listResourceListElementAttributeExpr maps a List/Set element to its framework
// attribute (ListAttribute or ListNestedAttribute; Set falls back to List).
func listResourceListElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, resourceName string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := listResourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("listschema", "ListAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := listResourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("listschema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", listResourceNestedAttributesMapFromSchema(elem, attrPath, resourceName)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("listschema", "ListNestedAttribute"), d...)
	}
	return nil
}

// listResourceMapElementAttributeExpr maps a Map element to its framework
// attribute (MapAttribute or MapNestedAttribute).
func listResourceMapElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, resourceName string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := listResourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("listschema", "MapAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := listResourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("listschema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", listResourceNestedAttributesMapFromSchema(elem, attrPath, resourceName)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("listschema", "MapNestedAttribute"), d...)
	}
	return nil
}

// listResourcePrimitiveAttributeExpr maps a primitive-typed attribute to its
// framework attribute, or nil when the type is not a recognized primitive.
func listResourcePrimitiveAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	switch attr.Schema.Type {
	case ir.TypeString:
		return astgen.CompositeLit(astgen.QualExpr("listschema", "StringAttribute"), listResourceAttributeValues(attr, nil)...)
	case ir.TypeInt:
		return astgen.CompositeLit(astgen.QualExpr("listschema", "Int64Attribute"), listResourceAttributeValues(attr, nil)...)
	case ir.TypeFloat:
		return astgen.CompositeLit(astgen.QualExpr("listschema", "Float64Attribute"), listResourceAttributeValues(attr, nil)...)
	case ir.TypeBool:
		return astgen.CompositeLit(astgen.QualExpr("listschema", "BoolAttribute"), listResourceAttributeValues(attr, nil)...)
	case ir.TypeDynamic:
		// A DynamicAttribute is only valid at the top level; nested inside a
		// collection it is rejected by the framework, so the nested map
		// builder drops it (G12).
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("listschema", "DynamicAttribute"), listResourceAttributeValues(attr, nil)...)
	}
	return nil
}

// listResourceBlockExpr returns an ast expression for a list/schema Block.
// Set-nested blocks are not supported by list/schema, so they fall back to
// ListNestedBlock. resourceName is included in panic messages for unsupported
// nested attributes. Blocks are always top-level in list resource schemas, so
// block.Name is used directly as the parent path for the block's nested
// attributes.
func listResourceBlockExpr(block ir.BlockIR, resourceName string) ast.Expr {
	// block.Name is both the Terraform block name and the parent path for nested
	// attributes (the doc comment above explains why blocks are always top-level
	// for list resources; L-46 removed the duplicated inline restatement).
	pathForNesting := block.Name
	elems := []ast.Expr{
		astgen.KeyValue("Attributes", listResourceNestedAttributesMap(block.Schema, pathForNesting, resourceName)),
	}

	var kind string
	switch block.NestingMode {
	case ir.NestingList, ir.NestingSet:
		kind = "ListNestedBlock"
		if exprs := listResourceBlockSizeValidatorExprs(block); len(exprs) > 0 {
			elems = append(elems, astgen.KeyValueExpr(
				astgen.Ident("Validators"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("validator", "List")), exprs...),
			))
		}
	default:
		kind = "SingleNestedBlock"
	}

	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	} else if block.Deprecated {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit("Deprecated")))
	}

	return astgen.CompositeLit(astgen.QualExpr("listschema", kind), elems...)
}

// listResourceBlockSizeValidatorExprs returns the plugin-framework validator
// expressions for a list resource block's MinItems/MaxItems cardinality
// constraints. The list/schema package only supports ListNestedBlock, so the
// validators always target the List validator kind. SingleNestedBlock does not
// support cardinality validators, so nil is returned for single-nested blocks.
func listResourceBlockSizeValidatorExprs(block ir.BlockIR) []ast.Expr {
	if block.NestingMode == ir.NestingSingle {
		return nil
	}
	if block.MinItems == nil && block.MaxItems == nil {
		return nil
	}
	var exprs []ast.Expr
	if block.MinItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr("listvalidator", "SizeAtLeast"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MinItems))),
		))
	}
	if block.MaxItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr("listvalidator", "SizeAtMost"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MaxItems))),
		))
	}
	return exprs
}

// listResourceAttributeValues builds the common field dictionary for a list
// resource schema attribute. List schema attributes only support Required and
// Optional; they do not support Computed, Sensitive, or WriteOnly. When no
// optionality flag is set, the attribute defaults to Optional.
func listResourceAttributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
	elems := []ast.Expr{}

	if attr.MarkdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.MarkdownDescription)))
	} else if attr.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.Description)))
	}

	if attr.Required {
		elems = append(elems, astgen.KeyValue("Required", astgen.BoolLit(true)))
	} else {
		elems = append(elems, astgen.KeyValue("Optional", astgen.BoolLit(true)))
	}

	if attr.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(attr.DeprecationMessage)))
	} else if attr.Deprecated {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit("Deprecated")))
	}

	return append(elems, extra...)
}

// listResourceNestedAttributesMap returns map[string]schema.Attribute{...} for
// the given object schema. parentPath is the dotted path of the enclosing
// attribute or block. It intentionally iterates only Attributes; the Terraform
// plugin-framework NestedAttributeObject type does not support Blocks, so any
// Blocks present in the ObjectSchemaIR are ignored. resourceName is included
// in panic messages for unsupported nested attributes.
func listResourceNestedAttributesMap(s ir.ObjectSchemaIR, parentPath, resourceName string) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		expr := listResourceAttributeExprWithPath(attr, parentPath, resourceName)
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
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("listschema", "Attribute")),
		elems...,
	)
}

// listResourceNestedAttributesMapFromSchema converts a SchemaIR object-like value
// to a nested attributes map expression for list resource schemas. parentPath is the
// dotted path of the enclosing attribute and is propagated to nested attribute
// panics. resourceName is included in panic messages so unsupported nested
// attributes can be traced back to the owning list resource.
func listResourceNestedAttributesMapFromSchema(s ir.SchemaIR, parentPath, resourceName string) ast.Expr {
	return listResourceNestedAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, parentPath, resourceName)
}
