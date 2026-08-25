package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ErrUnknownResourceBlockNesting is the sentinel returned (via panic, then
// recovered into an ErrorFile by ResourceFile) when a resource block has a
// NestingMode the generator does not recognize. Resource blocks fail closed —
// like the provider-config equivalent in provider.go, which returns an error
// for an unsupported nesting mode — rather than silently degrading to
// SingleNestedBlock, so an unexpected IR shape is surfaced instead of
// producing a wrong schema (L-52).
var ErrUnknownResourceBlockNesting = errors.New("unknown resource block nesting mode")

// ResourceFile returns the generated internal/provider/resource_<name>.go file for
// a single Terraform managed resource built from the supplied ResourceIR.
// clientImport is the import path of the generated internal/client package,
// used by resources whose CRUD mapping is complete enough to wire their
// Create/Read/Update/Delete bodies to the generated API client.
func ResourceFile(r ir.ResourceIR, clientImport string) File {
	path := fmt.Sprintf("internal/provider/resource_%s.go", naming.SnakeCase(r.Name))
	file, err := renderEntitySafely(func() (*ast.File, error) {
		return generateResourceFile(r, clientImport)
	})
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ResourceFiles returns the generated resource files for every ResourceIR in the
// provider. Files are emitted in the order the resources are supplied.
// clientImport is the import path of the generated internal/client package.
func ResourceFiles(resources []ir.ResourceIR, clientImport string) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		files = append(files, ResourceFile(r, clientImport))
	}
	return files
}

// generateResourceFile builds the *ast.File for a managed resource file.
func generateResourceFile(r ir.ResourceIR, clientImport string) (*ast.File, error) {
	if err := validateStateUpgrades(r); err != nil {
		return nil, err
	}
	if err := validateImportIDFormat(r); err != nil {
		return nil, err
	}

	f := astgen.NewFile("provider")

	structName := resourceStructName(r)
	modelName := resourceModelName(r)
	wiring := planResourceWiring(r)

	f.AddComment("Compile-time interface assertions.")
	f.AddDecl(astgen.VarDeclGen(resourceAssertSpecs(r, wiring, structName)...))

	// Resource struct. Wired resources carry the API client supplied by the
	// provider's Configure method via the framework provider-data mechanism.
	f.AddCommentf("%s is the generated Terraform managed resource implementation.", structName)
	if wiring.wired {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType(
			astgen.Field("client", astgen.StarExpr(astgen.QualExpr("client", "Client")), ""),
		)))
	} else {
		f.AddDecl(astgen.TypeDecl(structName, astgen.StructType()))
	}

	// Resource model.
	f.AddCommentf("%s describes the Terraform state and plan shape for %s.", modelName, structName)
	modelFields := make([]*ast.Field, 0, len(r.Schema.Attributes)+len(r.Schema.Blocks))
	for _, attr := range r.Schema.Attributes {
		if schema.SkipAttrForModel(attr) {
			continue
		}
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(attr.Name),
			modelFieldType(attr),
			modelFieldTags(attr),
		))
	}
	// Blocks are always materialized in the model struct because they carry
	// Terraform state; skipAttrForModel is a no-op today but is not applied to
	// blocks because there is no corresponding skipBlockForModel helper yet.
	for _, block := range r.Schema.Blocks {
		modelFields = append(modelFields, astgen.Field(
			naming.GoFieldName(block.Name),
			blockModelFieldType(block),
			fmt.Sprintf("tfsdk:%q", block.Name),
		))
	}
	f.AddDecl(astgen.TypeDecl(modelName, astgen.StructType(modelFields...)))

	// Metadata method.
	f.AddComment("Metadata returns the resource type name.")
	f.AddDecl(astgen.MethodDecl(
		"Metadata", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("resource", "MetadataRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "MetadataResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "TypeName")},
			[]ast.Expr{astgen.Lit(resourceTypeName(r))},
			token.ASSIGN,
		)),
	))

	// Schema method.
	f.AddComment("Schema returns the Terraform schema for this resource.")
	schemaValues := resourceSchemaValues(r)
	f.AddDecl(astgen.MethodDecl(
		"Schema", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("_", astgen.QualExpr("resource", "SchemaRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "SchemaResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "Schema")},
			[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("schema", "Schema"), schemaValues...)},
			token.ASSIGN,
		)),
	))

	// IdentitySchema method. A managed resource paired with a list resource
	// (shared type name) carries the list resource's identity schema so
	// terraform query can type the identities the list streams. Without this
	// method terraform query fails with "Identity schema not found for resource
	// type". Only emitted when the resource has identity attributes, so the
	// common case (no paired list resource) is unaffected.
	if resourceHasIdentity(r) {
		f.AddComment("IdentitySchema returns the resource identity schema shared with the paired list resource.")
		f.AddDecl(astgen.MethodDecl(
			"IdentitySchema", "r", astgen.StarExpr(astgen.Ident(structName)),
			astgen.Params(
				astgen.Field("_", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("_", astgen.QualExpr("resource", "IdentitySchemaRequest"), ""),
				astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "IdentitySchemaResponse")), ""),
			),
			astgen.Results(),
			astgen.Block(astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("resp"), "IdentitySchema")},
				[]ast.Expr{astgen.CompositeLit(astgen.QualExpr("identityschema", "Schema"), resourceIdentitySchemaValues(r)...)},
				token.ASSIGN,
			)),
		))
	}

	// Create method. Wired resources call the create endpoint and store the API
	// response as state; resources without a complete CRUD mapping keep the
	// honest scaffold body.
	f.AddComment("Create provisions the remote resource and stores the resulting state.")
	createBody := scaffoldCreateBody(r, modelName)
	if wiring.wired {
		createBody = wiredCreateBody(r, modelName)
	}
	f.AddDecl(astgen.MethodDecl(
		"Create", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("resource", "CreateRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "CreateResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(createBody...),
	))
	if wiring.wired {
		f.AddComment("createRemote performs the create HTTP exchange and decodes the response into plan. Extracted from Create so the request/response logic is unit-testable without a tfsdk.Plan.")
		f.AddDecl(wiredCreateHelperDecl(r, wiring, modelName, structName))
	}

	// Read method.
	f.AddComment("Read refreshes the Terraform state with the latest remote values.")
	readBody := scaffoldReadBody(modelName)
	if wiring.wired {
		readBody = wiredReadBody(r, modelName)
	}
	f.AddDecl(astgen.MethodDecl(
		"Read", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("resource", "ReadRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "ReadResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(readBody...),
	))
	if wiring.wired {
		f.AddComment("readRemote performs the read HTTP exchange and decodes the response into state, returning removed=true when the API reports 404. Extracted from Read so the request/response logic is unit-testable without a tfsdk.State.")
		f.AddDecl(wiredReadHelperDecl(r, wiring, modelName, structName))
	}

	// Update method. When the API exposes no update operation the method keeps
	// its honest scaffold body even on an otherwise wired resource.
	f.AddComment("Update modifies the remote resource to match the desired plan.")
	updateBody := scaffoldUpdateBody(r, modelName)
	if wiring.wired && wiring.update {
		updateBody = wiredUpdateBody(r, modelName)
	}
	f.AddDecl(astgen.MethodDecl(
		"Update", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("resource", "UpdateRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "UpdateResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(updateBody...),
	))
	if wiring.wired && wiring.update {
		f.AddComment("updateRemote performs the update HTTP exchange and decodes the response into plan. Extracted from Update so the request/response logic is unit-testable without a tfsdk.Plan.")
		f.AddDecl(wiredUpdateHelperDecl(r, wiring, modelName, structName))
	}

	// Delete method. Wired resources call the delete endpoint, treating an HTTP
	// 404 as already deleted; resources without a complete CRUD mapping keep
	// the honest scaffold body.
	f.AddComment("Delete destroys the remote resource.")
	deleteBody := scaffoldDeleteBody(modelName)
	if wiring.wired {
		deleteBody = wiredDeleteBody(modelName)
	}
	f.AddDecl(astgen.MethodDecl(
		"Delete", "r", astgen.StarExpr(astgen.Ident(structName)),
		astgen.Params(
			astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
			astgen.Field("req", astgen.QualExpr("resource", "DeleteRequest"), ""),
			astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "DeleteResponse")), ""),
		),
		astgen.Results(),
		astgen.Block(deleteBody...),
	))
	if wiring.wired {
		f.AddComment("deleteRemote performs the delete HTTP exchange, treating a 404 as already deleted. Extracted from Delete so the request/response logic is unit-testable without a tfsdk.State.")
		f.AddDecl(wiredDeleteHelperDecl(r, wiring, modelName, structName))
	}

	// Configure method. Wired resources implement ResourceWithConfigure to
	// receive the API client constructed by the provider's Configure method.
	if wiring.wired {
		f.AddComment("Configure stores the API client supplied by the provider.")
		f.AddDecl(resourceConfigureDecl(structName))
	}

	// ImportState method.
	if r.Importable {
		f.AddComment("ImportState imports an existing remote resource into Terraform state.")
		f.AddDecl(astgen.MethodDecl(
			"ImportState", "r", astgen.StarExpr(astgen.Ident(structName)),
			astgen.Params(
				astgen.Field("ctx", astgen.QualExpr("context", "Context"), ""),
				astgen.Field("req", astgen.QualExpr("resource", "ImportStateRequest"), ""),
				astgen.Field("resp", astgen.StarExpr(astgen.QualExpr("resource", "ImportStateResponse")), ""),
			),
			astgen.Results(),
			astgen.Block(importStateBody(r)...),
		))
	}

	// State upgrade method and versioned model structs.
	if hasStateUpgrades(r) {
		generatePriorModelStructs(f, r)
		f.AddComment("UpgradeState returns the state upgraders for this resource.")
		if decl := upgradeStateMethod(r); decl != nil {
			f.AddDecl(decl)
		}
	}

	// Register imports used by the generated file.
	if err := registerResourceImports(f, r, wiring, clientImport); err != nil {
		return nil, err
	}

	return f.AST(), nil
}

// resourceAssertSpecs returns the compile-time interface assertions for a
// resource: the base resource.Resource interface plus optional assertions for
// import support, state upgrades, and — for wired resources — Configure.
func resourceAssertSpecs(r ir.ResourceIR, wiring resourceWiringPlan, structName string) []*ast.ValueSpec {
	specFor := func(iface string) *ast.ValueSpec {
		return astgen.VarSpec(
			"_",
			astgen.QualExpr("resource", iface),
			astgen.Call(
				astgen.Parens(astgen.StarExpr(astgen.Ident(structName))),
				astgen.Nil(),
			),
		)
	}
	specs := []*ast.ValueSpec{specFor("Resource")}
	if resourceHasIdentity(r) {
		specs = append(specs, specFor("ResourceWithIdentity"))
	}
	if r.Importable {
		specs = append(specs, specFor("ResourceWithImportState"))
	}
	if hasStateUpgrades(r) {
		specs = append(specs, specFor("ResourceWithUpgradeState"))
	}
	if wiring.wired {
		specs = append(specs, specFor("ResourceWithConfigure"))
	}
	return specs
}

// registerResourceImports registers every import the generated resource file
// needs, including the generated client and JSON/HTTP packages used by wired
// CRUD bodies. It returns an error when the resource's import ID format is
// invalid.
func registerResourceImports(f *astgen.File, r ir.ResourceIR, wiring resourceWiringPlan, clientImport string) error {
	f.AddImport("context", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource", "resource")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource/schema", "schema")
	if resourceHasIdentity(r) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/resource/identityschema", "identityschema")
	}
	// Wired CRUD bodies populate resp.Identity via path.Root after Create/Read/
	// Update so the framework does not reject the response with "Missing Resource
	// Identity After Create/Read". Only needed when the resource has identity and
	// is wired (a non-wired resource keeps its scaffold body and never sets it).
	if resourceHasIdentity(r) && wiring.wired {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/path", "path")
	}
	// The model struct references types.* for every attribute and block field.
	// Auto-inferred resources with an empty schema (no attributes or blocks)
	// produce an empty model and must not import types, or the import is unused
	// and the generated provider does not compile.
	if len(r.Schema.Attributes) > 0 || len(r.Schema.Blocks) > 0 {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/types", "types")
	}
	if wiring.wired {
		registerWiredResourceImports(f, wiring, clientImport)
	}
	if objectSchemaNeedsValidators(r.Schema) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/schema/validator", "validator")
	}
	if r.Importable {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/path", "path")
		parsed, err := parseImportIDFormat(r.ImportIDFormat, r.IDAttribute)
		if err != nil {
			return fmt.Errorf("resource %q has invalid import ID format: %w", r.Name, err)
		}
		if !parsed.simple {
			f.AddImports("fmt", "strings")
		}
		// Non-string import attributes parse the string ID segment with strconv;
		// the parse-failure diagnostic is formatted with fmt.Sprintf.
		if importNeedsParsing(r, parsed.attrs) {
			f.AddImports("strconv", "fmt")
		}
	}
	needsList, needsSet := blockValidatorPackageImports(r.Schema)
	if needsList {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/listvalidator", "listvalidator")
	}
	if needsSet {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework-validators/setvalidator", "setvalidator")
	}
	if hasStateUpgrades(r) && stateUpgradeNeedsAttr(r) {
		f.AddImport("github.com/hashicorp/terraform-plugin-framework/attr", "attr")
	}
	return nil
}

// resourceModelName returns the generated model struct name for a resource.
func resourceModelName(r ir.ResourceIR) string {
	return naming.GoTypeName(r.Name) + "ResourceModel"
}

// registerWiredResourceImports adds the imports a wired CRUD body needs to build
// and send HTTP requests through the generated client. encoding/json is always
// imported because every wired Read (and the create/update response decode) uses
// json.NewDecoder to decode the response body. bytes is only imported when a
// wired create/update sends a JSON request body (bytes.NewReader); a form-encoded
// body (url.Values + strings.NewReader, used when the operation carries formData
// parameters) imports net/url instead. A resource with a JSON create and a form
// update imports both bytes and net/url.
func registerWiredResourceImports(f *astgen.File, wiring resourceWiringPlan, clientImport string) {
	f.AddImport(clientImport, "client")
	f.AddImports("encoding/json", "fmt", "io", "net/http")
	// bytes wraps the encoded payload as the request body reader for JSON
	// (bytes.NewReader) and XML (bytes.NewReader), and holds the multipart
	// body buffer (bytes.Buffer). A form-encoded body uses strings.NewReader
	// instead (net/url), so bytes is gated on JSON/XML/multipart.
	if wiring.needsJSONBody || wiring.needsXMLBody || wiring.needsMultipartBody {
		f.AddImport("bytes", "")
	}
	if wiring.needsFormBody || wiring.needsURL {
		f.AddImport("net/url", "")
	}
	// multipart/form-data bodies are built with mime/multipart.NewWriter;
	// a binary formData part reads the upload from a file path via os.Open.
	if wiring.needsMultipartBody {
		f.AddImport("mime/multipart", "")
	}
	if wiring.needsMultipartFile {
		f.AddImport("os", "")
		// filepath.Base derives the upload filename from the configured path
		// for the multipart Content-Disposition, so the request does not
		// leak the full local filesystem path as the part filename (A2).
		f.AddImport("path/filepath", "")
	}
	if wiring.needsStrings {
		f.AddImport("strings", "")
	}
	if wiring.needsStrconv {
		f.AddImport("strconv", "")
	}
}

// blockModelFieldType returns the Terraform Plugin Framework model field type for
// a nested block. List and set nested blocks use types.List / types.Set; single
// nested blocks use types.Object. These framework value types are concrete
// aliases (not Go generics), so the returned statement is a bare type reference.
func blockModelFieldType(block ir.BlockIR) ast.Expr {
	switch block.NestingMode {
	case ir.NestingList:
		return astgen.QualExpr("types", "List")
	case ir.NestingSet:
		return astgen.QualExpr("types", "Set")
	default:
		return astgen.QualExpr("types", "Object")
	}
}

// resourceTypeName returns the Terraform resource type name. It prefers
// ResourceIR.TypeName and falls back to a snake_cased ResourceIR.Name so that
// generated type names are always valid Terraform identifiers.
func resourceTypeName(r ir.ResourceIR) string {
	if strings.TrimSpace(r.TypeName) != "" {
		return strings.TrimSpace(r.TypeName)
	}
	return typeNameFallback(r.Name)
}

// resourceIDFieldInfo locates the identifier attribute used to track a resource
// instance in Terraform state. It returns the Terraform attribute name, the
// generated Go model field name, the primitive type, and whether a matching
// attribute was found. If ResourceIR.IDAttribute is empty it falls back to
// "id".
type idFieldInfo struct {
	attr      string
	wire      string
	field     string
	primitive ir.PrimitiveType
	found     bool
}

func resourceIDFieldInfo(r ir.ResourceIR) idFieldInfo {
	attr := strings.TrimSpace(r.IDAttribute)
	if attr == "" {
		attr = "id"
	}
	for _, a := range r.Schema.Attributes {
		if a.Name != attr {
			continue
		}
		// wire is the request/response JSON key the API uses for the identity,
		// which can differ from the tfsdk attribute name (camelCase policyName
		// vs snake_case policy_name). Generated request bodies are keyed by the
		// wire name, so mocks must inspect the body under this key; the tfsdk
		// name only addresses Terraform state attributes.
		wire := a.WireName
		if wire == "" {
			wire = attr
		}
		return idFieldInfo{
			attr:      attr,
			wire:      wire,
			field:     naming.GoFieldName(attr),
			primitive: a.Schema.Type,
			found:     true,
		}
	}
	return idFieldInfo{}
}

// idPlaceholder describes the placeholder value used for resource identifiers
// by the generated stub Create implementation. The HCL literal is used by the
// generated Terraform tests in tftest.go; the Go literal is used by the
// generated provider's Create method in resource.go. Keeping them in one
// table ensures the two generated artifacts never drift.
type idPlaceholder struct {
	hcl   string
	goLit any
}

var idPlaceholders = map[ir.PrimitiveType]idPlaceholder{
	ir.TypeString:  {hcl: `"generated"`, goLit: "generated"},
	ir.TypeInt:     {hcl: "1", goLit: 1},
	ir.TypeFloat:   {hcl: "1.0", goLit: 1.0},
	ir.TypeBool:    {hcl: "true", goLit: true},
	ir.TypeDynamic: {hcl: "null", goLit: nil},
}

// createIDPlaceholder returns the HCL literal placeholder used for resource
// identifiers by the generated stub Create implementation. It is the canonical
// source for id placeholder values so the generated Terraform tests in tftest.go
// assert exactly the same value the provider produces.
func createIDPlaceholder(t ir.PrimitiveType) string {
	p, ok := idPlaceholders[t]
	if !ok {
		return idPlaceholders[ir.TypeString].hcl
	}
	return p.hcl
}

// createIDValue returns an ast expression that builds the same placeholder value
// as createIDPlaceholder for assignment in generated Go code. Unrecognized
// primitive types return nil so callers can skip identifier assignment.
func createIDValue(t ir.PrimitiveType) ast.Expr {
	p, ok := idPlaceholders[t]
	if !ok || p.goLit == nil {
		return nil
	}
	switch v := p.goLit.(type) {
	case string:
		return astgen.Call(astgen.QualExpr("types", "StringValue"), astgen.Lit(v))
	case int:
		return astgen.Call(astgen.QualExpr("types", "Int64Value"), astgen.IntLit(v))
	case float64:
		return astgen.Call(astgen.QualExpr("types", "Float64Value"), astgen.FloatLit(v))
	case bool:
		return astgen.Call(astgen.QualExpr("types", "BoolValue"), astgen.BoolLit(v))
	}
	return nil
}

// createIDInitialization emits a statement that assigns a placeholder id value
// when the generated Create method would otherwise leave the identifier null.
// It returns nil when the resource has no recognizable identifier attribute.
func createIDInitialization(r ir.ResourceIR) ast.Stmt {
	info := resourceIDFieldInfo(r)
	if !info.found {
		return nil
	}
	value := createIDValue(info.primitive)
	if value == nil {
		return nil
	}
	return astgen.If(
		astgen.Binary(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("plan"), info.field), "IsNull")),
			token.LOR,
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("plan"), info.field), "IsUnknown")),
		),
		astgen.AssignStmt(
			[]ast.Expr{astgen.Selector(astgen.Ident("plan"), info.field)},
			[]ast.Expr{value},
			token.ASSIGN,
		),
	)
}

// updateIDPreservation emits a statement that copies a non-null identifier from
// state into the plan during updates so Terraform can continue tracking the
// resource. It returns nil when the resource has no recognizable identifier
// attribute.
func updateIDPreservation(r ir.ResourceIR) ast.Stmt {
	info := resourceIDFieldInfo(r)
	if !info.found {
		return nil
	}
	return astgen.If(
		astgen.Binary(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("plan"), info.field), "IsNull")),
			token.LOR,
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("plan"), info.field), "IsUnknown")),
		),
		astgen.If(
			astgen.Binary(
				astgen.Unary(token.NOT, astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("state"), info.field), "IsNull"))),
				token.LAND,
				astgen.Unary(token.NOT, astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("state"), info.field), "IsUnknown"))),
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Selector(astgen.Ident("plan"), info.field)},
				[]ast.Expr{astgen.Selector(astgen.Ident("state"), info.field)},
				token.ASSIGN,
			),
		),
	)
}

// resourceSchemaValues builds the []ast.Expr key/value elements for
// resource/schema.Schema{...}.
// resourceHasIdentity reports whether the resource carries a resource
// identity schema (the schema shared with its paired list resource). The
// generator only emits the IdentitySchema method and the ResourceWithIdentity
// assertion when this is true, so resources without a paired list resource
// are unaffected.
func resourceHasIdentity(r ir.ResourceIR) bool {
	return r.IdentitySchema != nil && len(r.IdentitySchema.Attributes) > 0
}

// resourceIdentitySchemaValues builds the identityschema.Schema composite
// literal elements for the resource's identity schema. Identity schemas only
// hold primitive (and list-of-primitive) attributes; the identity derivation
// (pkg/api) only ever produces primitive identity attributes from instance
// path parameters or the item "id", so each attribute maps to the matching
// identityschema primitive attribute type. Every identity attribute is
// RequiredForImport: importing a resource by its identity requires the fields
// that uniquely identify it.
func resourceIdentitySchemaValues(r ir.ResourceIR) []ast.Expr {
	attrs := r.IdentitySchema.Attributes
	attrElems := make([]ast.Expr, 0, len(attrs))
	for _, attr := range attrs {
		attrElems = append(attrElems, astgen.KeyValueExpr(
			astgen.Lit(attr.Name),
			resourceIdentityAttributeExpr(attr),
		))
	}
	return []ast.Expr{astgen.KeyValue("Attributes", astgen.CompositeLit(
		astgen.MapType(astgen.Ident("string"), astgen.QualExpr("identityschema", "Attribute")),
		attrElems...,
	))}
}

// resourceIdentityAttributeExpr returns an identityschema attribute composite
// literal for a primitive identity attribute. Non-primitive types fall back to
// StringAttribute; identity derivation only produces primitives, so this is a
// defensive default rather than an expected path.
func resourceIdentityAttributeExpr(attr ir.AttributeIR) ast.Expr {
	var attrType ast.Expr
	switch attr.Schema.Type {
	case ir.TypeInt:
		attrType = astgen.QualExpr("identityschema", "Int64Attribute")
	case ir.TypeFloat:
		attrType = astgen.QualExpr("identityschema", "Float64Attribute")
	case ir.TypeBool:
		attrType = astgen.QualExpr("identityschema", "BoolAttribute")
	default:
		attrType = astgen.QualExpr("identityschema", "StringAttribute")
	}
	return astgen.CompositeLit(attrType, astgen.KeyValue("RequiredForImport", astgen.Ident("true")))
}

func resourceSchemaValues(r ir.ResourceIR) []ast.Expr {
	elems := []ast.Expr{}
	if v := litOrOmit(r.Description); v != nil {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", v))
	}

	if v := resourceSchemaVersion(r); v > 0 {
		elems = append(elems, astgen.KeyValue("Version", astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(v)))))
	}

	attrs := r.Schema.Attributes
	blocks := r.Schema.Blocks

	if len(attrs) > 0 || len(blocks) > 0 {
		attrElems := make([]ast.Expr, 0, len(attrs))
		for _, attr := range attrs {
			attrElems = append(attrElems, astgen.KeyValueExpr(
				astgen.Lit(attr.Name),
				resourceAttributeExprWithPath(attr, ""),
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
				resourceBlockExpr(block, ""),
			))
		}
		elems = append(elems, astgen.KeyValue("Blocks", astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.QualExpr("schema", "Block")),
			blockElems...,
		)))
	}

	return elems
}

// resourceAttributeExpr returns an ast expression for a resource/schema Attribute.
func resourceAttributeExpr(attr ir.AttributeIR) ast.Expr {
	return resourceAttributeExprWithPath(attr, "")
}

// resourceAttributeExprWithPath returns an ast expression for a resource/schema
// Attribute, tracking the dotted parent path so that unsupported nested
// attributes can be reported with their full location.
func resourceAttributeExprWithPath(attr ir.AttributeIR, parentPath string) ast.Expr {
	path := fullAttrPath(parentPath, attr.Name)
	expr := frameworkResourceAttributeExpr(attr, path)
	if expr == nil {
		// A nested attribute that cannot be represented (e.g. a nested
		// collection) is dropped by the nested map builder; a top-level
		// attribute should never be nil because the framework expr falls back
		// to DynamicAttribute (G2).
		if parentPath == "" {
			panic(fmt.Sprintf("unsupported resource attribute %q: schema has no recognizable type or nested shape", path))
		}
		return nil
	}
	return expr
}

// frameworkResourceAttributeExpr maps an IR attribute to a Terraform Plugin Framework
// resource/schema attribute expression. attrPath is the dotted path to the
// current attribute and is propagated to nested attribute maps.
func frameworkResourceAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	s := attr.Schema

	// Collection types.
	if s.Collection != nil {
		if expr := resourceCollectionAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Primitive types.
	if s.Type != "" {
		if expr := resourcePrimitiveAttributeExpr(attr, attrPath); expr != nil {
			return expr
		}
	}

	// Union types (oneOf/anyOf): a discriminated union renders via the
	// dynamic-union strategy as a SingleNestedAttribute merging all variant
	// fields plus the discriminator attribute, with a DiscriminatorValidator
	// (D2); any other union falls back to DynamicAttribute because the
	// plugin-framework resource schema has no first-class union attribute. When
	// a schema has both Type and Union set, the primitive Type branch wins.
	if s.Union != nil {
		if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
			d := resourceAttributeValues(attr, []ast.Expr{
				astgen.KeyValue("Attributes", nestedResourceAttributesMapFromSchema(*merged, attrPath)),
			})
			d = append(d, schema.DiscriminatedUnionValidators(s))
			return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), resourceAttributeValues(attr, nil)...)
	}

	// Object-like types (Attributes or Blocks present without explicit primitive type).
	if schema.IsObjectLike(s) {
		d := resourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("Attributes", nestedResourceAttributesMapFromSchema(s, attrPath)),
		})
		d = schema.AddValidators(d, attr, "Object")
		return astgen.CompositeLit(astgen.QualExpr("schema", "SingleNestedAttribute"), d...)
	}

	// Unrepresentable shapes (e.g. a nested collection such as a List of Map of
	// Dynamic) cannot map to a framework attribute. At the top level a
	// DynamicAttribute is valid and honest; nested inside a collection it would
	// be rejected by the framework, so the nested map builder drops it (G2).
	if strings.Contains(attrPath, ".") {
		return nil
	}
	return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), resourceAttributeValues(attr, nil)...)
}

// resourceCollectionAttributeExpr maps a collection-typed attribute to its
// framework attribute, or nil when the shape falls through to the
// primitive/union/unrepresentable handling below (G12).
func resourceCollectionAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	elem := schema.DynamicUnionElement(attr.Schema.Collection.ElementType)
	// A collection whose element is directly dynamic/null cannot be represented
	// as a typed framework collection (List{ElementType: DynamicType} is
	// rejected). At the top level it degrades to DynamicAttribute; nested, it is
	// dropped (nil) so the enclosing collection's ContainsNestedDynamic check
	// can promote the ancestor (G12).
	if elem.Type == ir.TypeDynamic || elem.Type == ir.TypeNull {
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), resourceAttributeValues(attr, nil)...)
	}
	// A collection whose element is an object (or nested collection) that
	// contains a dynamic at any depth cannot be rendered as a typed framework
	// collection either: the terraform-plugin-framework rejects any collection
	// whose element type contains a dynamic (fwtype.ContainsCollectionWithDynamic).
	// Emit the whole collection as a DynamicAttribute, per the framework's own
	// guidance. This is valid in an object-or-top-level context; when this
	// collection is itself nested inside another collection's element, the
	// enclosing collection's ContainsNestedDynamic check has already promoted that
	// ancestor, so this emission is never reached inside a collection.
	if schema.ContainsNestedDynamic(elem) {
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), resourceAttributeValues(attr, nil)...)
	}
	switch attr.Schema.Collection.Kind {
	case ir.List:
		return resourceListElementAttributeExpr(attr, elem, attrPath, "List")
	case ir.Set:
		return resourceListElementAttributeExpr(attr, elem, attrPath, "Set")
	case ir.Map:
		return resourceMapElementAttributeExpr(attr, elem, attrPath)
	}
	return nil
}

// resourceListElementAttributeExpr maps a List/Set element to its framework
// attribute (List*Attribute or Set*Attribute).
func resourceListElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath, kind string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := resourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		d = schema.AddValidators(d, attr, kind)
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"Attribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := resourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", nestedResourceAttributesMapFromSchema(elem, attrPath)),
			)),
		})
		d = schema.AddValidators(d, attr, "Object")
		return astgen.CompositeLit(astgen.QualExpr("schema", kind+"NestedAttribute"), d...)
	}
	return nil
}

// resourceMapElementAttributeExpr maps a Map element to its framework
// attribute (MapAttribute or MapNestedAttribute).
func resourceMapElementAttributeExpr(attr ir.AttributeIR, elem ir.SchemaIR, attrPath string) ast.Expr {
	if schema.IsPrimitiveSchema(elem) {
		d := resourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("ElementType", primitiveAttrType(elem.Type)),
		})
		d = schema.AddValidators(d, attr, "Map")
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapAttribute"), d...)
	}
	if schema.IsObjectLike(elem) {
		d := resourceAttributeValues(attr, []ast.Expr{
			astgen.KeyValue("NestedObject", astgen.CompositeLit(
				astgen.QualExpr("schema", "NestedAttributeObject"),
				astgen.KeyValue("Attributes", nestedResourceAttributesMapFromSchema(elem, attrPath)),
			)),
		})
		d = schema.AddValidators(d, attr, "Object")
		return astgen.CompositeLit(astgen.QualExpr("schema", "MapNestedAttribute"), d...)
	}
	return nil
}

// resourcePrimitiveAttributeExpr maps a primitive-typed attribute to its
// framework attribute, or nil when the type is not a recognized primitive.
func resourcePrimitiveAttributeExpr(attr ir.AttributeIR, attrPath string) ast.Expr {
	switch attr.Schema.Type {
	case ir.TypeString:
		d := resourceAttributeValues(attr, nil)
		d = schema.AddValidators(d, attr, "String")
		return astgen.CompositeLit(astgen.QualExpr("schema", "StringAttribute"), d...)
	case ir.TypeInt:
		d := resourceAttributeValues(attr, nil)
		d = schema.AddValidators(d, attr, "Int64")
		return astgen.CompositeLit(astgen.QualExpr("schema", "Int64Attribute"), d...)
	case ir.TypeFloat:
		d := resourceAttributeValues(attr, nil)
		d = schema.AddValidators(d, attr, "Float64")
		return astgen.CompositeLit(astgen.QualExpr("schema", "Float64Attribute"), d...)
	case ir.TypeBool:
		d := resourceAttributeValues(attr, nil)
		d = schema.AddValidators(d, attr, "Bool")
		return astgen.CompositeLit(astgen.QualExpr("schema", "BoolAttribute"), d...)
	case ir.TypeDynamic:
		// A DynamicAttribute is only valid at the top level; nested inside a
		// collection it is rejected by the framework, so the nested map
		// builder drops it (G12).
		if strings.Contains(attrPath, ".") {
			return nil
		}
		return astgen.CompositeLit(astgen.QualExpr("schema", "DynamicAttribute"), resourceAttributeValues(attr, nil)...)
	}
	return nil
}

// resourceBlockExpr returns an ast expression for a resource/schema Block.
// parentPath is the dotted path of the enclosing attribute and is propagated to
// the block's nested attribute maps so panics can report the full dotted location.
func resourceBlockExpr(block ir.BlockIR, parentPath string) ast.Expr {
	var kind string
	path := fullAttrPath(parentPath, block.Name)
	attrs := nestedResourceAttributesMap(block.Schema, path)

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
		panic(fmt.Errorf("%w: %q for block %q", ErrUnknownResourceBlockNesting, block.NestingMode, block.Name))
	}

	if block.Description != "" {
		elems = append(elems, astgen.KeyValue("MarkdownDescription", astgen.Lit(block.Description)))
	}
	if block.DeprecationMessage != "" {
		elems = append(elems, astgen.KeyValue("DeprecationMessage", astgen.Lit(block.DeprecationMessage)))
	}

	return astgen.CompositeLit(astgen.QualExpr("schema", kind), elems...)
}

// blockSizeValidatorExprs returns the plugin-framework validator expressions for
// a block's MinItems/MaxItems cardinality constraints. pkg is the validator
// package path segment ("listvalidator" or "setvalidator"), chosen by the
// caller based on the block's NestingMode. The second parameter is unused
// (L-53: the prior doc described a `kind` parameter that was removed from the
// signature).
func blockSizeValidatorExprs(block ir.BlockIR, _, pkg string) []ast.Expr {
	if block.MinItems == nil && block.MaxItems == nil {
		return nil
	}
	var exprs []ast.Expr
	if block.MinItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr(pkg, "SizeAtLeast"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MinItems))),
		))
	}
	if block.MaxItems != nil {
		exprs = append(exprs, astgen.Call(
			astgen.QualExpr(pkg, "SizeAtMost"),
			astgen.Call(astgen.Ident("int64"), astgen.IntLit(int(*block.MaxItems))),
		))
	}
	return exprs
}

// resourceAttributeValues builds the common field dictionary for a resource attribute.
func resourceAttributeValues(attr ir.AttributeIR, extra []ast.Expr) []ast.Expr {
	// Resolve a Required+Computed conflict before emitting flags so the render
	// never produces a framework-invalid schema (N-25).
	attr = normalizeAttributeFlags(attr)

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

// nestedResourceAttributesMap returns map[string]schema.Attribute{...} for the given
// object schema. parentPath is the dotted path of the enclosing attribute or block.
func nestedResourceAttributesMap(s ir.ObjectSchemaIR, parentPath string) ast.Expr {
	elems := make([]ast.Expr, 0, len(s.Attributes))
	for _, attr := range s.Attributes {
		expr := resourceAttributeExprWithPath(attr, parentPath)
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

// nestedResourceAttributesMapFromSchema converts a SchemaIR object-like value to a nested
// attributes map expression. It is used for collection element types that are
// represented as SchemaIR rather than ObjectSchemaIR. parentPath is the dotted path
// of the enclosing attribute and is propagated to nested attribute panics.
//
// Blocks are intentionally omitted from the resulting nested attributes map
// because NestedAttributeObject only supports Attributes, not Blocks.
func nestedResourceAttributesMapFromSchema(s ir.SchemaIR, parentPath string) ast.Expr {
	return nestedResourceAttributesMap(ir.ObjectSchemaIR{Attributes: s.Attributes}, parentPath)
}

// typeNameFallback returns a normalized Terraform type name for an IR entity
// when no explicit TypeName is provided. It trims surrounding whitespace and
// snake_cases the name so the generated identifier is always valid HCL.
func typeNameFallback(name string) string {
	return naming.SnakeCase(strings.TrimSpace(name))
}
