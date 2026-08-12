package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// EphemeralFile returns the generated internal/provider/ephemeral_<name>.go file
// for a Terraform plugin-framework ephemeral resource built from the supplied
// EphemeralResourceIR. clientImport is the import path of the generated
// internal/client package, used when the Open body is wired to the API client.
func EphemeralFile(er ir.EphemeralResourceIR, clientImport string) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("ephemeral_%s.go", naming.SnakeCase(er.Name)))
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
		f = generateEphemeralFile(er, clientImport)
		return
	}()
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// EphemeralFiles returns the generated ephemeral resource files for every
// EphemeralResourceIR in the provider. clientImport is the import path of the
// generated internal/client package. Files are emitted in the order the
// ephemeral resources are supplied.
func EphemeralFiles(ers []ir.EphemeralResourceIR, clientImport string) []File {
	files := make([]File, 0, len(ers))
	for _, er := range ers {
		files = append(files, EphemeralFile(er, clientImport))
	}
	return files
}

// generateEphemeralFile builds the *ast.File for internal/provider/ephemeral_<name>.go.
// clientImport is the import path of the generated internal/client package,
// used when the ephemeral resource's Open body is wired to the API client.
func generateEphemeralFile(er ir.EphemeralResourceIR, clientImport string) *ast.File {
	f := astgen.NewFile("provider")

	structName := ephemeralResourceStructName(er)
	modelName := ephemeralResourceModelName(er)
	typeName := ephemeralResourceTypeName(er)
	wiring := planEphemeralWiring(er)

	// Interface assertions. Each assertion is emitted as its own single-spec
	// var declaration so go/format renders it inline (var _ ... = ...), which
	// keeps the generated output stable and diffable.
	f.AddComment("Compile-time interface assertion.")
	f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
		"_",
		astgen.QualExpr("ephemeral", "EphemeralResource"),
		astgen.Call(
			astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
			astgen.Nil(),
		),
	)))
	if wiring.wired {
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("ephemeral", "EphemeralResourceWithConfigure"),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)))
	}
	if er.HasRenew {
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("ephemeral", "EphemeralResourceWithRenew"),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)))
	}
	if er.HasClose {
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("ephemeral", "EphemeralResourceWithClose"),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)))
	}

	// Ephemeral resource struct. Wired ephemeral resources carry the API client
	// supplied by the provider's Configure method via the framework
	// ephemeral-resource-data mechanism.
	f.AddCommentf("%s is the generated Terraform ephemeral resource implementation.", structName)
	if wiring.wired {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
			astgen.Field("client", astgen.StarExpr(astgen.QualExpr("client", "Client")), ""),
		)))
	} else {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType()))
	}

	// Model struct.
	f.AddCommentf("%s describes the ephemeral resource config and result shape.", modelName)
	modelFields := []*ast.Field{}
	// ephemeralModelAttributes already deduplicates by name, so no seen map is
	// needed here. The prior seen map was redundant and, worse, marked a name
	// seen before skipAttrForModel was applied, which could suppress a later
	// same-name field (L-36).
	for _, attr := range ephemeralModelAttributes(er) {
		if schema.SkipAttrForModel(attr) {
			continue
		}
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(attr.Name),
			modelFieldType(attr),
			modelFieldTags(attr),
		))
	}
	f.AddDecl(astgen.TypeDecl(modelName, astgen.StructType(modelFields...)))

	// New constructor.
	f.AddCommentf("New%s returns a new instance of the generated ephemeral resource.", structName)
	f.AddDecl(astgen.FuncDeclFull(
		"New"+structName,
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("ephemeral", "EphemeralResource"), "")),
		astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName))))),
	))

	// Metadata method.
	f.AddComment("Metadata returns the ephemeral resource type name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "e", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("ephemeral", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("ephemeral", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(typeName)},
			token.ASSIGN,
		)),
	))

	// Schema method.
	f.AddComment("Schema returns the ephemeral resource schema.")
	f.AddDecl(astgen.MethodDecl(
		"Schema", "e", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("ephemeral", "SchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("ephemeral", "SchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "Schema"), ephemeralSchemaValues(er)...)},
			token.ASSIGN,
		)),
	))

	// Open method. Wired ephemeral resources call the open endpoint and store the
	// API response as the ephemeral result; ephemeral resources without a
	// resolvable bodiless open mapping keep the honest scaffold Open body.
	f.AddComment("Open generates a new ephemeral resource value.")
	openBody := scaffoldEphemeralOpenBody(modelName)
	if wiring.wired {
		openBody = wiredEphemeralOpenBody(er, wiring, modelName)
	}
	f.AddDecl(astgen.MethodDecl(
		"Open", "e", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("ephemeral", "OpenRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("ephemeral", "OpenResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(openBody...),
	))

	// Renew method. Wired when the open is wired and the renew mapping resolves
	// (bodiless, parameters resolvable): the body reads the parameter values
	// Open stashed in ephemeral private state and calls the renew endpoint.
	if er.HasRenew {
		f.AddComment("Renew extends the lifetime of the ephemeral resource.")
		f.AddDecl(ephemeralLifecycleMethodDecl(er, wiring, structName, "Renew", "renewing"))
	}

	// Close method. Wired on the same terms as Renew: the body reads the
	// parameter values Open stashed in ephemeral private state and calls the
	// close/revoke endpoint.
	if er.HasClose {
		f.AddComment("Close cleans up the ephemeral resource.")
		f.AddDecl(ephemeralLifecycleMethodDecl(er, wiring, structName, "Close", "closing"))
	}

	// Configure method. Wired ephemeral resources implement
	// EphemeralResourceWithConfigure to receive the API client constructed by
	// the provider's Configure method.
	if wiring.wired {
		f.AddComment("Configure stores the API client supplied by the provider.")
		f.AddDecl(ephemeralConfigureDecl(structName))
	}

	f.AddImport("context", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/ephemeral", "ephemeral")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/ephemeral/schema", "ephemeralschema")
	// The model struct references types.* for every attribute field. An
	// ephemeral resource whose config and result schemas are both empty
	// produces an empty model and must not import types, or the import is
	// unused and the generated provider does not compile (the §6 latent
	// unused-import bug). Blocks are schema-only and do not contribute model
	// fields, so only attributes gate the import.
	if len(modelFields) > 0 {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	if wiring.wired {
		// Wired Open bodies build and send HTTP requests through the generated
		// client and decode JSON responses. Ephemeral opens are bodiless, so
		// bytes is not imported.
		f.AddImport(clientImport, "client")
		f.AddImports("encoding/json", "fmt", "io", "net/http")
		if wiring.needsStrings {
			f.AddImport("strings", "")
		}
		if wiring.needsStrconv {
			f.AddImport("strconv", "")
		}
		if wiring.needsURL {
			f.AddImport("net/url", "")
		}
	}
	// The schema/validator package is only referenced by ephemeralBlockExpr when a
	// List/Set block emits a validator.List/validator.Set composite, which in turn
	// only happens when MinItems/MaxItems is set. Register the import only then to
	// avoid "imported and not used" compile errors for plain blocks.
	if ephemeralNeedsValidatorImport(er) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	}
	for _, block := range ephemeralMergedBlocks(er) {
		if block.NestingMode == ir.NestingList && (block.MinItems != nil || block.MaxItems != nil) {
			f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/listvalidator", "listvalidator")
		}
		if block.NestingMode == ir.NestingSet && (block.MinItems != nil || block.MaxItems != nil) {
			f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/setvalidator", "setvalidator")
		}
	}

	return f.AST()
}

// ephemeralLifecycleMethodDecl returns the Renew or Close method declaration
// for an ephemeral resource: the wired body when the open is wired and the
// lifecycle mapping resolved, otherwise the honest scaffold body.
func ephemeralLifecycleMethodDecl(er ir.EphemeralResourceIR, wiring ephemeralWiringPlan, structName, method, kind string) *ast.FuncDecl {
	var plan *crudOperationPlan
	if method == "Renew" {
		plan = wiring.renew
	} else {
		plan = wiring.close
	}
	body := []ast.Stmt{
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Selector(astgen.Ident("resp"), "Diagnostics"), "AddError"),
			astgen.Lit("Generated provider scaffold"),
			astgen.Lit(method+" is not wired to a remote API endpoint."),
		)),
	}
	if wiring.wired && plan != nil {
		body = wiredEphemeralLifecycleBody(er, *plan, kind)
	}
	return astgen.MethodDecl(
		method, "e", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("ephemeral", method+"Request"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("ephemeral", method+"Response")), ""),
		),
		astgen.Results(),
		astgen.Block(body...),
	)
}

// ephemeralNeedsValidatorImport reports whether any merged block of the
// ephemeral resource emits a validator.List/validator.Set composite literal,
// or any attribute is a discriminated union emitting a DiscriminatorValidator
// ([]validator.Object, D2) — the only references to the schema/validator
// package in the generated ephemeral schema. A List/Set block emits a
// validator only when MinItems/MaxItems is set; SingleNestedBlock never does.
// This keeps the import gated to the exact condition under which it is used,
// avoiding "imported and not used" compile failures for ephemeral resources
// that contain plain blocks.
func ephemeralNeedsValidatorImport(er ir.EphemeralResourceIR) bool {
	for _, block := range ephemeralMergedBlocks(er) {
		if block.NestingMode == ir.NestingSingle {
			continue
		}
		if block.MinItems != nil || block.MaxItems != nil {
			return true
		}
	}
	return schema.ObjectSchemaHasDiscriminatedUnion(er.ConfigSchema) || schema.ObjectSchemaHasDiscriminatedUnion(er.ResultSchema)
}

// ephemeralResourceModelName returns the generated model struct name for an ephemeral resource.
func ephemeralResourceModelName(er ir.EphemeralResourceIR) string {
	return naming.PascalCase(er.Name) + "EphemeralResourceModel"
}

// ephemeralResourceTypeName returns the Terraform ephemeral resource type name. It prefers
// EphemeralResourceIR.TypeName and falls back to a snake_cased ephemeral resource
// name so generated type names are always valid Terraform identifiers.
func ephemeralResourceTypeName(er ir.EphemeralResourceIR) string {
	if strings.TrimSpace(er.TypeName) != "" {
		return strings.TrimSpace(er.TypeName)
	}
	return typeNameFallback(er.Name)
}

// ephemeralModelAttributes returns the deduplicated list of attributes that
// appear in the ephemeral resource model, combining config and result schemas.
func ephemeralModelAttributes(er ir.EphemeralResourceIR) []ir.AttributeIR {
	seen := make(map[string]struct{})
	attrs := make([]ir.AttributeIR, 0, len(er.ConfigSchema.Attributes)+len(er.ResultSchema.Attributes))
	for _, attr := range er.ConfigSchema.Attributes {
		if _, ok := seen[attr.Name]; ok {
			continue
		}
		seen[attr.Name] = struct{}{}
		attrs = append(attrs, attr)
	}
	for _, attr := range er.ResultSchema.Attributes {
		if _, ok := seen[attr.Name]; ok {
			continue
		}
		seen[attr.Name] = struct{}{}
		attrs = append(attrs, attr)
	}
	return attrs
}

// ephemeralSchemaValues builds the []ast.Expr key/value elements for ephemeralschema.Schema{...}.
func ephemeralSchemaValues(er ir.EphemeralResourceIR) []ast.Expr {
	elems := []ast.Expr{}
	if v := litOrOmit(er.Description); v != nil {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", v))
	}

	attrs := ephemeralMergedAttributes(er)
	blocks := ephemeralMergedBlocks(er)

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				ephemeralAttributeExpr(attr, er.Name),
			))
		}
		elems = append(elems, astgen.KeyValue("Attributes", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("ephemeralschema", "Attribute")),
			attrElems...,
		)))
	}

	if len(blocks) > 0 {
		blockElems := make([]ast.Expr, 0, len(blocks))
		for _, block := range blocks {
			blockElems = append(blockElems, astgen.KeyValueExpr(
				astgen.Lit(block.Name),
				ephemeralBlockExpr(block, er.Name),
			))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("ephemeralschema", "Block")),
			blockElems...,
		)))
	}

	return elems
}

// ephemeralMergedAttributes returns the merged top-level attributes for an
// ephemeral resource schema. Config attributes keep their Required/Optional
// flags; result attributes are marked Computed. Attributes present in both
// schemas are merged so they can be configured and computed. The config
// schema takes precedence for the type definition; if the result schema
// defines the same attribute with an incompatible type, generation panics.
func ephemeralMergedAttributes(er ir.EphemeralResourceIR) []ir.AttributeIR {
	byName := make(map[string]ir.AttributeIR)
	for _, attr := range er.ConfigSchema.Attributes {
		byName[attr.Name] = attr
	}
	for _, attr := range er.ResultSchema.Attributes {
		if existing, ok := byName[attr.Name]; ok {
			if !ephemeralAttributeTypesCompatible(existing, attr) {
				panic(fmt.Sprintf("ephemeral resource %q: attribute %q has incompatible types in config and result schemas", er.Name, attr.Name))
			}
			// Same attribute in config and result: allow optional+computed.
			// The config schema's IR (including SchemaIR type) takes precedence.
			if existing.Required {
				existing.Required = false
				existing.Optional = true
			}
			existing.Computed = true
			byName[attr.Name] = existing
			continue
		}
		attr.Computed = true
		byName[attr.Name] = attr
	}

	attrs := make([]ir.AttributeIR, 0, len(byName))
	for _, attr := range byName {
		attrs = append(attrs, attr)
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	return attrs
}

// ephemeralAttributeTypesCompatible reports whether the config and result
// attributes describe the same structural type. Metadata such as
// Required/Optional/Computed/Description is ignored.
func ephemeralAttributeTypesCompatible(config, result ir.AttributeIR) bool {
	return ephemeralSchemaTypesEqual(config.Schema, result.Schema)
}

// ephemeralSchemaTypesEqual reports whether two schema IR values describe the
// same structural type recursively. Metadata such as Required/Optional,
// Descriptions, and validators are ignored.
//
// Object-like schemas are compared by building a map of the config-side
// attributes keyed by name and then verifying that the result-side attributes
// match by name and type. This runs in O(n) time and O(n) additional space,
// where n is the number of attributes in the object; the cost is acceptable
// for typical Terraform schemas.
func ephemeralSchemaTypesEqual(a, b ir.SchemaIR) bool {
	if a.Type != b.Type {
		return false
	}

	if (a.Collection == nil) != (b.Collection == nil) {
		return false
	}
	if a.Collection != nil {
		if a.Collection.Kind != b.Collection.Kind {
			return false
		}
		return ephemeralSchemaTypesEqual(a.Collection.ElementType, b.Collection.ElementType)
	}

	if (a.Union == nil) != (b.Union == nil) {
		return false
	}
	if a.Union != nil {
		// Union compatibility is intentionally shallow: only the presence of a
		// Union definition is matched. The discriminator and individual value
		// schemas are not compared, so structurally different unions will be
		// treated as compatible. Providers with complex union definitions
		// should validate them independently.
		return true
	}

	aObject := schema.IsObjectLike(a)
	bObject := schema.IsObjectLike(b)
	if aObject != bObject {
		return false
	}
	if aObject {
		if len(a.Attributes) != len(b.Attributes) {
			return false
		}
		aAttrs := make(map[string]ir.AttributeIR, len(a.Attributes))
		for _, attr := range a.Attributes {
			aAttrs[attr.Name] = attr
		}
		for _, attr := range b.Attributes {
			other, ok := aAttrs[attr.Name]
			if !ok {
				return false
			}
			if !ephemeralAttributeTypesCompatible(other, attr) {
				return false
			}
		}
	}

	return true
}

// ephemeralMergedBlocks returns the merged top-level blocks for an ephemeral
// resource schema. Config blocks take precedence over result blocks when names
// collide.
func ephemeralMergedBlocks(er ir.EphemeralResourceIR) []ir.BlockIR {
	byName := make(map[string]ir.BlockIR)
	for _, block := range er.ConfigSchema.Blocks {
		byName[block.Name] = block
	}
	for _, block := range er.ResultSchema.Blocks {
		if _, ok := byName[block.Name]; ok {
			continue
		}
		byName[block.Name] = block
	}

	blocks := make([]ir.BlockIR, 0, len(byName))
	for _, block := range byName {
		blocks = append(blocks, block)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Name < blocks[j].Name })
	return blocks
}

// ephemeralAttributeExpr returns an ast expression for an ephemeral/schema Attribute.
// resourceName is the ephemeral resource name and is included in panic messages
// for unsupported attributes so operators can locate the offending resource.
func ephemeralAttributeExpr(attr ir.AttributeIR, resourceName string) ast.Expr {
	return ephemeralAttributeExprWithPath(attr, "", resourceName)
}

// ephemeralAttributeExprWithPath returns an ast expression for an ephemeral/schema
// Attribute, tracking the dotted parent path so that unsupported nested attributes
// can be reported with their full location. resourceName is included in the panic
// message alongside the path.
func ephemeralAttributeExprWithPath(attr ir.AttributeIR, parentPath, resourceName string) ast.Expr {
	path := fullAttrPath(parentPath, attr.Name)
	expr := ephemeralFrameworkAttributeExpr(attr, path, resourceName)
	if expr == nil {
		// A nested attribute that cannot be represented (e.g. a nested
		// collection) is dropped by the nested map builder; a top-level
		// attribute should never be nil because the framework expr falls back
		// to DynamicAttribute (G2).
		if parentPath == "" {
			panic(fmt.Sprintf("ephemeral resource %q attribute %q: schema has no recognizable type or nested shape", resourceName, path))
		}
		return nil
	}
	return expr
}

// ephemeralFrameworkAttributeExpr maps an IR attribute to a Terraform Plugin
// Framework ephemeral/schema attribute expression. attrPath is the dotted path to
// the current attribute and is propagated to nested attribute maps. resourceName is
// included in panic messages so unsupported attributes can be traced back to the
// owning ephemeral resource. This helper intentionally accepts resourceName while
// the resource and datasource attribute helpers do not, because ephemeral (and
// list) resource panic messages include the resource name.
func ephemeralFrameworkAttributeExpr(attr ir.AttributeIR, attrPath, resourceName string) ast.Expr {
	s := attr.Schema

	// Collection types.
	if s.Collection != nil {
		if expr := ephemeralCollectionAttributeExpr(attr, attrPath, resourceName); expr != nil {
			return expr
		}
	}

	// Primitive types.
	if s.Type != "" {
		if expr := ephemeralPrimitiveAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework ephemeral schema has no first-class union attribute.
	// When a schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := ephemeralAttributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", ephemeralNestedAttributesMapFromSchema(*merged, attrPath, resourceName)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "DynamicAttribute"), ephemeralAttributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := ephemeralAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", ephemeralNestedAttributesMapFromSchema(s, attrPath, resourceName)),
		})
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "SingleNestedAttribute"), d...)
	}

	// Unrepresentable shapes (e.g. a nested collection) cannot map to a
	// framework attribute. At the top level a DynamicAttribute is valid and
	// honest; nested inside a collection it would be rejected by the
	// framework, so the nested map builder drops it (G2).
	if strings.Contains(attrPath, ".") {
		return nil
	}
	return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "DynamicAttribute"), ephemeralAttributeValues(attr, nil)...)
}

// ephemeralCollectionAttributeExpr maps a collection-typed attribute to its
// framework attribute, or nil when the shape falls through to the
// primitive/union/unrepresentable handling below (G12).
func ephemeralCollectionAttributeExpr(attr ir.AttributeIR, attrPath, resourceName string) ast.Expr {
	elem := schema.DynamicUnionElement(attr.Schema.Collection.ElementType)
	// A collection whose element is dynamic/null cannot be represented as a
	// framework collection (List{ElementType: DynamicType} is rejected by
	// the framework); treat it as an unrepresentable shape (G12).
	if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull {
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "DynamicAttribute"), ephemeralAttributeValues(attr, nil)...)
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
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "DynamicAttribute"), ephemeralAttributeValues(attr, nil)...)
	}
	switch attr.Schema.Collection.Kind {
	case ir.List:
		return ephemeralListElementAttributeExpr(attr, elem, attrPath, resourceName, "List")
	case ir.Set:
		return ephemeralListElementAttributeExpr(attr, elem, attrPath, resourceName, "Set")
	case ir.Map:
		return ephemeralMapElementAttributeExpr(attr, elem, attrPath, resourceName)
	}
	return nil
}

// ephemeralListElementAttributeExpr maps a List/Set element to its framework
// attribute (List*Attribute or Set*Attribute).
func ephemeralListElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, resourceName, kind string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := ephemeralAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", kind+"Attribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := ephemeralAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("ephemeralschema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", ephemeralNestedAttributesMapFromSchema(elem, attrPath, resourceName)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", kind+"NestedAttribute"), d...)
	}
	return nil
}

// ephemeralMapElementAttributeExpr maps a Map element to its framework
// attribute (MapAttribute or MapNestedAttribute).
func ephemeralMapElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, resourceName string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := ephemeralAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "MapAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := ephemeralAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("ephemeralschema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", ephemeralNestedAttributesMapFromSchema(elem, attrPath, resourceName)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "MapNestedAttribute"), d...)
	}
	return nil
}

// ephemeralPrimitiveAttributeExpr maps a primitive-typed attribute to its
// framework attribute, or nil when the type is not a recognized primitive.
func ephemeralPrimitiveAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	switch attr.Schema.Type {
	case ir.TypeString:
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "StringAttribute"), ephemeralAttributeValues(attr, nil)...)
	case ir.TypeInt:
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "Int64Attribute"), ephemeralAttributeValues(attr, nil)...)
	case ir.TypeFloat:
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "Float64Attribute"), ephemeralAttributeValues(attr, nil)...)
	case ir.TypeBool:
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "BoolAttribute"), ephemeralAttributeValues(attr, nil)...)
	case ir.TypeDynamic:
		// A DynamicAttribute is only valid at the top level; nested inside a
		// collection it is rejected by the framework, so the nested map
		// builder drops it (G12).
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", "DynamicAttribute"), ephemeralAttributeValues(attr, nil)...)
	}
	return nil
}

// ephemeralBlockExpr returns an ast expression for an ephemeral/schema Block.
// resourceName is included in panic messages for unsupported nested attributes.
// Blocks are always top-level in ephemeral resource schemas, so block.Name is
// used directly as the parent path for the block's nested attributes.
func ephemeralBlockExpr(block ir.BlockIR, resourceName string) ast.Expr {
	// Blocks are always top-level for ephemeral resources; block.Name is both the
	// Terraform block name and the parent path for nested attributes.
	pathForNesting := block.Name
	attrs := ephemeralNestedAttributesMap(block.Schema, pathForNesting, resourceName)

	var kind string
	var elems []ast.Expr
	switch block.NestingMode {
	case ir.NestingList:
		kind = "ListNestedBlock"
		elems = append(elems, astgen.KeyValue("NestedObject", astgen.CompositeLit(
			astgen.QualExpr("ephemeralschema", "NestedBlockObject"),
			astgen.KeyValue("Attributes", attrs),
		)))
		if exprs := ephemeralBlockSizeValidatorExprs(block, "listvalidator"); len(exprs) > 0 {
			elems = append(elems, astgen.KeyValueExpr(
				astgen.Ident("Validators"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("validator", "List")), exprs...),
			))
		}
	case ir.NestingSet:
		kind = "SetNestedBlock"
		elems = append(elems, astgen.KeyValue("NestedObject", astgen.CompositeLit(
			astgen.QualExpr("ephemeralschema", "NestedBlockObject"),
			astgen.KeyValue("Attributes", attrs),
		)))
		if exprs := ephemeralBlockSizeValidatorExprs(block, "setvalidator"); len(exprs) > 0 {
			elems = append(elems, astgen.KeyValueExpr(
				astgen.Ident("Validators"),
				astgen.CompositeLit(astgen.SliceType(astgen.QualExpr("validator", "Set")), exprs...),
			))
		}
	default:
		// SingleNestedBlock does not support cardinality constraints and exposes its
		// attributes directly; MinItems/MaxItems are only emitted for List/Set blocks.
		kind = "SingleNestedBlock"
		elems = append(elems, astgen.KeyValue("Attributes", attrs))
	}

	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	} else if block.Deprecated {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit("Deprecated")))
	}

	return astgen.CompositeLit(astgen.QualExpr("ephemeralschema", kind), elems...)
}

// ephemeralBlockSizeValidatorExprs returns the plugin-framework validator
// expressions for an ephemeral resource block's MinItems/MaxItems cardinality
// constraints. validatorPkg is the named validator package (e.g. "listvalidator"
// or "setvalidator") whose SizeAtLeast/SizeAtMost helpers are emitted. The
// caller selects validatorPkg based on the block's NestingMode (L-35: the prior
// signature took four string parameters, two of which were unused).
func ephemeralBlockSizeValidatorExprs(block ir.BlockIR, validatorPkg string) []ast.Expr {
	if block.NestingMode == ir.NestingSingle {
		return nil
	}
	if block.MinItems == nil && block.MaxItems == nil {
		return nil
	}
	var exprs []ast.Expr
	if block.MinItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr(validatorPkg, "SizeAtLeast"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MinItems))),
		))
	}
	if block.MaxItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr(validatorPkg, "SizeAtMost"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MaxItems))),
		))
	}
	return exprs
}

// ephemeralAttributeValues builds the common field dictionary for an ephemeral
// resource schema attribute. Ephemeral attributes preserve the Required/Optional
// flags from the IR and mark result-only attributes Computed.
func ephemeralAttributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
	elems := []ast.Expr{}

	if attr.MarkdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.MarkdownDescription)))
	} else if attr.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.Description)))
	}

	if attr.Required {
		elems = append(elems, astgen.KeyValue("Required", astgen.BoolLit(true)))
	} else if attr.Optional {
		elems = append(elems, astgen.KeyValue("Optional", astgen.BoolLit(true)))
	}
	if attr.Computed {
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

// ephemeralNestedAttributesMap returns map[string]schema.Attribute{...} for the given
// object schema. parentPath is the dotted path of the enclosing attribute or block.
// It intentionally iterates only Attributes; the Terraform plugin-framework
// NestedAttributeObject type does not support Blocks, so any Blocks present in the
// ObjectSchemaIR are ignored. resourceName is included in panic messages for
// unsupported nested attributes.
func ephemeralNestedAttributesMap(s ir.ObjectSchemaIR, parentPath, resourceName string) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		expr := ephemeralAttributeExprWithPath(attr, parentPath, resourceName)
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
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("ephemeralschema", "Attribute")),
		elems...,
	)
}

// ephemeralNestedAttributesMapFromSchema converts a SchemaIR object-like value
// to a nested attributes map expression for ephemeral resource schemas. parentPath
// is the dotted path of the enclosing attribute and is propagated to nested attribute
// panics. resourceName is included in panic messages so unsupported nested
// attributes can be traced back to the owning ephemeral resource.
//
// Blocks are intentionally omitted from the resulting nested attributes map
// because NestedAttributeObject only supports Attributes, not Blocks.
func ephemeralNestedAttributesMapFromSchema(s ir.SchemaIR, parentPath, resourceName string) ast.Expr {
	return ephemeralNestedAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes}, parentPath, resourceName)
}
