package generator

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ProviderFile returns the generated internal/provider/provider.go file for a
// Terraform plugin-framework provider built from the supplied ProviderIR.
//
// The renderer is wrapped in a recover so a panic from hostile IR — e.g.
// validateIntBound/validatePatternProperties on a fractional exclusiveMinimum or
// an invalid regex reachable via provider config attributes — surfaces as a
// generation error instead of crashing the process. This mirrors the recover in
// ResourceFile/DataSourceFile/ActionFile/EphemeralFile/ListResourceFile (M-56).
func ProviderFile(pir ir.ProviderIR) (File, error) {
	return ProviderFileWithClient(pir, "")
}

// ProviderFileWithClient is ProviderFile with the import path of the generated
// internal/client package. When clientImport is non-empty and at least one
// resource has a complete CRUD mapping, the generated Configure method
// constructs the API client and passes it to resources via the framework
// provider-data mechanism. An empty clientImport keeps the historical
// config-decode-only Configure body.
func ProviderFileWithClient(pir ir.ProviderIR, clientImport string) (File, error) {
	f, err := func() (f *ast.File, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				if recErr, ok := rec.(error); ok {
					err = fmt.Errorf("renderer panic: %w", recErr)
				} else {
					err = fmt.Errorf("renderer panic: %v", rec)
				}
			}
		}()
		if err = validateProviderConfig(pir.ConfigSchema); err != nil {
			return
		}
		f, err = generateProviderFile(pir, clientImport)
		return
	}()
	if err != nil {
		return File{}, err
	}
	return GoCodeAST("internal/provider/provider.go", f), nil
}

// providerPackageName returns the Go identifier used for the provider struct.
// It is lower-camelCase so the struct remains unexported, e.g. "mycloudProvider".
func providerPackageName(pir ir.ProviderIR) string {
	return naming.CamelCase(pir.Name) + "Provider"
}

// providerModelName returns the Go identifier used for the provider config model
// struct, e.g. "mycloudProviderModel".
func providerModelName(pir ir.ProviderIR) string {
	return naming.CamelCase(pir.Name) + "ProviderModel"
}

// providerTypeName returns the Terraform provider type name. It prefers
// ProviderIR.TypeName and falls back to ProviderIR.Name.
func providerTypeName(pir ir.ProviderIR) string {
	if strings.TrimSpace(pir.TypeName) != "" {
		return strings.TrimSpace(pir.TypeName)
	}
	return strings.TrimSpace(pir.Name)
}

// generateProviderFile builds the *ast.File for internal/provider/provider.go.
// clientImport is the import path of the generated internal/client package; see
// ProviderFileWithClient for how it affects the Configure method.
func generateProviderFile(pir ir.ProviderIR, clientImport string) (*ast.File, error) {
	f := astgen.NewFile("provider")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/provider", "tfframeworkprovider")
	f.AddImports(
		"context",
		"github.com/hashicorp/terraform-plugin-framework/provider/schema",
		"github.com/hashicorp/terraform-plugin-framework/datasource",
		"github.com/hashicorp/terraform-plugin-framework/resource",
		"github.com/hashicorp/terraform-plugin-framework/function",
		"github.com/hashicorp/terraform-plugin-framework/ephemeral",
		"github.com/hashicorp/terraform-plugin-framework/list",
	)
	// The types package is referenced by primitiveAttrType/modelFieldType for the
	// provider config attributes. The generator-owned log_* trace-logging
	// attributes are always emitted (see providerConfigAttributes), so the import
	// is unconditional.
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")

	providerStruct := providerPackageName(pir)
	modelStruct := providerModelName(pir)

	// Interface assertions. List resources in the IR are registered with the
	// framework as data sources; there is no separate provider interface method
	// for them, but the generated provider still exposes ListResources() to
	// satisfy ProviderWithListResources where the framework defines it.
	assertions := []string{
		"Provider",
		"ProviderWithFunctions",
		"ProviderWithEphemeralResources",
		"ProviderWithListResources",
	}
	if len(pir.Actions) > 0 {
		assertions = append(assertions, "ProviderWithActions")
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/action", "")
	}
	for _, iface := range assertions {
		f.AddDecl(astgen.VarDeclGen(astgen.VarSpec(
			"_",
			astgen.QualExpr("tfframeworkprovider", iface),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(providerStruct))),
				astgen.Nil(),
			),
		)))
	}

	// Provider struct.
	f.AddCommentf("%s is the generated Terraform provider implementation.", providerStruct)
	f.AddDecl(astgen.TypeDecl(providerStruct, astgen.StructType(
		astgen.Field("configured", astgen.Ident("bool"), ""),
	)))

	// Provider config model.
	f.AddCommentf("%s describes the provider-level configuration shape.", modelStruct)
	configAttrs := providerConfigAttributes(pir)
	modelFields := make([]*ast.Field, 0, len(configAttrs))
	for _, attr := range configAttrs {
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(attr.Name),
			modelFieldType(attr),
			modelFieldTags(attr),
		))
	}
	f.AddDecl(astgen.TypeDecl(modelStruct, astgen.StructType(modelFields...)))

	schemaValues, err := providerSchemaValues(pir)
	if err != nil {
		return nil, err
	}

	// New constructor.
	f.AddComment("New returns a new instance of the generated provider.")
	f.AddDecl(astgen.FuncDeclFull(
		"New",
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("tfframeworkprovider", "Provider"), "")),
		astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(providerStruct))))),
	))

	// Metadata method.
	f.AddComment("Metadata returns the provider type name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("tfframeworkprovider", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("tfframeworkprovider", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(providerTypeName(pir))},
			token.ASSIGN,
		)),
	))

	// Schema method.
	f.AddComment("Schema returns the provider configuration schema.")
	f.AddDecl(astgen.MethodDecl(
		"Schema", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("tfframeworkprovider", "SchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("tfframeworkprovider", "SchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(
				astgen.QualExpr("schema", "Schema"),
				schemaValues...,
			)},
			token.ASSIGN,
		)),
	))

	// Configure method.
	wireClient := clientImport != "" && (AnyResourceWired(pir.Resources) || AnyDataSourceWired(pir.DataSources) || AnyEphemeralWired(pir.EphemeralResources) || AnyActionWired(pir.Actions) || AnyListResourceWired(pir.ListResources))
	if wireClient {
		f.AddImport(clientImport, "client")
	}
	f.AddComment("Configure decodes practitioner configuration and marks the provider as configured.")
	configureBody := []ast.Stmt{
		astgen.VarDecl("config", modelStruct, nil),
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
			astgen.Block(astgen.Return()),
		),
	}
	if wireClient {
		// Construct the API client and hand it to resources (and future wired
		// data sources) via the framework provider-data mechanism. An endpoint
		// attribute, when declared, overrides the base URL baked into the
		// client from the spec's servers — the generated acceptance tests use
		// it to point the provider at their mock server.
		configureBody = append(configureBody,
			astgen.AssignSingle(astgen.Ident("opts"), astgen.CompositeLit(
				astgen.SliceType(astgen.QualExpr("client", "ClientOption")),
			)),
		)
		if providerHasEndpointAttr(pir) {
			endpoint := astgen.Selector(astgen.Ident("config"), "Endpoint")
			configureBody = append(configureBody, astgen.If(
				astgen.Binary(
					astgen.Unary(token.NOT, astgen.Call(astgen.Selector(endpoint, "IsNull"))),
					token.LAND,
					astgen.Unary(token.NOT, astgen.Call(astgen.Selector(endpoint, "IsUnknown"))),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("opts")},
					[]ast.Expr{astgen.Call(
						astgen.Ident("append"),
						astgen.Ident("opts"),
						astgen.Call(
							astgen.QualExpr("client", "WithBaseURL"),
							astgen.Call(astgen.Selector(endpoint, "ValueString")),
						),
					)},
					token.ASSIGN,
				),
			))
		}
		// Construct request interceptors from the decoded auth config and add
		// them to opts so wired resources authenticate against the API. Only
		// scheme types with a generated interceptor (apiKey, HTTP basic/bearer,
		// OAuth2 client_credentials) contribute here; each interceptor is
		// guarded on its credential being set so an Optional auth attribute left
		// unset does not send an empty credential.
		authStmts, err := authConfigureStmts(pir.SecurityIR.Schemes)
		if err != nil {
			return nil, err
		}
		configureBody = append(configureBody, authStmts...)
		// Wire HTTP trace logging: build a client.LoggingConfig from the
		// generator-owned log_* attributes (over generator.yaml defaults baked
		// from ClientIR.Logging) and attach it when a log file is configured.
		configureBody = append(configureBody, loggingConfigureStmts(pir)...)
		configureBody = append(configureBody,
			astgen.AssignSingle(astgen.Ident("c"), astgen.Call(
				astgen.QualExpr("client", "New"),
				astgen.Ellipsis(astgen.Ident("opts")),
			)),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "DataSourceData")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "ResourceData")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "EphemeralResourceData")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "ActionData")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "ListResourceData")},
				[]ast.Expr{astgen.Ident("c")},
				token.ASSIGN,
			),
		)
	}
	configureBody = append(configureBody, astgen.AssignStmt(
		[]ast.Expr{astgen.Selector(astgen.Ident("p"), "configured")},
		[]ast.Expr{astgen.BoolLit(true)},
		token.ASSIGN,
	))
	f.AddDecl(astgen.MethodDecl(
		"Configure", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("tfframeworkprovider", "ConfigureRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("tfframeworkprovider", "ConfigureResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(configureBody...),
	))

	// DataSources method.
	f.AddComment("DataSources returns the data sources registered with this provider.")
	f.AddDecl(astgen.MethodDecl(
		"DataSources", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
			astgen.Params(),
			astgen.Results(astgen.Field("", astgen.QualExpr("datasource", "DataSource"), "")),
		)), "")),
		dataSourceRegistrationBody(pir.DataSources),
	))

	// Resources method.
	f.AddComment("Resources returns the managed resources registered with this provider.")
	f.AddDecl(astgen.MethodDecl(
		"Resources", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
			astgen.Params(),
			astgen.Results(astgen.Field("", astgen.QualExpr("resource", "Resource"), "")),
		)), "")),
		resourceRegistrationBody(pir.Resources),
	))

	// Actions method.
	if len(pir.Actions) > 0 {
		f.AddComment("Actions returns the actions registered with this provider.")
		f.AddDecl(astgen.MethodDecl(
			"Actions", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
			astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
			astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
				astgen.Params(),
				astgen.Results(astgen.Field("", astgen.QualExpr("action", "Action"), "")),
			)), "")),
			actionRegistrationBody(pir.Actions),
		))
	}

	// Functions method (optional interface).
	f.AddComment("Functions returns the provider-defined functions registered with this provider.")
	f.AddDecl(astgen.MethodDecl(
		"Functions", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
			astgen.Params(),
			astgen.Results(astgen.Field("", astgen.QualExpr("function", "Function"), "")),
		)), "")),
		functionRegistrationBody(pir.Functions),
	))

	// EphemeralResources method (optional interface).
	f.AddComment("EphemeralResources returns the ephemeral resources registered with this provider.")
	f.AddDecl(astgen.MethodDecl(
		"EphemeralResources", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
			astgen.Params(),
			astgen.Results(astgen.Field("", astgen.QualExpr("ephemeral", "EphemeralResource"), "")),
		)), "")),
		ephemeralRegistrationBody(pir.EphemeralResources),
	))

	// ListResources method (optional interface).
	f.AddComment("ListResources returns the list resources registered with this provider.")
	f.AddDecl(astgen.MethodDecl(
		"ListResources", "p", astgen.StarExpr(astgen.Ident(providerStruct)),
		astgen.Params(astgen.Field("_", astgen.QualExpr("context", "Context"), "")),
		astgen.Results(astgen.Field("", astgen.SliceType(astgen.FuncType(
			astgen.Params(),
			astgen.Results(astgen.Field("", astgen.QualExpr("list", "ListResource"), "")),
		)), "")),
		listRegistrationBody(pir.ListResources, pir.Resources),
	))

	return f.AST(), nil
}

// registrationBody returns the method body for a registration method that
// returns nil when the supplied slice is empty and a slice literal of factory
// functions otherwise.
func registrationBody(factoryType ast.Expr, factories []ast.Expr) *ast.BlockStmt {
	if len(factories) == 0 {
		return astgen.Block(astgen.Return(astgen.Nil()))
	}
	return astgen.Block(astgen.Return(astgen.CompositeLit(
		astgen.SliceType(factoryType),
		factories...,
	)))
}

// dataSourceRegistrationBody returns the body for (p *Provider) DataSources().
func dataSourceRegistrationBody(dataSources []ir.DataSourceIR) *ast.BlockStmt {
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("datasource", "DataSource"), "")),
	)
	factories := make([]ast.Expr, 0, len(dataSources))
	for _, ds := range dataSources {
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(dataSourceStructName(ds)))))),
		))
	}
	return registrationBody(factoryType, factories)
}

// resourceRegistrationBody returns the body for (p *Provider) Resources().
func resourceRegistrationBody(resources []ir.ResourceIR) *ast.BlockStmt {
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("resource", "Resource"), "")),
	)
	factories := make([]ast.Expr, 0, len(resources))
	for _, r := range resources {
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(resourceStructName(r)))))),
		))
	}
	return registrationBody(factoryType, factories)
}

// actionRegistrationBody returns the body for (p *Provider) Actions().
func actionRegistrationBody(actions []ir.ActionIR) *ast.BlockStmt {
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("action", "Action"), "")),
	)
	factories := make([]ast.Expr, 0, len(actions))
	for _, a := range actions {
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(actionStructName(a)))))),
		))
	}
	return registrationBody(factoryType, factories)
}

// functionRegistrationBody returns the body for (p *Provider) Functions().
func functionRegistrationBody(functions []ir.FunctionIR) *ast.BlockStmt {
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("function", "Function"), "")),
	)
	factories := make([]ast.Expr, 0, len(functions))
	for _, fn := range functions {
		initElems := []ast.Expr{}
		if strings.TrimSpace(fn.SourceOperation) != "" {
			initElems = append(initElems, astgen.KeyValue("SourceOperation", astgen.Lit(strings.TrimSpace(fn.SourceOperation))))
		}
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(
				astgen.Ident(functionStructName(fn)),
				initElems...,
			)))),
		))
	}
	return registrationBody(factoryType, factories)
}

// ephemeralRegistrationBody returns the body for (p *Provider) EphemeralResources().
func ephemeralRegistrationBody(ephemerals []ir.EphemeralResourceIR) *ast.BlockStmt {
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("ephemeral", "EphemeralResource"), "")),
	)
	factories := make([]ast.Expr, 0, len(ephemerals))
	for _, er := range ephemerals {
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(ephemeralResourceStructName(er)))))),
		))
	}
	return registrationBody(factoryType, factories)
}

// listRegistrationBody returns the body for (p *Provider) ListResources().
//
// The framework requires every registered ListResource type name to match a
// managed Resource type name; a list resource whose type name has no matching
// managed resource fails the whole provider schema load (G12). Only list
// resources that pair with a managed resource are registered.
func listRegistrationBody(listResources []ir.ListResourceIR, resources []ir.ResourceIR) *ast.BlockStmt {
	managed := make(map[string]struct{}, len(resources))
	for _, r := range resources {
		managed[r.TypeName] = struct{}{}
	}
	factoryType := astgen.FuncType(
		astgen.Params(),
		astgen.Results(astgen.Field("", astgen.QualExpr("list", "ListResource"), "")),
	)
	factories := make([]ast.Expr, 0, len(listResources))
	for _, lr := range listResources {
		if _, ok := managed[lr.TypeName]; !ok {
			continue
		}
		factories = append(factories, astgen.FuncLit(
			factoryType,
			astgen.Block(astgen.Return(astgen.UnaryPtr(astgen.CompositeLit(astgen.Ident(listResourceStructName(lr)))))),
		))
	}
	return registrationBody(factoryType, factories)
}

// validateProviderConfig rejects provider configuration schemas that would
// generate invalid plugin-framework provider schemas. Provider config
// attributes cannot be Computed or WriteOnly, and nested blocks must use a
// supported nesting mode.
//
// The function is recursive: every nested block's schema is validated in full,
// so any attribute anywhere in the provider config block tree is checked for
// Computed/WriteOnly and any block is checked for a supported nesting mode.
func validateProviderConfig(cfg ir.ObjectSchemaIR) error {
	for _, attr := range cfg.Attributes {
		if attr.Computed {
			return fmt.Errorf("provider config attribute %q cannot be Computed", attr.Name)
		}
		if attr.WriteOnly {
			return fmt.Errorf("provider config attribute %q cannot be WriteOnly", attr.Name)
		}
		if err := validateProviderSchema(attr.Schema, "provider config attribute "+attr.Name); err != nil {
			return err
		}
	}
	for _, block := range cfg.Blocks {
		switch block.NestingMode {
		case ir.NestingSingle, ir.NestingList, ir.NestingSet:
			// supported
		default:
			return fmt.Errorf("provider config block %q has unsupported nesting mode %q", block.Name, block.NestingMode)
		}
		if err := validateProviderConfig(block.Schema); err != nil {
			return fmt.Errorf("provider config block %q: %w", block.Name, err)
		}
	}
	return nil
}

// validateProviderSchema checks that a schema used inside a provider config
// attribute can be mapped to a model field type and a framework attribute.
// It does not itself enforce the provider-config-only Computed/WriteOnly
// constraints; validateProviderConfig applies those at every recursion level
// (direct attributes, nested blocks, and object-like collection elements).
func validateProviderSchema(s ir.SchemaIR, ctx string) error {
	if s.Collection != nil {
		return validateProviderCollectionSchema(s, ctx)
	}
	if schema.IsObjectLike(s) {
		return validateProviderObjectSchema(s, ctx)
	}
	return validateProviderPrimitiveType(s, ctx)
}

func validateProviderCollectionSchema(s ir.SchemaIR, ctx string) error {
	switch s.Collection.Kind {
	case ir.List, ir.Set, ir.Map:
		if err := validateProviderSchema(s.Collection.ElementType, ctx+" collection element"); err != nil {
			return err
		}
		// Object-like collection elements can carry provider-config attributes,
		// so enforce the same Computed/WriteOnly constraints recursively.
		if schema.IsObjectLike(s.Collection.ElementType) {
			elem := s.Collection.ElementType
			if err := validateProviderConfig(ir.ObjectSchemaIR{
				Attributes: elem.Attributes,
				Blocks:     elem.Blocks,
			}); err != nil {
				return fmt.Errorf("%s collection element: %w", ctx, err)
			}
		}
	default:
		return fmt.Errorf("%s has unsupported collection kind %q", ctx, s.Collection.Kind)
	}
	return nil
}

func validateProviderObjectSchema(s ir.SchemaIR, ctx string) error {
	for _, attr := range s.Attributes {
		if err := validateProviderSchema(attr.Schema, ctx+" attribute "+attr.Name); err != nil {
			return err
		}
	}
	for _, block := range s.Blocks {
		switch block.NestingMode {
		case ir.NestingSingle, ir.NestingList, ir.NestingSet:
			// supported
		default:
			return fmt.Errorf("%s block %q has unsupported nesting mode %q", ctx, block.Name, block.NestingMode)
		}
		if err := validateProviderConfig(block.Schema); err != nil {
			return fmt.Errorf("%s block %q: %w", ctx, block.Name, err)
		}
	}
	return nil
}

func validateProviderPrimitiveType(s ir.SchemaIR, ctx string) error {
	switch s.Type {
	case ir.TypeString, ir.TypeInt, ir.TypeFloat, ir.TypeBool, ir.TypeDynamic:
		return nil
	case ir.TypeNull:
		return fmt.Errorf("%s has unsupported primitive type %q", ctx, s.Type)
	default:
		if s.Type == "" {
			return fmt.Errorf("%s has no recognizable type", ctx)
		}
		return fmt.Errorf("%s has unsupported primitive type %q", ctx, s.Type)
	}
}

// providerSchemaValues builds the []ast.Expr of KeyValueExpr for schema.Schema{...}.
func providerSchemaValues(pir ir.ProviderIR) ([]ast.Expr, error) {
	elems := []ast.Expr{}

	if pir.Description != "" {
		elems = append(elems, astgen.KeyValue("Description", astgen.Lit(strings.TrimSpace(pir.Description))))
	}

	attrs := providerConfigAttributes(pir)
	blocks := pir.ConfigSchema.Blocks

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				providerAttributeExprWithPath(attr, ""),
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
			expr, err := providerBlockExpr(block, "")
			if err != nil {
				return nil, err
			}
			blockElems = append(blockElems, astgen.KeyValueExpr(astgen.Lit(block.Name), expr))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Block")),
			blockElems...,
		)))
	}

	return elems, nil
}

// loggingAttributes returns the generator-owned log_* provider attributes that
// expose the generated client's HTTP trace logging (internal/client/logging.go)
// to practitioners. All are Optional; capture flags default to false so request
// and response bodies are never written to disk unless explicitly enabled.
func loggingAttributes() []ir.AttributeIR {
	boolAttr := func(name, desc string) ir.AttributeIR {
		return ir.AttributeIR{Name: name, Optional: true, Description: desc, Schema: ir.SchemaIR{Type: ir.TypeBool}}
	}
	return []ir.AttributeIR{
		{
			Name:        "log_file",
			Optional:    true,
			Description: "Path to a file that receives HTTP request/response trace logs. When unset, trace logging is disabled.",
			Schema:      ir.SchemaIR{Type: ir.TypeString},
		},
		boolAttr("log_capture_request_headers", "Capture request headers in the trace log. Sensitive headers are redacted."),
		boolAttr("log_capture_request_body", "Capture request bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk."),
		boolAttr("log_capture_response_headers", "Capture response headers in the trace log. Sensitive headers are redacted."),
		boolAttr("log_capture_response_body", "Capture response bodies in the trace log. Disabled by default to avoid writing sensitive payloads to disk."),
		{
			Name:        "log_max_body_bytes",
			Optional:    true,
			Description: "Maximum number of body bytes captured per log entry before truncation. Defaults to 4096.",
			Schema:      ir.SchemaIR{Type: ir.TypeInt},
		},
	}
}

// providerConfigAttributes returns the provider config attributes: the IR's
// declared attributes plus the generator-owned log_* trace-logging attributes.
// A declared attribute whose name collides with a log_* name wins — the
// logging attribute is skipped so the spec's own schema stays authoritative —
// and loggingConfigureStmts skips the colliding field for the same reason (its
// declared type may not match what the logging wiring reads).
func providerConfigAttributes(pir ir.ProviderIR) []ir.AttributeIR {
	declared := make(map[string]bool, len(pir.ConfigSchema.Attributes))
	for _, a := range pir.ConfigSchema.Attributes {
		declared[a.Name] = true
	}
	attrs := make([]ir.AttributeIR, 0, len(pir.ConfigSchema.Attributes)+len(loggingAttributes()))
	attrs = append(attrs, pir.ConfigSchema.Attributes...)
	for _, la := range loggingAttributes() {
		if declared[la.Name] {
			continue
		}
		attrs = append(attrs, la)
	}
	return attrs
}

// loggingConfigureStmts builds the Configure statements that construct a
// client.LoggingConfig from the log_* model fields — seeded with the
// generator.yaml logging defaults baked into ClientIR.Logging — and append
// client.WithLogging to opts when a log file is configured. The generated
// client's New also guards on LogFile != "", so the append is doubly safe. A
// declared config attribute colliding with log_file disables the whole block:
// without a known string log_file field there is nothing to wire.
func loggingConfigureStmts(pir ir.ProviderIR) []ast.Stmt {
	declared := make(map[string]bool, len(pir.ConfigSchema.Attributes))
	for _, a := range pir.ConfigSchema.Attributes {
		declared[a.Name] = true
	}
	if declared["log_file"] {
		return nil
	}

	loggingConfig := astgen.Ident("loggingConfig")

	// Seed the literal with the baked generator.yaml defaults; practitioner
	// attributes override them below when set. Field order is fixed so output
	// is deterministic.
	litElems := []ast.Expr{}
	if l := pir.ClientIR.Logging; l != nil {
		if l.LogFile != "" {
			litElems = append(litElems, astgen.KeyValue("LogFile", astgen.Lit(l.LogFile)))
		}
		if l.CaptureRequestHeaders {
			litElems = append(litElems, astgen.KeyValue("CaptureRequestHeaders", astgen.BoolLit(true)))
		}
		if l.CaptureRequestBody {
			litElems = append(litElems, astgen.KeyValue("CaptureRequestBody", astgen.BoolLit(true)))
		}
		if l.CaptureResponseHeaders {
			litElems = append(litElems, astgen.KeyValue("CaptureResponseHeaders", astgen.BoolLit(true)))
		}
		if l.CaptureResponseBody {
			litElems = append(litElems, astgen.KeyValue("CaptureResponseBody", astgen.BoolLit(true)))
		}
		if l.MaxBodyBytes > 0 {
			litElems = append(litElems, astgen.KeyValue("MaxBodyBytes", astgen.IntLit(l.MaxBodyBytes)))
		}
		if len(l.RedactHeaders) > 0 {
			redact := make([]ast.Expr, 0, len(l.RedactHeaders))
			for _, h := range l.RedactHeaders {
				redact = append(redact, astgen.Lit(h))
			}
			litElems = append(litElems, astgen.KeyValue("RedactHeaders", astgen.CompositeLit(
				astgen.SliceType(astgen.Ident("string")), redact...,
			)))
		}
	}

	stmts := []ast.Stmt{
		astgen.AssignSingle(loggingConfig, astgen.CompositeLit(
			astgen.QualExpr("client", "LoggingConfig"), litElems...,
		)),
	}

	// override emits `if !config.<ModelField>.IsNull() && !config.<ModelField>.IsUnknown()
	// { loggingConfig.<TargetField> = <value> }` for a practitioner attribute,
	// unless the provider declares a colliding config attribute of its own.
	override := func(attrName, targetField string, value func(configField ast.Expr) ast.Expr) {
		if declared[attrName] {
			return
		}
		configField := astgen.Selector(astgen.Ident("config"), naming.GoFieldName(attrName))
		stmts = append(stmts, astgen.If(
			astgen.Binary(
				astgen.Unary(token.NOT, astgen.Call(astgen.Selector(configField, "IsNull"))),
				token.LAND,
				astgen.Unary(token.NOT, astgen.Call(astgen.Selector(configField, "IsUnknown"))),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(loggingConfig, targetField)},
				[]ast.Expr{value(configField)},
				token.ASSIGN,
			),
		))
	}
	override("log_file", "LogFile", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Selector(c, "ValueString"))
	})
	override("log_capture_request_headers", "CaptureRequestHeaders", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Selector(c, "ValueBool"))
	})
	override("log_capture_request_body", "CaptureRequestBody", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Selector(c, "ValueBool"))
	})
	override("log_capture_response_headers", "CaptureResponseHeaders", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Selector(c, "ValueBool"))
	})
	override("log_capture_response_body", "CaptureResponseBody", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Selector(c, "ValueBool"))
	})
	override("log_max_body_bytes", "MaxBodyBytes", func(c ast.Expr) ast.Expr {
		return astgen.Call(astgen.Ident("int"), astgen.Call(astgen.Selector(c, "ValueInt64")))
	})

	stmts = append(stmts, astgen.If(
		astgen.Binary(astgen.Selector(loggingConfig, "LogFile"), token.NEQ, astgen.Lit("")),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("opts")},
			[]ast.Expr{astgen.Call(
				astgen.Ident("append"),
				astgen.Ident("opts"),
				astgen.Call(astgen.QualExpr("client", "WithLogging"), loggingConfig),
			)},
			token.ASSIGN,
		),
	))
	return stmts
}

// providerAttributeExprWithPath returns an ast.Expr for a provider/schema
// Attribute, tracking the dotted parent path so that unsupported nested
// attributes can be reported with their full location.
func providerAttributeExprWithPath(attr ir.AttributeIR, parentPath string) ast.Expr {
	path := fullAttrPath(parentPath, attr.Name)
	expr := frameworkAttributeExpr(attr, path)
	if expr == nil {
		expr = astgen.CompositeLit(astgen.QualExpr("schema", "StringAttribute"))
	}
	return expr
}

// frameworkAttributeExpr maps an IR attribute to a Terraform Plugin Framework
// provider/schema attribute expression. attrPath is the dotted path to the
// current attribute and is propagated to nested attribute maps.
func frameworkAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	s := attr.Schema

	// Collection types.
	if s.Collection != nil {
		elem := schema.DynamicUnionElement(s.Collection.ElementType)
		// A collection whose element is, or contains at any depth, a dynamic
		// type cannot be rendered as a typed framework collection: the
		// terraform-plugin-framework rejects any collection whose element type
		// contains a dynamic type (fwtype.ContainsCollectionWithDynamic). Emit
		// the whole collection as a DynamicAttribute instead, per the
		// framework's own guidance. An enclosing collection's
		// ContainsNestedDynamic check promotes any collection ancestor, so this
		// DynamicAttribute is only reached in an object-or-top-level context
		// where it is valid.
		if schema.ContainsNestedDynamic(elem) {
			return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), attributeValues(attr, nil)...)
		}
		switch s.Collection.Kind {
		case ir.List:
			if schema.IsPrimitiveSchema(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
				}), attr, "List")
				return astgen.CompositeLit(astgen.QualExpr("schema", "ListAttribute"), d...)
			}
			if schema.IsObjectLike(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("NestedObject", astgen.CompositeLit(
						astgen.QualExpr("schema", "NestedAttributeObject"),
						astgen.KeyValue("Attributes", nestedAttributesMapFromSchema(elem, attrPath)),
					)),
				}), attr, "Object")
				return astgen.CompositeLit(astgen.QualExpr("schema", "ListNestedAttribute"), d...)
			}
		case ir.Set:
			if schema.IsPrimitiveSchema(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
				}), attr, "Set")
				return astgen.CompositeLit(astgen.QualExpr("schema", "SetAttribute"), d...)
			}
			if schema.IsObjectLike(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("NestedObject", astgen.CompositeLit(
						astgen.QualExpr("schema", "NestedAttributeObject"),
						astgen.KeyValue("Attributes", nestedAttributesMapFromSchema(elem, attrPath)),
					)),
				}), attr, "Object")
				return astgen.CompositeLit(astgen.QualExpr("schema", "SetNestedAttribute"), d...)
			}
		case ir.Map:
			if schema.IsPrimitiveSchema(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
				}), attr, "Map")
				return astgen.CompositeLit(astgen.QualExpr("schema", "MapAttribute"), d...)
			}
			if schema.IsObjectLike(elem) {
				d := schema.AddValidators(attributeValues(attr, []ast.Expr{
					astgen.KeyValue("NestedObject", astgen.CompositeLit(
						astgen.QualExpr("schema", "NestedAttributeObject"),
						astgen.KeyValue("Attributes", nestedAttributesMapFromSchema(elem, attrPath)),
					)),
				}), attr, "Object")
				return astgen.CompositeLit(astgen.QualExpr("schema", "MapNestedAttribute"), d...)
			}
		}
	}

	// Primitive types.
	if s.Type != "" {
		switch s.Type {
		case ir.TypeString:
			d := schema.AddValidators(attributeValues(attr, nil), attr, "String")
			return astgen.CompositeLit(astgen.QualExpr("schema", "StringAttribute"), d...)
		case ir.TypeInt:
			d := schema.AddValidators(attributeValues(attr, nil), attr, "Int64")
			return astgen.CompositeLit(astgen.QualExpr("schema", "Int64Attribute"), d...)
		case ir.TypeFloat:
			d := schema.AddValidators(attributeValues(attr, nil), attr, "Float64")
			return astgen.CompositeLit(astgen.QualExpr("schema", "Float64Attribute"), d...)
		case ir.TypeBool:
			d := schema.AddValidators(attributeValues(attr, nil), attr, "Bool")
			return astgen.CompositeLit(astgen.QualExpr("schema", "BoolAttribute"), d...)
		case ir.TypeDynamic:
			return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), attributeValues(attr, nil)...)
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework provider schema has no first-class union attribute. When
	// a schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := attributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", nestedAttributesMapFromSchema(*merged, attrPath)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), attributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := schema.AddValidators(attributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", nestedAttributesMapFromSchema(s, attrPath)),
		}), attr, "Object")
		return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
	}

	return nil
}

// providerBlockExpr returns an ast.Expr for a provider/schema Block.
// parentPath is the dotted path of the enclosing attribute and is propagated to
// the block's nested attribute maps.
func providerBlockExpr(block ir.BlockIR, parentPath string) (ast.Expr, error) {
	var kind string
	switch block.NestingMode {
	case ir.NestingSingle:
		kind = "SingleNestedBlock"
	case ir.NestingList:
		kind = "ListNestedBlock"
	case ir.NestingSet:
		kind = "SetNestedBlock"
	default:
		return nil, fmt.Errorf("unsupported provider config block nesting mode %q for block %q", block.NestingMode, block.Name)
	}

	path := fullAttrPath(parentPath, block.Name)
	elems := []ast.Expr{
		astgen.KeyValue("Attributes", nestedAttributesMap(block.Schema, path)),
	}
	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	}

	return astgen.CompositeLit(astgen.QualExpr("schema", kind), elems...), nil
}

// attributeValues builds the common field dictionary for an attribute.
func attributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
	elems := []ast.Expr{}

	if attr.MarkdownDescription != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.MarkdownDescription)))
	} else if attr.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(attr.Description)))
	}

	// Provider config attributes have already been validated as neither Computed
	// nor WriteOnly; emit Required/Optional/Sensitive only.
	if attr.Required {
		elems = append(elems, astgen.KeyValue("Required", astgen.BoolLit(true)))
	} else {
		elems = append(elems, astgen.KeyValue("Optional", astgen.BoolLit(true)))
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

// nestedAttributesMap returns map[string]schema.Attribute{...} for the given
// object schema. parentPath is propagated to nested attribute and block
// expressions so panics can report the full dotted location.
func nestedAttributesMap(s ir.ObjectSchemaIR, parentPath string) ast.Expr {
	elemExprs := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		elemExprs = append(elemExprs, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			providerAttributeExprWithPath(attr, parentPath),
		))
	}
	return astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Attribute")),
		elemExprs...,
	)
}

// nestedAttributesMapFromSchema converts a SchemaIR object-like value to a nested
// attributes map expression. It is used for collection element types that are
// represented as SchemaIR rather than ObjectSchemaIR. parentPath is propagated
// to nested attribute and block expressions so panics can report the full
// dotted location.
func nestedAttributesMapFromSchema(s ir.SchemaIR, parentPath string) ast.Expr {
	return nestedAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, parentPath)
}

// objectSchemaNeedsValidators reports whether any attribute or nested block in
// the object schema emits a Validators field. It is used to decide whether the
// generated file needs the schema/validator package import.
func objectSchemaNeedsValidators(s ir.ObjectSchemaIR) bool {
	for _, attr := range s.Attributes {
		if schemaIRNeedsValidators(attr.Schema) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if blockNeedsValidators(block) {
			return true
		}
	}
	return false
}

// schemaIRNeedsValidators reports whether the given schema emits a Validators
// field when rendered as a Terraform plugin-framework attribute. It mirrors the
// render-kind decision in the framework attribute renderers and the per-kind
// validator expr functions in validators.go, so the schema/validator import is
// registered only when a validator.<Kind> slice is actually emitted.
//
// Kinds that never emit validators (String, Bool, List, Set, Dynamic) return
// false. Notably, union schemas render as DynamicAttribute with no Validators,
// so a Union.Discriminator never emits a validator here (discriminator
// validation is currently dropped — see H-9); and PatternProperties only emits
// when the schema renders as a Map attribute (Map-of-primitive).
func schemaIRNeedsValidators(s ir.SchemaIR) bool {
	if schemaEmitsValidators(s) {
		return true
	}
	// Object-like collection elements (List/Set/Map of object) render their
	// nested attributes in a map, each of which may emit validators. Primitive
	// collection elements are rendered as bare type references with no
	// validators, so they are not recursed.
	if s.Collection != nil && schema.IsObjectLike(s.Collection.ElementType) {
		if objectSchemaNeedsValidators(ir.ObjectSchemaIR{
			Attributes: s.Collection.ElementType.Attributes,
			Blocks:     s.Collection.ElementType.Blocks,
		}) {
			return true
		}
	}
	if schema.IsObjectLike(s) {
		return objectSchemaNeedsValidators(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})
	}
	return false
}

// schemaEmitsValidators reports whether the schema, rendered as a single
// framework attribute, emits a non-empty Validators slice that references the
// schema/validator package. The kind is determined by attributeValidatorKind,
// which mirrors the framework attribute renderers; the per-kind emission check
// delegates to the validator expr functions in validators.go.
func schemaEmitsValidators(s ir.SchemaIR) bool {
	switch attributeValidatorKind(s) {
	case "Int64":
		return len(schema.Int64ValidatorExprs(s)) > 0
	case "Float64":
		return len(schema.Float64ValidatorExprs(s)) > 0
	case "Object":
		return len(schema.ObjectValidatorExprs(s)) > 0
	case "Map":
		return len(schema.MapValidatorExprs(s)) > 0
	}
	return false
}

// attributeValidatorKind returns the validator "kind" string that the framework
// attribute renderers pass to addValidators for the given schema, mirroring the
// render-kind decision in frameworkResourceAttributeExpr and its siblings. The
// kind determines which per-kind validator expr function (if any) is consulted.
// Kinds that never emit validators (String, Bool, List, Set, Dynamic) are still
// returned so callers can distinguish them from unrendered schemas ("").
func attributeValidatorKind(s ir.SchemaIR) string {
	if s.Collection != nil {
		elem := s.Collection.ElementType
		switch s.Collection.Kind {
		case ir.List:
			if schema.IsPrimitiveSchema(elem) {
				return "List"
			}
			if schema.IsObjectLike(elem) {
				return "Object"
			}
		case ir.Set:
			if schema.IsPrimitiveSchema(elem) {
				return "Set"
			}
			if schema.IsObjectLike(elem) {
				return "Object"
			}
		case ir.Map:
			if schema.IsPrimitiveSchema(elem) {
				return "Map"
			}
			if schema.IsObjectLike(elem) {
				return "Object"
			}
		}
	}
	if s.Type != "" {
		switch s.Type {
		case ir.TypeString:
			return "String"
		case ir.TypeInt:
			return "Int64"
		case ir.TypeFloat:
			return "Float64"
		case ir.TypeBool:
			return "Bool"
		case ir.TypeDynamic:
			return "Dynamic"
		}
	}
	if s.Union != nil {
		// A discriminated union renders as a SingleNestedAttribute via the
		// dynamic-union strategy (D2), so its validators attach through the
		// "Object" path; any other union renders as DynamicAttribute with no
		// validators.
		if schema.MergedDiscriminatedUnion(s) != nil {
			return "Object"
		}
		return "Dynamic"
	}
	if schema.IsObjectLike(s) {
		return "Object"
	}
	return ""
}

// blockNeedsValidators reports whether a nested block emits block-size validators
// or contains attributes/blocks that emit validators.
func blockNeedsValidators(block ir.BlockIR) bool {
	if block.MinItems != nil || block.MaxItems != nil {
		return true
	}
	return objectSchemaNeedsValidators(block.Schema)
}

// blockValidatorPackageImports returns which framework-validators subpackages are
// needed by blocks in the schema (listvalidator for list blocks, setvalidator for
// set blocks) because of MinItems/MaxItems constraints.
func blockValidatorPackageImports(s ir.ObjectSchemaIR) (needsList, needsSet bool) {
	for _, block := range s.Blocks {
		if block.MinItems != nil || block.MaxItems != nil {
			switch block.NestingMode {
			case ir.NestingList:
				needsList = true
			case ir.NestingSet:
				needsSet = true
			}
		}
		nl, ns := blockValidatorPackageImports(block.Schema)
		if nl {
			needsList = true
		}
		if ns {
			needsSet = true
		}
	}
	return
}

// objectSchemaNeedsBlockSizeValidators reports whether any block at any depth in
// the object schema has a MinItems/MaxItems constraint, which is the only
// condition under which the schema/validator package is referenced by block
// size validators ([]validator.List / []validator.Set). It is used by renderers
// whose attributes never emit per-attribute validators (datasource, ephemeral)
// to gate the schema/validator import precisely, avoiding "imported and not
// used" when only attributes (not blocks) carry validator-bearing constraints.
func objectSchemaNeedsBlockSizeValidators(s ir.ObjectSchemaIR) bool {
	for _, block := range s.Blocks {
		if block.MinItems != nil || block.MaxItems != nil {
			return true
		}
		if objectSchemaNeedsBlockSizeValidators(block.Schema) {
			return true
		}
	}
	return false
}

// primitiveAttrType maps an IR primitive type to its Terraform Plugin Go attr.Type.
// Unrecognized types fall back to types.StringType; callers that need strict
// validation should check the type before calling this function.
func primitiveAttrType(t ir.PrimitiveType) ast.Expr {
	switch t {
	case ir.TypeString:
		return astgen.QualExpr("types", "StringType")
	case ir.TypeInt:
		return astgen.QualExpr("types", "Int64Type")
	case ir.TypeFloat:
		return astgen.QualExpr("types", "Float64Type")
	case ir.TypeBool:
		return astgen.QualExpr("types", "BoolType")
	case ir.TypeDynamic:
		return astgen.QualExpr("types", "DynamicType")
	}
	// Fallback for unrecognized primitive types. This is an intentional
	// guardrail: validated IRs should never reach it, but emitting a stable
	// string type keeps generated providers compilable if an unexpected type
	// variant appears.
	return astgen.QualExpr("types", "StringType")
}

// modelFieldType maps an IR attribute to its Terraform Plugin Framework model field type.
// Unrecognized types fall back to types.String; callers that need strict validation
// should check the type before calling this function.
func modelFieldType(attr ir.AttributeIR) ast.Expr {
	s := attr.Schema

	// Union types and TypeNull are represented in the model as Dynamic values
	// because the model cannot express a static type for them — except a
	// discriminated union, which renders as a SingleNestedAttribute (D2) and
	// therefore needs a types.Object model field.
	if s.Union != nil || s.Type == ir.TypeNull {
		if s.Union != nil && schema.MergedDiscriminatedUnion(s) != nil {
			return astgen.QualExpr("types", "Object")
		}
		return astgen.QualExpr("types", "Dynamic")
	}

	if s.Collection != nil {
		// The schema generator degrades a collection to a DynamicAttribute when
		// its element cannot be rendered as a typed framework collection: when
		// the element is a union (collapsed to dynamic via DynamicUnionElement),
		// is dynamic/null, is a nested collection (the list-element builder
		// cannot render list-of-list, so the schema falls back to
		// DynamicAttribute via G2), or contains a dynamic at any depth
		// (fwtype.ContainsCollectionWithDynamic rejects it). The model field
		// type must track that predicate exactly so the tfsdk Go field type
		// matches the schema attribute type — a types.List field under a
		// DynamicAttribute fails state decoding at runtime. Mirrors
		// dataSourceCollectionAttributeExpr and its resource/ephemeral/action
		// peers, which use DynamicUnionElement + ContainsNestedDynamic.
		elem := schema.DynamicUnionElement(s.Collection.ElementType)
		if elem.Collection != nil || schema.ContainsNestedDynamic(elem) {
			return astgen.QualExpr("types", "Dynamic")
		}
		switch s.Collection.Kind {
		case ir.List:
			return astgen.QualExpr("types", "List")
		case ir.Set:
			return astgen.QualExpr("types", "Set")
		case ir.Map:
			return astgen.QualExpr("types", "Map")
		}
	}

	if schema.IsObjectLike(s) {
		return astgen.QualExpr("types", "Object")
	}

	switch s.Type {
	case ir.TypeString:
		return astgen.QualExpr("types", "String")
	case ir.TypeInt:
		return astgen.QualExpr("types", "Int64")
	case ir.TypeFloat:
		return astgen.QualExpr("types", "Float64")
	case ir.TypeBool:
		return astgen.QualExpr("types", "Bool")
	case ir.TypeDynamic:
		return astgen.QualExpr("types", "Dynamic")
	}

	// Fallback for unrecognized model field types. This is an intentional
	// guardrail: validated IRs should never reach it, but emitting a stable
	// string type keeps generated providers compilable if an unexpected type
	// variant (for example, a future IR schema kind) appears.
	return astgen.QualExpr("types", "String")
}

// modelFieldTags returns the struct tags for a model field: the tfsdk tag
// (the Terraform attribute name) plus a json tag carrying the wire name when it
// differs, so the JSON converter can emit/read the API's original property name
// (G18).
func modelFieldTags(attr ir.AttributeIR) string {
	tags := fmt.Sprintf("tfsdk:%q", attr.Name)
	if attr.WireName != "" && attr.WireName != attr.Name {
		tags += fmt.Sprintf(" json:%q", attr.WireName)
	}
	return tags
}

// resourceStructName returns the generated resource struct name for an IR resource.
func resourceStructName(r ir.ResourceIR) string {
	return naming.PascalCase(r.Name) + "Resource"
}

// dataSourceStructName returns the generated data source struct name.
func dataSourceStructName(ds ir.DataSourceIR) string {
	return naming.PascalCase(ds.Name) + "DataSource"
}

// functionStructName returns the generated function struct name.
func functionStructName(fn ir.FunctionIR) string {
	return naming.PascalCase(fn.Name) + "Function"
}

// ephemeralResourceStructName returns the generated ephemeral resource struct name.
func ephemeralResourceStructName(er ir.EphemeralResourceIR) string {
	return naming.PascalCase(er.Name) + "EphemeralResource"
}

// listResourceStructName returns the generated list resource struct name.
func listResourceStructName(lr ir.ListResourceIR) string {
	return naming.PascalCase(lr.Name) + "ListResource"
}

// litOrOmit returns the trimmed string as an ast.Expr, or nil when the input
// is empty. Callers should only add the returned value to an element slice when
// it is non-nil so empty strings are omitted from generated values.
func litOrOmit(s string) ast.Expr {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return astgen.Lit(strings.TrimSpace(s))
}
