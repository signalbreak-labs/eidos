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

// ActionFile returns the generated internal/provider/action_<name>.go file for a
// Terraform plugin-framework action built from the supplied ActionIR.
// clientImport is the import path of the generated internal/client package,
// used when the Invoke body is wired to the API client.
func ActionFile(a ir.ActionIR, clientImport string) File {
	relPath := path.Join("internal", "provider", fmt.Sprintf("action_%s.go", naming.SnakeCase(a.Name)))
	file, err := renderEntitySafely(func() (*ast.File, error) {
		return generateActionFile(a, clientImport), nil
	})
	if err != nil {
		return ErrorFile(relPath, err)
	}
	return GoCodeAST(relPath, file)
}

// ActionFiles returns the generated action files for every ActionIR in the
// provider. clientImport is the import path of the generated internal/client
// package. Files are emitted in the order the actions are supplied.
func ActionFiles(actions []ir.ActionIR, clientImport string) []File {
	files := make([]File, 0, len(actions))
	for _, a := range actions {
		files = append(files, ActionFile(a, clientImport))
	}
	return files
}

// generateActionFile builds the *ast.File for internal/provider/action_<name>.go.
// clientImport is the import path of the generated internal/client package,
// used when the action's Invoke body is wired to the API client.
func generateActionFile(a ir.ActionIR, clientImport string) *ast.File {
	f := astgen.NewFile("provider")

	structName := actionStructName(a)
	modelName := actionModelName(a)
	typeName := actionTypeName(a)
	wiring := planActionWiring(a)

	// Compile-time interface assertions.
	assertions := []struct {
		name string
		ifc  string
	}{
		{"action.Action", "Action"},
	}
	if wiring.wired {
		assertions = append(assertions, struct {
			name string
			ifc  string
		}{"action.ActionWithConfigure", "ActionWithConfigure"})
	}
	if wiring.modifyPlan != nil {
		assertions = append(assertions, struct {
			name string
			ifc  string
		}{"action.ActionWithModifyPlan", "ActionWithModifyPlan"})
	}
	if wiring.validateConfig != nil {
		assertions = append(assertions, struct {
			name string
			ifc  string
		}{"action.ActionWithValidateConfig", "ActionWithValidateConfig"})
	}
	for _, as := range assertions {
		f.AddComment(fmt.Sprintf("Compile-time interface assertion for %s.", as.name))
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("action", as.ifc),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)))
	}

	// Action struct. Wired actions carry the API client supplied by the
	// provider's Configure method via the framework action-data mechanism.
	f.AddCommentf("%s is the generated Terraform action implementation.", structName)
	if wiring.wired {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
			astgen.Field("client", astgen.StarExpr(astgen.QualExpr("client", "Client")), ""),
		)))
	} else {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType()))
	}

	// Action model.
	f.AddCommentf("%s describes the action configuration shape.", modelName)
	modelFields := actionModelFields(a)
	f.AddDecl(astgen.TypeDecl(modelName, astgen.StructType(modelFields...)))

	// New constructor.
	f.AddCommentf("New%s returns a new instance of the generated action.", structName)
	f.AddDecl(astgen.FuncDeclFull(
		"New"+structName,
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("action", "Action"), "")),
		astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(structName))))),
	))

	// Metadata method.
	f.AddComment("Metadata returns the action type name.")
	var metadataStmt ast.Stmt
	if typeName != "" {
		metadataStmt = astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(typeName)},
			token.ASSIGN,
		)
	} else {
		metadataStmt = astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Binary(
				astgen.Binary(
					astgen.Selector(astgen.Ident("req"), "ProviderTypeName"),
					token.ADD,
					astgen.Lit("_"),
				),
				token.ADD,
				astgen.Lit(naming.SnakeCase(a.Name)),
			)},
			token.ASSIGN,
		)
	}
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("action", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(metadataStmt),
	))

	// Schema method.
	f.AddComment("Schema returns the action schema.")
	schemaValues := actionSchemaValues(a)
	f.AddDecl(astgen.MethodDecl(
		"Schema", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("action", "SchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "SchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("schema", "Schema"), schemaValues...)},
			token.ASSIGN,
		)),
	))

	// Invoke method. Wired actions call the invoke endpoint and surface any
	// error via Diagnostics; actions without a resolvable bodiless invoke
	// mapping keep the honest scaffold Invoke body. The extracted invokeRemote
	// helper is emitted alongside Invoke so the request/response logic is
	// unit-testable without a tfsdk.Config.
	if wiring.invoke != nil {
		f.AddComment("Invoke executes the action against the remote API.")
	} else {
		f.AddComment("Invoke executes the action against the remote API.", "The generated Invoke method is intentionally stubbed; the remote API is not wired.")
	}
	invokeBody, invokeHelperComment, invokeHelperDecl := actionInvokePlan(a, wiring, modelName, structName)
	f.AddDecl(astgen.MethodDecl(
		"Invoke", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("action", "InvokeRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "InvokeResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(invokeBody...),
	))
	if invokeHelperDecl != nil {
		f.AddComment(invokeHelperComment)
		f.AddDecl(invokeHelperDecl)
	}

	// ModifyPlan method. Wired when the action declares a resolvable
	// modify_plan_operation mapping: the body calls the preflight endpoint and
	// surfaces non-success statuses as diagnostics. The spec does not encode
	// plan mutations, so a successful call leaves the plan unchanged.
	//
	// The interface and method are emitted only when the mapping resolves. A
	// scaffold ModifyPlan that hard-errors (resp.Diagnostics.AddError) would
	// fail every terraform plan for the action — the framework invokes ModifyPlan
	// during planning — so an action without a resolvable preflight endpoint
	// simply does not implement the optional ActionWithModifyPlan interface.
	if wiring.modifyPlan != nil {
		f.AddComment("ModifyPlan validates the action plan with optional API access.")
		summary := fmt.Sprintf("Error running preflight for action %s", actionTypeName(a))
		f.AddDecl(astgen.MethodDecl(
			"ModifyPlan", "r", astgen.StarExpr(astgen.Ident(structName)),
			astgen.Params(
				astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("req", astgen.QualExpr("action", "ModifyPlanRequest"), ""),
				astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "ModifyPlanResponse")), ""),
			),
			astgen.Results(),
			astgen.Block(wiredActionPreflightBody(*wiring.modifyPlan, modelName, summary)...),
		))
	}

	// ValidateConfig method. Wired when the action declares a resolvable
	// validate_config_operation mapping: the body calls the server-side
	// validation endpoint and surfaces non-success statuses as diagnostics.
	//
	// The interface and method are emitted only when the mapping resolves. A
	// scaffold ValidateConfig that hard-errors would fail every terraform plan
	// for the action — the framework invokes ValidateConfig during planning — so
	// an action without a resolvable server-side validation endpoint simply does
	// not implement the optional ActionWithValidateConfig interface.
	if wiring.validateConfig != nil {
		f.AddComment("ValidateConfig validates the action configuration.")
		summary := fmt.Sprintf("Error validating action %s configuration", actionTypeName(a))
		f.AddDecl(astgen.MethodDecl(
			"ValidateConfig", "r", astgen.StarExpr(astgen.Ident(structName)),
			astgen.Params(
				astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("req", astgen.QualExpr("action", "ValidateConfigRequest"), ""),
				astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("action", "ValidateConfigResponse")), ""),
			),
			astgen.Results(),
			astgen.Block(wiredActionPreflightBody(*wiring.validateConfig, modelName, summary)...),
		))
	}

	// Configure method. Wired actions implement ActionWithConfigure to receive
	// the API client constructed by the provider's Configure method.
	if wiring.wired {
		f.AddComment("Configure stores the API client supplied by the provider.")
		f.AddDecl(actionConfigureDecl(structName))
	}

	f.AddImport("context", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/action", "action")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/action/schema", "schema")
	if wiring.wired {
		// Wired Invoke bodies build and send HTTP requests through the generated
		// client and surface errors via Diagnostics. A body-bearing action
		// encodes the config model as a JSON request body (modelToJSONMap +
		// json.Marshal + bytes.NewReader), so encoding/json and bytes are
		// imported only when sendsBody; there is no response to decode (no io).
		f.AddImport(clientImport, "client")
		f.AddImports("fmt", "net/http")
		if wiring.sendsBody {
			f.AddImports("bytes", "encoding/json")
		}
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
	// The model struct references types.* via modelFieldType for every config
	// attribute field (modelFieldType always returns a types.* expression), and
	// the schema references types.* via primitiveAttrType for a collection
	// attribute inside a block whose element type is primitive (blocks are
	// schema-only and not model fields). An action whose config schema has no
	// attributes and only primitive/block-of-primitive nested attributes must
	// not import types, or the import is unused and the generated provider
	// does not compile (the §6 latent unused-import bug). The attribute gate
	// is the model-field count; the block gate reuses the list resource
	// objectSchemaReferencesTypes helper, which mirrors the exact render
	// decision for primitiveAttrType.
	if actionNeedsTypesImport(a) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	// The schema/validator package is referenced by a discriminated union's
	// DiscriminatorValidator ([]validator.Object, D2) and by a List/Set block's
	// size validators ([]validator.List / []validator.Set, N-24); gate the import
	// on either condition to avoid "imported and not used".
	if schema.ObjectSchemaHasDiscriminatedUnion(a.ConfigSchema) || objectSchemaNeedsBlockSizeValidators(a.ConfigSchema) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	}
	// List/Set blocks with MinItems/MaxItems constraints reference
	// listvalidator.SizeAtLeast/SizeAtMost or setvalidator equivalents (N-24).
	needsList, needsSet := blockValidatorPackageImports(a.ConfigSchema)
	if needsList {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/listvalidator", "listvalidator")
	}
	if needsSet {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/setvalidator", "setvalidator")
	}

	return f.AST()
}

// actionInvokePlan selects the Invoke body and the extracted invokeRemote
// helper declaration for an action. An unwired action (no resolvable invoke
// mapping) gets the honest scaffold body and no helper. Factoring this out of
// generateActionFile keeps that function's cognitive complexity bounded.
func actionInvokePlan(a ir.ActionIR, wiring actionWiringPlan, modelName, structName string) (invokeBody []ast.Stmt, helperComment string, helperDecl *ast.FuncDecl) {
	invokeBody = scaffoldActionInvokeBody(modelName)
	if wiring.invoke == nil {
		return invokeBody, "", nil
	}
	return wiredActionInvokeBody(a, modelName),
		"invokeRemote performs the invoke HTTP exchange and surfaces any error via diagnostics. Extracted from Invoke so the request/response logic is unit-testable without a tfsdk.Config.",
		wiredActionInvokeHelperDecl(a, *wiring.invoke, modelName, structName)
}

// actionModelFields returns the model struct fields for an action's config
// schema, mirroring the resource/data-source/ephemeral model: one field per
// config attribute (skipAttrForModel is currently a no-op), tagged with the
// tfsdk attribute name. Extracted so the types-import gate can decide from the
// same field set the model is built from.
func actionModelFields(a ir.ActionIR) []*ast.Field {
	modelFields := make([]*ast.Field, 0, len(a.ConfigSchema.Attributes))
	for _, attr := range a.ConfigSchema.Attributes {
		if schema.SkipAttrForModel(attr) {
			continue
		}
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(attr.Name),
			modelFieldType(attr),
			modelFieldTags(attr),
		))
	}
	return modelFields
}

// actionNeedsTypesImport reports whether the generated action file references
// the terraform-plugin-framework types package. The model references types.*
// via modelFieldType for every config attribute field (modelFieldType always
// returns a types.* expression), so any config attribute needs types. Blocks
// are schema-only (not model fields) and reference types only via
// primitiveAttrType when a nested collection attribute has a primitive element
// type; that block gate reuses the list resource objectSchemaReferencesTypes
// helper, which mirrors the exact render decision. An action with an empty
// config schema and only primitive block attributes must not import types, or
// the import is unused and the generated provider does not compile (the §6
// latent unused-import bug).
func actionNeedsTypesImport(a ir.ActionIR) bool {
	if len(actionModelFields(a)) > 0 {
		return true
	}
	for _, block := range a.ConfigSchema.Blocks {
		if objectSchemaReferencesTypes(block.Schema) {
			return true
		}
	}
	return false
}

// Sentinel errors for programmatic detection of generator panic conditions in
// the action generator. These values are wrapped by panic messages so callers
// can match the cause with errors.Is even though the public API still panics
// for unsupported shapes.
var (
	ErrEmptyActionName             = errors.New("action name must not be empty")
	ErrUnsupportedActionShape      = errors.New("unsupported action config attribute shape")
	ErrUnknownActionBlockNesting   = errors.New("unknown action block nesting mode")
	ErrUnsupportedActionCollection = errors.New("unsupported action collection kind")
)

// actionStructName returns the generated action struct name for an IR action.
func actionStructName(a ir.ActionIR) string {
	if strings.TrimSpace(a.Name) == "" {
		panic(fmt.Errorf("%w: name is empty", ErrEmptyActionName))
	}
	return naming.GoTypeName(a.Name) + "Action"
}

// actionModelName returns the generated model struct name for an IR action.
func actionModelName(a ir.ActionIR) string {
	if strings.TrimSpace(a.Name) == "" {
		panic(fmt.Errorf("%w: name is empty", ErrEmptyActionName))
	}
	return naming.GoTypeName(a.Name) + "ActionModel"
}

// actionTypeName returns the Terraform action type name. It prefers
// ActionIR.TypeName and falls back to an empty string so Metadata can build
// the type from ProviderTypeName.
func actionTypeName(a ir.ActionIR) string {
	if strings.TrimSpace(a.TypeName) != "" {
		return strings.TrimSpace(a.TypeName)
	}
	return ""
}

// actionSchemaValues builds the []ast.Expr key/value elements for
// action/schema.Schema{...}.
//
// The action schema emits Description (not MarkdownDescription) as the primary
// description field. MarkdownDescription is still emitted when it is explicitly
// set on the IR.
func actionSchemaValues(a ir.ActionIR) []ast.Expr {
	elems := []ast.Expr{}
	if v := litOrOmit(a.Description); v != nil {
		elems = append(elems, astgen.KeyValue("Description", v))
	}
	if v := litOrOmit(a.MarkdownDescription); v != nil {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", v))
	}

	attrs := a.ConfigSchema.Attributes
	blocks := a.ConfigSchema.Blocks

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				actionAttributeExpr(attr),
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
				actionBlockExpr(block),
			))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Block")),
			blockElems...,
		)))
	}

	return elems
}

// actionAttributeExpr returns an ast expression for an action/schema Attribute.
// It panics if the IR attribute cannot be mapped to a supported action schema
// attribute so that unsupported shapes fail closed instead of silently falling
// back to a string attribute.
func actionAttributeExpr(attr ir.AttributeIR) ast.Expr {
	expr := frameworkActionAttributeExpr(attr)
	if expr == nil {
		panic(fmt.Errorf("%w: attribute %q has no recognizable type or nested shape", ErrUnsupportedActionShape, attr.Name))
	}
	return expr
}

// frameworkActionAttributeExpr maps an IR attribute to a Terraform Plugin
// Framework action/schema attribute expression.
func frameworkActionAttributeExpr(attr ir.AttributeIR) ast.Expr {
	s := attr.Schema

	// Collection types.
	if s.Collection != nil {
		if expr := actionCollectionAttributeExpr(attr); expr != nil {
			return expr
		}
	}

	// Primitive types.
	if s.Type != "" {
		if expr := actionPrimitiveAttributeExpr(attr); expr != nil {
			return expr
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework action schema has no first-class union attribute. When a
	// schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := actionAttributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", nestedActionAttributesMapFromSchema(*merged)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), actionAttributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := actionAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", nestedActionAttributesMapFromSchema(s)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
	}

	// Unrepresentable shapes (e.g. a nested collection) map to a
	// DynamicAttribute so generation succeeds instead of panicking (G2).
	return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), actionAttributeValues(attr, nil)...)
}

// actionCollectionAttributeExpr maps a collection-typed attribute to its
// framework attribute, or DynamicAttribute when the element type is
// unrepresentable in the framework (G12).
func actionCollectionAttributeExpr(attr ir.AttributeIR) ast.Expr {
	elem := schema.DynamicUnionElement(attr.Schema.Collection.ElementType)
	// A collection whose element is, or contains at any depth, a dynamic-typed
	// shape cannot be rendered as a typed framework collection: the framework
	// rejects any collection whose element type contains a dynamic
	// (fwtype.ContainsCollectionWithDynamic). Emit the whole collection as a
	// DynamicAttribute instead, per the framework's own guidance. An enclosing
	// collection's ContainsNestedDynamic check promotes any collection ancestor,
	// so this is never reached inside a collection (G12).
	if schema.ContainsNestedDynamic(elem) {
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), actionAttributeValues(attr, nil)...)
	}
	switch attr.Schema.Collection.Kind {
	case ir.List:
		return actionListElementAttributeExpr(attr, elem, "List")
	case ir.Set:
		return actionListElementAttributeExpr(attr, elem, "Set")
	case ir.Map:
		return actionMapElementAttributeExpr(attr, elem)
	default:
		panic(fmt.Errorf("%w: collection kind %q for attribute %q", ErrUnsupportedActionCollection, attr.Schema.Collection.Kind, attr.Name))
	}
}

// actionListElementAttributeExpr maps a List/Set element to its framework
// attribute (List*Attribute or Set*Attribute).
func actionListElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, kind string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := actionAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"Attribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := actionAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", nestedActionAttributesMapFromSchema(elem)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"NestedAttribute"), d...)
	}
	return nil
}

// actionMapElementAttributeExpr maps a Map element to its framework attribute
// (MapAttribute or MapNestedAttribute).
func actionMapElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := actionAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := actionAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", nestedActionAttributesMapFromSchema(elem)),
			)),
		})
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapNestedAttribute"), d...)
	}
	return nil
}

// actionPrimitiveAttributeExpr maps a primitive-typed attribute to its
// framework attribute, or nil when the type is not a recognized primitive.
func actionPrimitiveAttributeExpr(attr ir.AttributeIR) ast.Expr {
	switch attr.Schema.Type {
	case ir.TypeString:
		return astgen.CompositeLit(astgen.QualExpr("schema", "StringAttribute"), actionAttributeValues(attr, nil)...)
	case ir.TypeInt:
		return astgen.CompositeLit(astgen.QualExpr("schema", "Int64Attribute"), actionAttributeValues(attr, nil)...)
	case ir.TypeFloat:
		return astgen.CompositeLit(astgen.QualExpr("schema", "Float64Attribute"), actionAttributeValues(attr, nil)...)
	case ir.TypeBool:
		return astgen.CompositeLit(astgen.QualExpr("schema", "BoolAttribute"), actionAttributeValues(attr, nil)...)
	case ir.TypeDynamic, ir.TypeNull:
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), actionAttributeValues(attr, nil)...)
	}
	return nil
}

// actionBlockExpr returns an ast expression for an action/schema Block.
func actionBlockExpr(block ir.BlockIR) ast.Expr {
	var kind string
	attrs := nestedActionAttributesMap(block.Schema)

	var elems []ast.Expr
	switch block.NestingMode {
	case ir.NestingSingle:
		kind = "SingleNestedBlock"
		elems = append(elems, astgen.KeyValue("Attributes", attrs))
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
	default:
		panic(fmt.Errorf("%w: %q for block %q", ErrUnknownActionBlockNesting, block.NestingMode, block.Name))
	}

	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	}

	return astgen.CompositeLit(astgen.QualExpr("schema", kind), elems...)
}

// actionAttributeValues builds the common field dictionary for an action schema
// attribute. Action configuration attributes cannot be Computed or Sensitive,
// so those flags are ignored. WriteOnly is supported by the action schema and
// is emitted when set. Attributes default to Optional when neither Required nor
// Optional is explicitly set.
//
// Generic AttributeIR.Validators and AttributeIR.PlanModifiers are intentionally
// not propagated to action config attributes. The plugin-framework action
// schema supports typed validators, but the IR's generic ValidatorIR and
// PlanModifierIR metadata is not mapped to typed validator/plan-modifier
// constructors; only schema-level constraints are wired through
// ValidatorsFile.
func actionAttributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
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
	if attr.WriteOnly {
		elems = append(elems, astgen.KeyValue("WriteOnly", astgen.BoolLit(true)))
	}
	if attr.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(attr.DeprecationMessage)))
	} else if attr.Deprecated {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit("Deprecated")))
	}

	return append(elems, extra...)
}

// nestedActionAttributesMap returns map[string]schema.Attribute{...} for the given object schema.
func nestedActionAttributesMap(s ir.ObjectSchemaIR) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		elems = append(elems, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			actionAttributeExpr(attr),
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Attribute")),
		elems...,
	)
}

// nestedActionAttributesMapFromSchema converts a SchemaIR object-like value to a nested
// attributes map expression for action schemas.
//
// Blocks nested inside object-typed attributes are dropped: NestedAttributeObject
// only supports Attributes, not Blocks (M-14). Known limitation; see CLAUDE.md
// "Current limitations".
func nestedActionAttributesMapFromSchema(s ir.SchemaIR) ast.Expr {
	return nestedActionAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes})
}
