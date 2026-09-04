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

// caseWithBody creates a switch case clause with the given case expressions and
// body statements. It wraps astgen.CaseClause, which only accepts case values.
func caseWithBody(cases []ast.Expr, body ...ast.Stmt) *ast.CaseClause {
	cc := astgen.CaseClause(cases...)
	cc.Body = body
	return cc
}

// ResourceAcceptanceTestFile returns the generated
// internal/provider/resource_<name>_acceptance_test.go file containing an
// acceptance test that exercises the resource lifecycle against an httptest
// mock API. A malformed ImportIDFormat on an importable resource is surfaced as
// a generation error (ErrorFile) rather than silently dropping the import step
// (L-26), mirroring the fail-loud validateImportIDFormat check in ResourceFile.
func ResourceAcceptanceTestFile(pir ir.ProviderIR, r ir.ResourceIR, cfg BuildConfig) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("resource_%s_acceptance_test.go", naming.SnakeCase(r.Name)))
	file, err := func() (f *ast.File, err error) {
		f, err = generateResourceAcceptanceTestFile(pir, r, cfg)
		return
	}()
	if err != nil {
		return ErrorFile(path, err)
	}
	return GoCodeAST(path, file)
}

// ResourceAcceptanceTestFiles returns the generated resource acceptance test
// files for every wired ResourceIR in the provider. Files are emitted in the
// order supplied. A scaffolded (unwired) resource's CRUD bodies report "is not
// wired to a remote API endpoint", so a lifecycle acceptance test against a mock
// server could never pass; skipping it keeps the generated provider's own
// `go test ./...` green while the scaffold's honest runtime diagnostic still
// surfaces when the resource is used.
//
// A resource whose wired create sends a multipart/form-data body with a binary
// formData parameter (a file upload, OpenAPI format: binary) is also skipped:
// the generated client reads each such attribute's value as a local file path
// via os.Open, and the mock server JSON-decodes request bodies, so a
// placeholder-driven acceptance lifecycle cannot round-trip a file upload. The
// resource keeps its (non-acceptance) schema test; skipping the lifecycle here
// is honest rather than emitting a test that fails at runtime.
func ResourceAcceptanceTestFiles(pir ir.ProviderIR, cfg BuildConfig) []File {
	files := make([]File, 0, len(pir.Resources))
	for _, r := range pir.Resources {
		if !planResourceWiring(r).wired || resourceCreateHasBinaryUpload(r) {
			continue
		}
		files = append(files, ResourceAcceptanceTestFile(pir, r, cfg))
	}
	return files
}

// resourceCreateHasBinaryUpload reports whether the resource's wired create
// operation sends a multipart/form-data body with at least one binary formData
// parameter (a file upload, OpenAPI format: binary). Such resources cannot be
// exercised by the placeholder-driven mock acceptance lifecycle (see
// ResourceAcceptanceTestFiles).
func resourceCreateHasBinaryUpload(r ir.ResourceIR) bool {
	for _, p := range r.CRUDMapping.Create.FormDataParams {
		if strings.EqualFold(p.Schema.Format, "binary") {
			return true
		}
	}
	return false
}

// AcceptanceTestFiles returns the complete set of generated acceptance-test
// files for a provider.
func AcceptanceTestFiles(pir ir.ProviderIR, cfg BuildConfig) []File {
	return ResourceAcceptanceTestFiles(pir, cfg)
}

func generateResourceAcceptanceTestFile(pir ir.ProviderIR, r ir.ResourceIR, cfg BuildConfig) (*ast.File, error) {
	f := astgen.NewFile("provider")

	structName := resourceStructName(r)
	providerName := cfg.providerName()
	resourceAddr := resourceTypeName(r) + ".example"
	testFuncName := "TestAcc" + structName + "Lifecycle"
	configFuncName := "testAcc" + structName + "Config"
	mockFuncName := "new" + structName + "MockServer"

	paramAttr, paramCreate, paramUpdated, hasParam := acceptanceParamAttribute(r)
	hasEndpoint := providerHasEndpointAttr(pir)
	// The token URL placeholders are only injected when the provider config
	// schema actually carries the attribute (the real pipeline merges the auth
	// attributes into ConfigSchema); gating the flags on attribute presence
	// keeps the config function's parameter list and the template's fmt
	// placeholders in lockstep.
	hasTokenURL := providerHasOAuth2TokenFlow(pir) && findAcceptancePlaceholderAttr(pir, "token_url") != nil
	hasOIDCTokenURL := providerHasOpenIDConnect(pir) && findAcceptancePlaceholderAttr(pir, "oidc_token_url") != nil
	configTmpl := generateResourceAcceptanceConfigTemplate(pir, r, paramAttr, hasTokenURL, hasOIDCTokenURL)

	// Register imports used by the test function and helpers.
	f.AddImport("net/http", "http")
	f.AddImport("net/http/httptest", "httptest")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-testing/helper/resource", "resource")
	f.AddImport("github.com/hashicorp/terraform-plugin-go/tfprotov6", "tfprotov6")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/providerserver", "providerserver")

	needsJSON := false
	needsIO := false
	needsFmt := false

	routes := mockRoutes(r.CRUDMapping)
	// The mock handler closure uses strings.Trim/TrimPrefix to extract the
	// resource id from the request path and a sync.Mutex to serialize state, so
	// those imports are only needed when at least one route is stubbed. A wired
	// resource whose path collapses to an empty prefix (every leading segment is
	// an unresolved dynamic placeholder) produces no routes; adding them
	// unconditionally would leave them imported-and-unused and fail go vet.
	if len(routes) > 0 {
		f.AddImport("strings", "")
		f.AddImport("sync", "")
	}
	for _, route := range routes {
		if route.create || route.read || route.update {
			needsJSON = true
		}
		if route.create || route.update {
			needsIO = true
		}
		// The create handler stringifies the identity value (body[idKey]) to
		// key state, so it emits fmt.Sprintf whenever a create route exists.
		if route.create {
			needsFmt = true
		}
	}
	if needsJSON {
		f.AddImport("encoding/json", "json")
	}
	if needsIO {
		f.AddImport("io", "")
	}

	generateAcceptanceConfigFunction(f, configFuncName, configTmpl, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam)
	if needsFmt || hasEndpoint || hasTokenURL || hasOIDCTokenURL || hasParam {
		f.AddImport("fmt", "")
	}
	generateMockServerFunction(f, r, mockFuncName, routes, pir.SecurityIR.Schemes)

	steps, err := acceptanceTestSteps(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam, paramAttr, paramCreate, paramUpdated)
	if err != nil {
		return nil, err
	}

	testBody := astgen.Block(
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("t"), "Setenv"), astgen.Lit("TF_ACC"), astgen.Lit("1"))),
		astgen.AssignSingle(astgen.Ident("server"), astgen.Call(astgen.Ident(mockFuncName))),
		astgen.Defer(astgen.Call(astgen.Selector(astgen.Ident("server"), "Close"))),
		astgen.ExprStmt(astgen.Call(
			astgen.QualExpr("resource", "Test"),
			astgen.Ident("t"),
			astgen.CompositeLit(
				astgen.QualExpr("resource", "TestCase"),
				astgen.KeyValue("ProtoV6ProviderFactories", protoV6ProviderFactories(providerName)),
				astgen.KeyValue("Steps", steps),
			),
		)),
	)
	testFn := astgen.FuncDeclFull(testFuncName,
		astgen.Params(astgen.Field("t", astgen.StarExpr(astgen.QualExpr("testing", "T")), "")),
		nil,
		testBody,
	)
	testFn.Doc = &ast.CommentGroup{
		List: []*ast.Comment{
			{Text: "// " + fmt.Sprintf("%s verifies create, update, delete, and import flows against a mock API.", testFuncName)},
		},
	}
	f.AddDecl(testFn)

	return f.AST(), nil
}

// protoV6ProviderFactories returns the map expression used by acceptance tests
// to wire the generated provider into terraform-plugin-testing.
func protoV6ProviderFactories(providerName string) ast.Expr {
	return astgen.CompositeLit(
		astgen.MapType(
			astgen.Ident("string"),
			astgen.FuncType(
				astgen.Params(),
				astgen.Results(
					astgen.Field("", astgen.QualExpr("tfprotov6", "ProviderServer"), ""),
					astgen.Field("", astgen.Ident("error"), ""),
				),
			),
		),
		astgen.KeyValueExpr(
			astgen.Lit(providerName),
			astgen.Call(
				astgen.QualExpr("providerserver", "NewProtocol6WithError"),
				astgen.Call(astgen.Ident("New")),
			),
		),
	)
}

// acceptanceTestSteps builds the []resource.TestStep slice for the lifecycle
// test. It always emits a create step, emits an update step when a configurable
// primitive attribute can be varied, and emits an import step when the resource
// is importable. When the resource is importable but its ImportIDFormat cannot
// be parsed, the error is returned (and surfaced as a generation error by
// ResourceAcceptanceTestFile) rather than silently dropping the import step,
// which would invisibly lose import test coverage (L-26).
func acceptanceTestSteps(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool, paramAttr, paramCreate, paramUpdated string) (ast.Expr, error) {
	elems := []ast.Expr{createTestStep(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam, paramAttr, paramCreate)}
	// The update step applies a changed config and expects the resource to be
	// updated. A resource whose Update is a scaffold (no update operation in the
	// spec, or an unresolvable mapping) reports "Update is not wired to a remote
	// API endpoint" as a diagnostic, so the step could never pass; skip it and
	// keep the lifecycle test to create/import/destroy (G-21).
	if hasParam && planResourceWiring(r).update {
		elems = append(elems, updateTestStep(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, paramAttr, paramUpdated))
	}
	// A child resource (read via read_collection_path, a parent GET whose
	// response nests the collection) selects the element whose identifier
	// matches the imported id — but the mock's read path carries only the
	// parent id, so it cannot serve an element keyed by an arbitrary imported
	// identifier. The import step would exercise a mismatch the mock cannot
	// resolve, so it is skipped; create/update/delete coverage remains.
	if r.Importable && strings.TrimSpace(r.CRUDMapping.Read.NestedCollectionPath) == "" {
		importID, err := acceptanceImportID(r)
		if err != nil {
			return nil, fmt.Errorf("resource %q acceptance import step: %w", r.Name, err)
		}
		elems = append(elems, importTestStep(resourceAddr, importID))
	}
	return astgen.CompositeLit(
		astgen.SliceType(astgen.QualExpr("resource", "TestStep")),
		elems...,
	), nil
}

func createTestStep(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool, paramAttr, paramCreate string) ast.Expr {
	paramValue := ""
	if hasParam {
		paramValue = paramCreate
	}
	args := configFuncCallArgs(hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam, paramValue)
	checks := []ast.Expr{}
	if idInfo := resourceIDFieldInfo(r); idInfo.found {
		// Use the actual ID attribute name (r.IDAttribute, falling back to
		// "id") rather than a hardcoded "id", so a resource whose ID is
		// exposed as e.g. pet_id does not assert a nonexistent attribute
		// (M-15).
		checks = append(checks, astgen.Call(
			astgen.QualExpr("resource", "TestCheckResourceAttrSet"),
			astgen.Lit(resourceAddr),
			astgen.Lit(idInfo.attr),
		))
	}
	if hasParam {
		checks = append(checks, astgen.Call(
			astgen.QualExpr("resource", "TestCheckResourceAttr"),
			astgen.Lit(resourceAddr),
			astgen.Lit(paramAttr),
			astgen.Lit(paramCreate),
		))
	}
	return astgen.CompositeLit(
		astgen.QualExpr("resource", "TestStep"),
		astgen.KeyValue("Config", astgen.Call(astgen.Ident(configFuncName), args...)),
		astgen.KeyValue("Check", composeAggregateCheckFunc(checks)),
	)
}

func updateTestStep(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL bool, paramAttr, paramUpdated string) ast.Expr {
	args := configFuncCallArgs(hasEndpoint, hasTokenURL, hasOIDCTokenURL, true, paramUpdated)
	checks := []ast.Expr{}
	if idInfo := resourceIDFieldInfo(r); idInfo.found {
		checks = append(checks, astgen.Call(
			astgen.QualExpr("resource", "TestCheckResourceAttrSet"),
			astgen.Lit(resourceAddr),
			astgen.Lit(idInfo.attr),
		))
	}
	checks = append(checks, astgen.Call(
		astgen.QualExpr("resource", "TestCheckResourceAttr"),
		astgen.Lit(resourceAddr),
		astgen.Lit(paramAttr),
		astgen.Lit(paramUpdated),
	))
	return astgen.CompositeLit(
		astgen.QualExpr("resource", "TestStep"),
		astgen.KeyValue("Config", astgen.Call(astgen.Ident(configFuncName), args...)),
		astgen.KeyValue("Check", composeAggregateCheckFunc(checks)),
	)
}

func importTestStep(resourceAddr, importID string) ast.Expr {
	return astgen.CompositeLit(
		astgen.QualExpr("resource", "TestStep"),
		astgen.KeyValue("ResourceName", astgen.Lit(resourceAddr)),
		astgen.KeyValue("ImportState", astgen.Ident("true")),
		astgen.KeyValue("ImportStateId", astgen.Lit(importID)),
	)
}

func composeAggregateCheckFunc(checks []ast.Expr) ast.Expr {
	if len(checks) == 0 {
		return astgen.Call(astgen.QualExpr("resource", "ComposeAggregateTestCheckFunc"))
	}
	return astgen.Call(astgen.QualExpr("resource", "ComposeAggregateTestCheckFunc"), checks...)
}

// configFuncCallArgs builds the argument expressions passed to the generated
// config helper at a test step's call site, in the same fixed order the config
// template writes its fmt placeholders: the mock server URL (endpoint), the
// mock token URL (token_url, when the provider has a token-fetching OAuth2
// flow), the mock token URL again (oidc_token_url, when the provider has an
// OpenID Connect scheme), then the parameterized attribute value. The token
// URL is the mock server URL with the /oauth/token path appended, matching the
// endpoint the mock token stub (mockTokenEndpointStmts) registers; the same
// stub serves every grant, so both token URL attributes point at it.
func configFuncCallArgs(hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool, paramValue string) []ast.Expr {
	args := []ast.Expr{}
	mockTokenURL := astgen.Binary(
		astgen.Selector(astgen.Ident("server"), "URL"),
		token.ADD,
		astgen.Lit("/oauth/token"),
	)
	if hasEndpoint {
		args = append(args, astgen.Selector(astgen.Ident("server"), "URL"))
	}
	if hasTokenURL {
		args = append(args, mockTokenURL)
	}
	if hasOIDCTokenURL {
		args = append(args, mockTokenURL)
	}
	if hasParam {
		args = append(args, astgen.Lit(paramValue))
	}
	return args
}

// generateAcceptanceConfigFunction emits the helper that returns the HCL config
// for a test step. It accepts a server URL placeholder when the provider has an
// "endpoint" attribute, a token URL placeholder when the provider has a
// token-fetching OAuth2 flow, an OIDC token URL placeholder when the provider
// has an OpenID Connect scheme, and a name placeholder when a configurable
// primitive attribute was selected for the update step. The params mirror the
// fixed placeholder order in the template (endpoint, token_url, oidc_token_url,
// name).
func generateAcceptanceConfigFunction(f *astgen.File, name, tmpl string, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool) {
	params := []*ast.Field{}
	args := []ast.Expr{}
	if hasEndpoint {
		params = append(params, astgen.Field("serverURL", astgen.Ident("string"), ""))
		args = append(args, astgen.Ident("serverURL"))
	}
	if hasTokenURL {
		params = append(params, astgen.Field("tokenURL", astgen.Ident("string"), ""))
		args = append(args, astgen.Ident("tokenURL"))
	}
	if hasOIDCTokenURL {
		params = append(params, astgen.Field("oidcTokenURL", astgen.Ident("string"), ""))
		args = append(args, astgen.Ident("oidcTokenURL"))
	}
	if hasParam {
		params = append(params, astgen.Field("name", astgen.Ident("string"), ""))
		args = append(args, astgen.Ident("name"))
	}
	if len(args) > 0 {
		formatArgs := append([]ast.Expr{astgen.Lit(tmpl)}, args...)
		f.AddDecl(astgen.FuncDeclFull(name,
			astgen.Params(params...),
			astgen.Results(astgen.Field("", astgen.Ident("string"), "")),
			astgen.Block(astgen.Return(astgen.Call(astgen.QualExpr("fmt", "Sprintf"), formatArgs...))),
		))
		return
	}
	f.AddDecl(astgen.FuncDeclFull(name,
		nil,
		astgen.Results(astgen.Field("", astgen.Ident("string"), "")),
		astgen.Block(astgen.Return(astgen.Lit(tmpl))),
	))
}

// generateResourceAcceptanceConfigTemplate builds an HCL config string with Go
// fmt placeholders for the server URL ("%s"), the OAuth2 token URL ("%s", when
// the provider has a client_credentials scheme), and the parameterized attribute
// value ("%s"). The placeholder order is fixed: endpoint, token_url,
// oidc_token_url, then the parameterized resource attribute;
// configFuncCallArgs emits args in that order.
func generateResourceAcceptanceConfigTemplate(pir ir.ProviderIR, r ir.ResourceIR, paramAttr string, hasTokenURL, hasOIDCTokenURL bool) string {
	var h hclBuilder
	writeProviderAcceptanceConfig(&h, pir, hasTokenURL, hasOIDCTokenURL)
	h.writeLinef(`resource "%s" "example" {`, resourceTypeName(r))
	h.indent++
	writeHCLAcceptanceBody(&h, r.Schema, paramAttr)
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

func writeProviderAcceptanceConfig(h *hclBuilder, pir ir.ProviderIR, hasTokenURL, hasOIDCTokenURL bool) {
	h.writeLinef(`provider "%s" {`, providerTypeName(pir))
	h.indent++
	// endpoint, token_url, and oidc_token_url carry fmt placeholders that
	// configFuncCallArgs fills positionally. Write them in a fixed order
	// (endpoint, then token_url, then oidc_token_url) before the remaining
	// attributes so the %s ordering in the template is deterministic regardless
	// of ConfigSchema attribute order; the matching configFuncCallArgs /
	// generateAcceptanceConfigFunction emit their args and params in this same
	// order.
	if findAcceptancePlaceholderAttr(pir, "endpoint") != nil {
		h.writeLinef(`endpoint = "%%s"`)
	}
	if hasTokenURL && findAcceptancePlaceholderAttr(pir, "token_url") != nil {
		h.writeLinef(`token_url = "%%s"`)
	}
	if hasOIDCTokenURL && findAcceptancePlaceholderAttr(pir, "oidc_token_url") != nil {
		h.writeLinef(`oidc_token_url = "%%s"`)
	}
	for _, attr := range pir.ConfigSchema.Attributes {
		if attr.Schema.Type == ir.TypeString && includeInExample(attr) {
			if attr.Name == "endpoint" {
				continue
			}
			if hasTokenURL && attr.Name == "token_url" {
				continue
			}
			if hasOIDCTokenURL && attr.Name == "oidc_token_url" {
				continue
			}
		}
		writeHCLAcceptanceAttribute(h, attr, "")
	}
	for _, block := range pir.ConfigSchema.Blocks {
		writeHCLAcceptanceBlock(h, block)
	}
	h.indent--
	h.writeLinef("}")
}

// writeHCLAcceptanceBody writes configurable attributes and blocks to the HCL
// template, substituting the parameterized attribute value with a fmt
// placeholder so the acceptance test can vary it between create and update
// steps.
//
// Attribute names are emitted directly as HCL identifiers. Terraform attribute
// names are always valid HCL identifiers, so no additional escaping is needed.
func writeHCLAcceptanceBody(h *hclBuilder, obj ir.ObjectSchemaIR, paramAttr string) {
	for _, attr := range obj.Attributes {
		if !includeInExample(attr) {
			continue
		}
		if attr.Name == paramAttr && schema.IsPrimitiveSchema(attr.Schema) {
			if attr.Schema.Type == ir.TypeString {
				h.writeLinef("%s = \"%%s\"", attr.Name)
			} else {
				h.writeLinef("%s = %%s", attr.Name)
			}
			continue
		}
		writeHCLAcceptanceAttribute(h, attr, "")
	}
	for _, block := range obj.Blocks {
		writeHCLAcceptanceBlock(h, block)
	}
}

func writeHCLAcceptanceAttribute(h *hclBuilder, attr ir.AttributeIR, _ string) {
	s := attr.Schema
	// A DynamicAttribute (a primitive dynamic, or a collection degraded to
	// dynamic because its element is/nests a dynamic) carries arbitrary JSON.
	// Configure it with a SCALAR placeholder, never a collection literal: a list
	// literal on a DynamicAttribute is parsed by the framework as a Tuple, whose
	// concrete element types the response mapping (dynamicValueFromRaw ->
	// inferTFTypes) cannot reliably reproduce, causing "wrong final value type:
	// tuple required" at apply (G18). null round-trips for an Optional Dynamic
	// (omitted from the request body, absent from the echoed response); a string
	// literal round-trips for a Required Dynamic (the request side unwraps
	// UnderlyingValue, the response side rebuilds via dynamicValueFromRaw). Seen
	// on GitLab application.scopes (Required) and protected_branch.allowed_to_*
	// (Optional, degraded from array), and Grafana alert_rule.data (G-22).
	if schema.IsDynamicAttribute(s) {
		if attr.Required {
			h.writeLinef("%s = %s", attr.Name, `"example"`)
		} else {
			h.writeLinef("%s = %s", attr.Name, "null")
		}
		return
	}
	if s.Collection != nil {
		writeHCLAcceptanceCollectionAttribute(h, attr)
		return
	}
	// Union types (oneOf/anyOf): a discriminated union renders as a
	// SingleNestedAttribute merging variant fields plus the discriminator, with
	// a DiscriminatorValidator (D2); any other union degrades to a
	// DynamicAttribute and is emitted as a scalar placeholder. Mirrors the
	// resource schema emission order (resource.go) so the config matches the
	// generated schema shape. Without this branch a union attribute (empty
	// Attributes in the IR) falls through to primitiveExampleValue and emits a
	// string for an object attribute, failing schema validation at plan time.
	if s.Union != nil {
		if writeHCLDiscriminatedUnion(h, attr, func(hh *hclBuilder, a ir.AttributeIR) {
			writeHCLAcceptanceAttribute(hh, a, "")
		}) {
			return
		}
		if attr.Required {
			h.writeLinef("%s = %s", attr.Name, `"example"`)
		} else {
			h.writeLinef("%s = %s", attr.Name, "null")
		}
		return
	}
	if schema.IsObjectLike(s) {
		h.writeLinef("%s = {", attr.Name)
		h.indent++
		writeHCLAcceptanceBody(h, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, "")
		h.indent--
		h.writeLinef("}")
		return
	}
	// The placeholder honors the schema's constraints (enum/const/bounds/
	// length/pattern) so the acceptance config satisfies the validators the
	// schema emits at plan time; an unconstrained schema keeps the type-only
	// placeholder.
	h.writeLinef("%s = %s", attr.Name, schemaExampleLiteral(s))
}

func writeHCLAcceptanceCollectionAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema
	elem := s.Collection.ElementType

	switch s.Collection.Kind {
	case ir.List, ir.Set:
		if schema.IsPrimitiveSchema(elem) {
			// The element placeholder honors the element's own constraints so
			// enum-constrained elements validate against the ValueStringsAre
			// validator emitted from the same schema.
			h.writeLinef("%s = [%s]", attr.Name, schemaExampleLiteral(elem))
			return
		}
		if schema.IsObjectLike(elem) {
			h.writeLinef("%s = [{", attr.Name)
			h.indent++
			writeHCLAcceptanceBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, "")
			h.indent--
			h.writeLinef("}]")
			return
		}
	case ir.Map:
		if schema.IsPrimitiveSchema(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef(`"key" = %s`, schemaExampleLiteral(elem))
			h.indent--
			h.writeLinef("}")
			return
		}
		if schema.IsObjectLike(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef(`"key" = {`)
			h.indent++
			writeHCLAcceptanceBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, "")
			h.indent--
			h.writeLinef("}")
			h.indent--
			h.writeLinef("}")
			return
		}
	}

	h.writeLinef("%s = []", attr.Name)
}

func writeHCLAcceptanceBlock(h *hclBuilder, block ir.BlockIR) {
	h.writeLinef("%s {", block.Name)
	h.indent++
	writeHCLAcceptanceBody(h, block.Schema, "")
	h.indent--
	h.writeLinef("}")
}

// acceptanceParamAttribute selects the first configurable primitive attribute to
// vary across the create and update test steps, along with the constraint-aware
// create and updated values for it. It prefers a required or optional string
// attribute and falls back to any primitive attribute. An attribute whose
// constraints admit no two distinct valid values (const, a one-member enum, a
// degenerate numeric range, an unmatchable pattern) is skipped, so the update
// step never varies a value the generated validators would reject.
func acceptanceParamAttribute(r ir.ResourceIR) (name, create, updated string, ok bool) {
	// The update step mutates this attribute to verify the resource was
	// updated. The identifier (id) attribute must never be the mutation target:
	// changing it rewrites the resource's identity and, for a PUT-as-create
	// upsert, the instance path the Create/Update PUT substitutes, so the step
	// would not exercise a real update. Skip it regardless of Required/Optional.
	idAttr := strings.TrimSpace(r.IDAttribute)
	if idAttr == "" {
		idAttr = "id"
	}
	// candidate reports whether the attribute can serve as the create/update
	// mutation parameter: a configurable scalar (not a collection or object),
	// included in the example config, not the identifier, and admitting two
	// distinct values its validators accept (acceptanceParamPair). A Required
	// Dynamic is excluded: Terraform rejects null for a Required attribute at
	// plan time ("Missing Configuration for Required Attribute"), and
	// writeHCLAcceptanceAttribute configures a Required Dynamic with a string
	// scalar ("example") instead, so it round-trips without needing a parameter
	// placeholder.
	candidate := func(attr ir.AttributeIR) bool {
		if !includeInExample(attr) || attr.Name == idAttr {
			return false
		}
		if attr.Schema.Collection != nil || schema.IsObjectLike(attr.Schema) {
			return false
		}
		if attr.Required && schema.IsDynamicAttribute(attr.Schema) {
			return false
		}
		var createVal, updatedVal string
		var pairOK bool
		createVal, updatedVal, pairOK = acceptanceParamPair(attr.Schema)
		if !pairOK {
			return false
		}
		create, updated = createVal, updatedVal
		return true
	}
	for _, attr := range r.Schema.Attributes {
		if candidate(attr) && attr.Schema.Type == ir.TypeString {
			return attr.Name, create, updated, true
		}
	}
	for _, attr := range r.Schema.Attributes {
		if candidate(attr) && attr.Schema.Type != "" {
			return attr.Name, create, updated, true
		}
	}
	return "", "", "", false
}

func providerHasEndpointAttr(pir ir.ProviderIR) bool {
	for _, attr := range pir.ConfigSchema.Attributes {
		if attr.Name == "endpoint" && attr.Schema.Type == ir.TypeString {
			return true
		}
	}
	return false
}

// providerHasOAuth2TokenFlow reports whether the provider declares an OAuth2
// scheme with a token-fetching flow whose interceptor reads the token_url
// config attribute: client_credentials, password, or authorization_code
// (refresh path). Such providers need the mock token endpoint and a token_url
// placeholder in the acceptance config.
func providerHasOAuth2TokenFlow(pir ir.ProviderIR) bool {
	for _, s := range pir.SecurityIR.Schemes {
		if s.Type != ir.SecuritySchemeOAuth2 || s.Flows == nil {
			continue
		}
		if s.Flows.ClientCredentials != nil || s.Flows.Password != nil || s.Flows.AuthorizationCode != nil {
			return true
		}
	}
	return false
}

// providerHasOpenIDConnect reports whether the provider declares an OpenID
// Connect scheme. Its generated interceptor reads the oidc_token_url config
// attribute (a token endpoint override that skips discovery), so such
// providers need the mock token endpoint and an oidc_token_url placeholder in
// the acceptance config.
func providerHasOpenIDConnect(pir ir.ProviderIR) bool {
	for _, s := range pir.SecurityIR.Schemes {
		if s.Type == ir.SecuritySchemeOpenIDConnect {
			return true
		}
	}
	return false
}

// findAcceptancePlaceholderAttr returns the first ConfigSchema attribute named
// name that is a string included in example output, or nil. Such attributes are
// written as fmt "%s" placeholders in the acceptance config (endpoint and
// token_url) rather than literal example values, so configFuncCallArgs can
// inject the mock server URL.
func findAcceptancePlaceholderAttr(pir ir.ProviderIR, name string) *ir.AttributeIR {
	for i := range pir.ConfigSchema.Attributes {
		attr := &pir.ConfigSchema.Attributes[i]
		if attr.Name == name && attr.Schema.Type == ir.TypeString && includeInExample(*attr) {
			return attr
		}
	}
	return nil
}

// acceptanceImportSegment returns the import ID segment for one attribute.
// Non-string attributes use a value the generated ImportState can parse back
// (matching the mock's placeholder ID so the mock's read fallback serves the
// created resource); string attributes use the "imported-"<attr> marker.
func acceptanceImportSegment(r ir.ResourceIR, attr string) string {
	switch importAttributeType(r, attr) {
	case ir.TypeInt, ir.TypeFloat:
		return "1"
	case ir.TypeBool:
		return "true"
	default:
		if v, ok := staticImportPathSegment(r, attr); ok {
			return v
		}
		return "imported-" + attr
	}
}

// staticImportPathSegment returns the static value the acceptance mock
// registered for a path placeholder bound to the given import attribute, when
// mockRoutePrefix substitutes that placeholder (const/default/first enum — the
// same rule the mock applies). The generated ImportState fills the attribute
// from the import ID segment and the import refresh's Read substitutes it back
// into the request path; a segment that differs from the mock's static value
// builds a path outside every registered route, so the import 404s with
// "Cannot import non-existent remote object". gigavuecore's notif_meta_config
// is the motivating case: {notifType} binds the enum attribute `type` (via the
// enum-equivalence rule, not a name match), the mock registers only the
// enum-first /notification/event/notifMetaConfig/instant prefix, so the import
// ID must carry "instant" as its type segment rather than "imported-type".
// True identity segments (e.g. {taskId}, no const/default/enum) have no static
// value and keep the "imported-<attr>" form that exercises the mock's lastKey
// fallback.
func staticImportPathSegment(r ir.ResourceIR, attrName string) (string, bool) {
	ops := []ir.OperationMappingIR{r.CRUDMapping.Create, r.CRUDMapping.Read, r.CRUDMapping.Delete}
	if r.CRUDMapping.Update != nil {
		ops = append(ops, *r.CRUDMapping.Update)
	}
	for _, op := range ops {
		for _, ph := range pathPlaceholders(op.PathTemplate) {
			if !placeholderBindsImportAttribute(r, op.PathParams, ph, attrName) {
				continue
			}
			if v, ok := staticPathValue(op.PathParams, ph); ok {
				return v, true
			}
		}
	}
	return "", false
}

// placeholderBindsImportAttribute reports whether a path placeholder resolves
// to attrName under the same rules resolvePathSubstitution applies: a name or
// WireName match, or the enum-equivalence binding for a placeholder whose
// names differ (notifType ↔ type).
func placeholderBindsImportAttribute(r ir.ResourceIR, pathParams []ir.ParamIR, placeholder, attrName string) bool {
	for _, attr := range r.Schema.Attributes {
		if attr.Name != attrName {
			continue
		}
		if attr.Name == placeholder || attr.WireName == placeholder {
			return true
		}
		if bound, ok := enumEquivalentAttribute(r, pathParams, placeholder); ok && bound.Name == attrName {
			return true
		}
	}
	return false
}

// acceptanceImportID builds a deterministic import identifier for the
// acceptance import step. It mirrors the parsing logic used by the generated
// ImportState method, producing segments the ImportState can convert back into
// each attribute's primitive type (an int64 ID cannot store "imported-id").
// When the format cannot be parsed, it returns an empty id along with the
// parse error; the sole caller (acceptanceTestSteps) surfaces that error as a
// generation error rather than silently dropping the import step, so a
// malformed ImportIDFormat fails loud instead of invisibly losing import test
// coverage (L-26).
func acceptanceImportID(r ir.ResourceIR) (string, error) {
	parsed, err := parseImportIDFormat(r.ImportIDFormat, r.IDAttribute)
	if err != nil {
		return "", err
	}
	if parsed.simple {
		return acceptanceImportSegment(r, parsed.attrs[0]), nil
	}
	parts := make([]string, len(parsed.attrs))
	for i, attr := range parsed.attrs {
		parts[i] = acceptanceImportSegment(r, attr)
	}
	return strings.Join(parts, parsed.delimiter), nil
}

// mockRoute describes a single httptest route stubbed for a resource operation.
type mockRoute struct {
	path   string
	create bool
	read   bool
	update bool
	delete bool
	// createMethod is the HTTP method of the create operation. It is "POST" for
	// a conventional collection create and "PUT" for a PUT-as-create (upsert)
	// resource whose Create is the instance-path PUT. The generated handler
	// dispatches the create branch on this method instead of a hard-coded POST,
	// so a PUT create reaches the create branch. Empty defaults to POST for
	// routes built without a create method (preserves prior output).
	createMethod string
	// deleteMethod is the HTTP method of the delete operation. A non-DELETE
	// delete (e.g. POST /pets/{id}/scrap) shares the route prefix with the
	// collection POST create; the generated handler uses this to dispatch
	// nested-path requests to the delete branch instead of the create branch.
	deleteMethod string
	// The HTTP status code the stub returns for each operation. These mirror
	// the spec's declared success codes (first declared code; conventional
	// 201/200/200/204 when none are declared) so the generated client's success
	// check, which matches against the same codes, accepts the stub response.
	createStatus int
	readStatus   int
	updateStatus int
	deleteStatus int
	// readNestedPath/readNestedEnvelope describe a child-resource read: the
	// parent GET's response (after the readNestedEnvelope unwrap) nests the
	// collection under readNestedPath (config read_collection_path, e.g. a port
	// filter rule read nested at "rules.*" inside {"portFilter": {...}}). The
	// mock's GET branch must serve the stored element body wrapped in that
	// shape, or the generated read's navigation finds no array and reports the
	// resource removed — failing the acceptance test it cannot otherwise
	// distinguish from a genuine deletion.
	readNestedPath     []string
	readNestedEnvelope string
}

// firstSuccessCode returns the first declared success code for an operation,
// falling back to conventional default when the spec declares none.
func firstSuccessCode(codes []int, fallback int) int {
	if len(codes) == 0 {
		return fallback
	}
	return codes[0]
}

// updateMethodList returns the HTTP methods the update branch should dispatch
// on. The update branch covers PUT and PATCH; when the create branch already
// handles one of them (PUT-as-create: create and update are the same instance
// PUT), it is dropped to avoid a duplicate switch label. The returned slice
// preserves the conventional [PUT, PATCH] order for byte-identical output on
// resources whose create is a collection POST.
func updateMethodList(create bool, createMethod string) []string {
	methods := []string{"PUT", "PATCH"}
	if !create {
		return methods
	}
	kept := make([]string, 0, len(methods))
	for _, m := range methods {
		if strings.EqualFold(m, createMethod) {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

func generateMockServerFunction(f *astgen.File, r ir.ResourceIR, funcName string, routes []mockRoute, schemes []ir.SecuritySchemeIR) {
	body := make([]ast.Stmt, 0, len(routes)+3)
	body = append(body, astgen.AssignSingle(astgen.Ident("mux"), astgen.Call(astgen.QualExpr("http", "NewServeMux"))))
	// Register the OAuth2 token endpoint before the resource routes so a
	// client_credentials interceptor has a token to fetch during the lifecycle.
	body = append(body, mockTokenEndpointStmts(schemes)...)
	idInfo := resourceIDFieldInfo(r)
	idPrimitive := idInfo.primitive
	// The mock echoes the ID back under the resource's actual ID attribute name
	// (r.IDAttribute, falling back to "id") so the create/update response
	// decodes into the resource's ID attribute — a resource whose ID is exposed
	// as e.g. symbol would otherwise never have its ID set and the generated
	// Create would fail its identifier check (G-21).
	//
	// The body is keyed by the ID's wire name (idInfo.wire), not the tfsdk
	// attribute name: generated request bodies use the wire name as the JSON
	// key, so a presence check on the snake_case tfsdk name always misses, the
	// mock injects a placeholder over a real user-supplied identity, and the
	// test observes the placeholder instead of the configured value.
	idKey := idInfo.wire
	if idKey == "" {
		idKey = "id"
	}
	for i, route := range routes {
		body = append(body, statefulMockRouteHandler(route, i, schemes, idPrimitive, idKey)...)
	}
	body = append(body, astgen.Return(astgen.Call(astgen.QualExpr("httptest", "NewServer"), astgen.Ident("mux"))))

	fn := astgen.FuncDeclFull(funcName,
		nil,
		astgen.Results(astgen.Field("", astgen.StarExpr(astgen.QualExpr("httptest", "Server")), "")),
		astgen.Block(body...),
	)
	fn.Doc = &ast.CommentGroup{
		List: []*ast.Comment{
			{Text: "// " + fmt.Sprintf("%s returns an httptest server that stubs the %s CRUD endpoints.", funcName, resourceStructName(r))},
			{Text: "// The server echoes request bodies so that create/update responses reflect the values sent by the test."},
		},
	}
	f.AddDecl(fn)
}

// routeKind identifies which CRUD operation an addRoute call contributes, so a
// non-DELETE delete (e.g. POST /pets/{id}/scrap) is recorded as a delete rather
// than as a create on the same prefix — otherwise it would overwrite the
// collection POST create's status code (G-21).
type routeKind int

const (
	routeCreate routeKind = iota
	routeRead
	routeUpdate
	routeDelete
)

// mockRoutes aggregates CRUD operations into route stubs keyed by the path prefix
// up to the first '{' parameter. Operations that share that prefix are merged
// and dispatched by HTTP method in the generated handler. Nested paths such as
// POST /pets/{id}/action share the same prefix as POST /pets and would be
// merged, so only first-segment-level CRUD routes are currently stubbed; a
// non-DELETE delete on a nested path is dispatched to the delete branch by the
// generated handler (see statefulMockRouteHandler).
func mockRoutes(m ir.CRUDMappingIR) []mockRoute {
	byPath := map[string]mockRoute{}

	addRoute := func(pathTemplate, method string, status int, kind routeKind, pathParams []ir.ParamIR) {
		path := mockRoutePrefix(pathTemplate, pathParams)
		if path == "" {
			return
		}
		route := byPath[path]
		route.path = path
		switch kind {
		case routeCreate:
			route.create = true
			route.createMethod = method
			route.createStatus = status
		case routeRead:
			route.read = true
			route.readStatus = status
		case routeUpdate:
			route.update = true
			route.updateStatus = status
		case routeDelete:
			route.delete = true
			route.deleteMethod = method
			route.deleteStatus = status
		}
		byPath[path] = route
	}

	addRoute(m.Create.PathTemplate, m.Create.Method, firstSuccessCode(m.Create.SuccessCodes, 201), routeCreate, m.Create.PathParams)
	addRoute(m.Read.PathTemplate, m.Read.Method, firstSuccessCode(m.Read.SuccessCodes, 200), routeRead, m.Read.PathParams)
	// A child-resource read (read_collection_path) is a parent GET whose
	// response nests the collection: the GET handler must serve the stored
	// element wrapped in that envelope + path shape so the generated read's
	// navigation finds it. Attach the shape to the read's route bucket.
	if nested := strings.TrimSpace(m.Read.NestedCollectionPath); nested != "" {
		if path := mockRoutePrefix(m.Read.PathTemplate, m.Read.PathParams); path != "" {
			route := byPath[path]
			route.readNestedPath = strings.Split(nested, ".")
			route.readNestedEnvelope = m.Read.ResponseEnvelope
			byPath[path] = route
		}
	}
	if m.Update != nil {
		addRoute(m.Update.PathTemplate, m.Update.Method, firstSuccessCode(m.Update.SuccessCodes, 200), routeUpdate, m.Update.PathParams)
	}
	addRoute(m.Delete.PathTemplate, m.Delete.Method, firstSuccessCode(m.Delete.SuccessCodes, 204), routeDelete, m.Delete.PathParams)

	keys := make([]string, 0, len(byPath))
	for k := range byPath {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	routes := make([]mockRoute, 0, len(keys))
	for _, k := range keys {
		routes = append(routes, byPath[k])
	}
	return routes
}

// mockRoutePrefix computes the route-registration prefix for a path template:
// static placeholders (const/default/enum in pathParams) are substituted with
// their literal values so a leading static segment such as {apiVersion} (enum
// ["v4beta"]) turns /{apiVersion}/things into /v4beta/things, then the path is
// truncated at the first dynamic '{' placeholder and trailing slashes trimmed.
// "" is returned when nothing remains (the mock cannot register an empty path).
func mockRoutePrefix(pathTemplate string, pathParams []ir.ParamIR) string {
	pathTemplate = substituteStaticPathPlaceholders(pathTemplate, pathParams)
	path := pathTemplate
	if idx := strings.Index(pathTemplate, "{"); idx >= 0 {
		path = pathTemplate[:idx]
	}
	return strings.TrimRight(path, "/")
}

// substituteStaticPathPlaceholders replaces path placeholders that resolve to a
// static literal (const/default/first enum value, via staticPathValue) with
// their literal values, leaving dynamic placeholders (instance ids, parent
// ids) in place. The result is the most concrete path the mock can register:
// /{apiVersion}/things/{id} with apiVersion enum ["v4beta"] becomes
// /v4beta/things/{id}. Placeholders absent from pathParams are left unchanged.
func substituteStaticPathPlaceholders(pathTemplate string, pathParams []ir.ParamIR) string {
	for _, ph := range pathPlaceholders(pathTemplate) {
		if v, ok := staticPathValue(pathParams, ph); ok {
			pathTemplate = strings.ReplaceAll(pathTemplate, "{"+ph+"}", v)
		}
	}
	return pathTemplate
}

// mockAuthExpectedCredential is the credential value the generated acceptance
// config writes for every string auth attribute (it is primitiveExampleValue
// for ir.TypeString). The mock asserts this exact value so a regression that
// drops the credential or sends the wrong one is caught at runtime. The
// coupling lives in this one generator package: if primitiveExampleValue's
// string value changes, update this constant to match.
const mockAuthExpectedCredential = "example"

// mockOAuth2Token is the bearer token the mock token endpoint hands out for a
// token-fetching scheme (OAuth2 client_credentials, password, or
// authorization_code via its refresh path, or OpenID Connect). The generated
// client's interceptor POSTs to the mock's /oauth/token endpoint, receives
// this token, and attaches "Authorization: Bearer example-token" to every
// resource request; the resource-path handler asserts that header. It is
// distinct from mockAuthExpectedCredential so a fetched token cannot be
// confused with a static HTTP bearer credential.
const mockOAuth2Token = "example-token"

// mockAuthCandidate is one assertable scheme rendered as a credential-check
// statement, keyed by scheme name for deterministic sorting.
type mockAuthCandidate struct {
	name string
	stmt ast.Stmt
}

// tokenFetchingSchemeInSchemes reports whether any scheme has a generated
// interceptor that fetches a token and injects it as an Authorization: Bearer
// header — OAuth2 client_credentials, password, or authorization_code (refresh
// path), or OpenID Connect. These contest the Authorization header with HTTP
// bearer under AND semantics, so the mock credential checks skip one side when
// both are present. The mock token endpoint returns the same mockOAuth2Token
// for every grant, so multiple token-fetching schemes do not contest each
// other: whichever interceptor runs last attaches the same header value.
func tokenFetchingSchemeInSchemes(schemes []ir.SecuritySchemeIR) bool {
	for _, s := range schemes {
		switch s.Type {
		case ir.SecuritySchemeOAuth2:
			if s.Flows != nil && (s.Flows.ClientCredentials != nil || s.Flows.Password != nil || s.Flows.AuthorizationCode != nil) {
				return true
			}
		case ir.SecuritySchemeOpenIDConnect:
			return true
		}
	}
	return false
}

// httpBearerInSchemes reports whether any scheme is an HTTP bearer scheme.
func httpBearerInSchemes(schemes []ir.SecuritySchemeIR) bool {
	for _, s := range schemes {
		if s.Type == ir.SecuritySchemeHTTP && strings.EqualFold(s.Scheme, "bearer") {
			return true
		}
	}
	return false
}

// httpBasicInSchemes reports whether any scheme is an HTTP basic scheme.
func httpBasicInSchemes(schemes []ir.SecuritySchemeIR) bool {
	for _, s := range schemes {
		if s.Type == ir.SecuritySchemeHTTP && strings.EqualFold(s.Scheme, "basic") {
			return true
		}
	}
	return false
}

// apiKeyAuthorizationHeaderInSchemes reports whether any apiKey scheme writes
// the Authorization header (in: header, name: Authorization). Such a scheme
// contests the Authorization header with HTTP basic and HTTP bearer schemes,
// all of which the generated client writes to Authorization.
func apiKeyAuthorizationHeaderInSchemes(schemes []ir.SecuritySchemeIR) bool {
	for _, s := range schemes {
		if s.Type != ir.SecuritySchemeAPIKey {
			continue
		}
		loc := s.In
		if loc == "" {
			loc = "header"
		}
		if strings.EqualFold(loc, "header") && strings.EqualFold(s.NameField, "Authorization") {
			return true
		}
	}
	return false
}

// mockAuthCandidates builds the per-scheme credential-check candidates. The
// hasTokenFetching and hasHTTPBearer flags carry the cross-scheme
// Authorization-header conflict: when both are present neither Authorization
// assertion is emitted (last writer wins under AND semantics), so either could
// spuriously fail depending on interceptor order.
func mockAuthCandidates(schemes []ir.SecuritySchemeIR, hasTokenFetching bool) []mockAuthCandidate {
	hasHTTPBearer := httpBearerInSchemes(schemes)
	hasHTTPBasic := httpBasicInSchemes(schemes)
	hasAPIKeyAuthorization := apiKeyAuthorizationHeaderInSchemes(schemes)
	var candidates []mockAuthCandidate
	for i, s := range schemes {
		if c, ok := mockAuthCandidateForScheme(s, i, hasTokenFetching, hasHTTPBearer, hasHTTPBasic, hasAPIKeyAuthorization); ok {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

// mockAuthCandidateForScheme returns the credential-check candidate for a single
// scheme, or ok=false when the scheme has no assertable interceptor. The
// Authorization-header conflict skips are applied here.
func mockAuthCandidateForScheme(s ir.SecuritySchemeIR, i int, hasTokenFetching, hasHTTPBearer, hasHTTPBasic, hasAPIKeyAuthorization bool) (mockAuthCandidate, bool) {
	switch s.Type {
	case ir.SecuritySchemeAPIKey:
		loc := s.In
		if loc == "" {
			loc = "header"
		}
		name := s.NameField
		if name == "" {
			name = "X-API-Key"
		}
		// An apiKey scheme that writes the Authorization header contests the
		// same header as an HTTP basic scheme (e.g. Grafana declares alternative
		// basic and api_key schemes, both writing Authorization). Under AND
		// semantics the mock would require both, which is impossible on a single
		// Authorization header, so neither side is asserted when both are
		// present (mirroring the bearer/token-fetching mutual skip above). The
		// provider's single Authorization header — whichever interceptor wins —
		// is then accepted.
		if hasHTTPBasic && strings.EqualFold(loc, "header") && strings.EqualFold(name, "Authorization") {
			return mockAuthCandidate{}, false
		}
		return mockAuthCandidate{s.Name, mockAPIKeyAuthCheck(loc, name, i)}, true
	case ir.SecuritySchemeHTTP:
		switch strings.ToLower(s.Scheme) {
		case "basic":
			// Symmetric skip: when an apiKey-in-Authorization scheme is also
			// present, the Authorization header is contested (see above), so do
			// not assert basic auth either.
			if hasAPIKeyAuthorization {
				return mockAuthCandidate{}, false
			}
			return mockAuthCandidate{s.Name, mockBasicAuthCheck(i)}, true
		case "bearer":
			// Skip when a token-fetching scheme is also present: both
			// interceptors write Authorization and the winner depends on
			// interceptor order (AND semantics), so either assertion could
			// spuriously fail. The OAuth2/OIDC bearer check is skipped
			// symmetrically below.
			if hasTokenFetching {
				return mockAuthCandidate{}, false
			}
			return mockAuthCandidate{s.Name, mockBearerAuthCheck()}, true
		}
	case ir.SecuritySchemeOAuth2:
		if s.Flows == nil || (s.Flows.ClientCredentials == nil && s.Flows.Password == nil && s.Flows.AuthorizationCode == nil) {
			// The implicit flow and the degenerate no-flows surface have no
			// token-fetching interceptor, so there is nothing to assert.
			return mockAuthCandidate{}, false
		}
		// Symmetric skip: when an HTTP bearer scheme is also present, the
		// Authorization header is contested (see above), so do not assert the
		// OAuth2 bearer token either.
		if hasHTTPBearer {
			return mockAuthCandidate{}, false
		}
		return mockAuthCandidate{s.Name, mockOAuth2BearerCheck()}, true
	case ir.SecuritySchemeOpenIDConnect:
		// The OIDC interceptor fetches mockOAuth2Token from the mock's token
		// endpoint (the acceptance config overrides oidc_token_url to point at
		// it, skipping discovery) and attaches it as a Bearer header.
		if hasHTTPBearer {
			return mockAuthCandidate{}, false
		}
		return mockAuthCandidate{s.Name, mockOAuth2BearerCheck()}, true
	}
	return mockAuthCandidate{}, false
}

// mockAuthCheckStmts generates statements that validate the provider's auth
// schemes on each mock request, returning 401 if any expected credential is
// missing or wrong. This closes the REMAINING_GAPS §5 item "the mock asserts
// nothing about auth headers" and delivers §1.2 level (c) for the
// static-credential schemes (API key in header/query/cookie, HTTP basic, HTTP
// bearer) and for the token-fetching schemes — OAuth2 client_credentials,
// password, and authorization_code (refresh path), plus OpenID Connect — where
// the mock stubs the token endpoint (mockTokenEndpointStmts), the client
// fetches mockOAuth2Token from it, and the resource path asserts the resulting
// Bearer header. The mock token endpoint ignores the grant form, so every
// grant type (client_credentials, password, refresh_token) is served by the
// same stub. The OAuth2 implicit flow is skipped: it has no token-fetching
// interceptor. Schemes are sorted by name so generation is deterministic
// (buildSecurityIR iterates a spec map). Returns nil for providers with no
// assertable scheme, so the generated handler is unchanged for unauthenticated
// providers.
//
// The HTTP bearer and token-fetching interceptors both set the Authorization
// header (last writer wins under the provider's AND semantics). When both are
// present neither Authorization assertion is emitted, because either one could
// spuriously fail depending on interceptor order; this is the documented
// AND-semantics limitation, not a silent drop.
func mockAuthCheckStmts(schemes []ir.SecuritySchemeIR) []ast.Stmt {
	candidates := mockAuthCandidates(schemes, tokenFetchingSchemeInSchemes(schemes))
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].name < candidates[j].name })
	if len(candidates) == 0 {
		return nil
	}
	stmts := make([]ast.Stmt, 0, len(candidates))
	for _, c := range candidates {
		stmts = append(stmts, c.stmt)
	}
	return stmts
}

// mockAuthRejectBody returns the statements written when a credential check
// fails: a 401 Unauthorized response and an early return. The check runs before
// mu.Lock, so no mutex is held on the rejection path.
func mockAuthRejectBody(msg string) []ast.Stmt {
	return []ast.Stmt{
		astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "Error"), astgen.Ident("w"), astgen.Lit(msg), astgen.QualExpr("http", "StatusUnauthorized"))),
		astgen.Return(),
	}
}

// mockAPIKeyAuthCheck returns the credential check for an API key scheme in the
// given location (header, query, or cookie) under the given name. The index
// namespaces per-scheme local variables so two cookie schemes do not collide.
func mockAPIKeyAuthCheck(loc, name string, i int) ast.Stmt {
	msg := fmt.Sprintf("missing %s api key", name)
	switch loc {
	case "query":
		return astgen.If(
			astgen.Binary(
				astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("r"), "URL"), "Query")), "Get"), astgen.Lit(name)),
				token.NEQ,
				astgen.Lit(mockAuthExpectedCredential),
			),
			mockAuthRejectBody(msg)...,
		)
	case "cookie":
		ck := fmt.Sprintf("ck%d", i)
		ckErr := fmt.Sprintf("ckErr%d", i)
		// r.Cookie returns a nil cookie with the error when the cookie is absent;
		// the || short-circuits so ck.Value is only read when ckErr == nil.
		return &ast.IfStmt{
			Init: astgen.Assign(
				[]ast.Expr{astgen.Ident(ck), astgen.Ident(ckErr)},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("r"), "Cookie"), astgen.Lit(name))},
			),
			Cond: astgen.Binary(
				astgen.Binary(astgen.Ident(ckErr), token.NEQ, astgen.Nil()),
				token.LOR,
				astgen.Binary(astgen.Selector(astgen.Ident(ck), "Value"), token.NEQ, astgen.Lit(mockAuthExpectedCredential)),
			),
			Body: astgen.Block(mockAuthRejectBody(msg)...),
		}
	default: // header
		return astgen.If(
			astgen.Binary(
				astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("r"), "Header"), "Get"), astgen.Lit(name)),
				token.NEQ,
				astgen.Lit(mockAuthExpectedCredential),
			),
			mockAuthRejectBody(msg)...,
		)
	}
}

// mockBasicAuthCheck returns the credential check for an HTTP basic scheme. The
// index namespaces the decoded username/password/ok locals.
func mockBasicAuthCheck(i int) ast.Stmt {
	bu := fmt.Sprintf("bu%d", i)
	bp := fmt.Sprintf("bp%d", i)
	bok := fmt.Sprintf("bok%d", i)
	return &ast.IfStmt{
		Init: astgen.Assign(
			[]ast.Expr{astgen.Ident(bu), astgen.Ident(bp), astgen.Ident(bok)},
			[]ast.Expr{astgen.Call(astgen.Selector(astgen.Ident("r"), "BasicAuth"))},
		),
		Cond: astgen.Binary(
			astgen.Binary(
				astgen.Unary(token.NOT, astgen.Ident(bok)),
				token.LOR,
				astgen.Binary(astgen.Ident(bu), token.NEQ, astgen.Lit(mockAuthExpectedCredential)),
			),
			token.LOR,
			astgen.Binary(astgen.Ident(bp), token.NEQ, astgen.Lit(mockAuthExpectedCredential)),
		),
		Body: astgen.Block(mockAuthRejectBody("missing basic auth credential")...),
	}
}

// mockBearerAuthCheck returns the credential check for an HTTP bearer scheme.
func mockBearerAuthCheck() ast.Stmt {
	return astgen.If(
		astgen.Binary(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("r"), "Header"), "Get"), astgen.Lit("Authorization")),
			token.NEQ,
			astgen.Lit("Bearer "+mockAuthExpectedCredential),
		),
		mockAuthRejectBody("missing bearer token")...,
	)
}

// mockOAuth2BearerCheck returns the credential check for a token-fetching
// scheme (OAuth2 client_credentials, password, or authorization_code via its
// refresh path, or OpenID Connect). The generated client fetches
// mockOAuth2Token from the mock's /oauth/token endpoint (registered by
// mockTokenEndpointStmts) and attaches it as "Authorization: Bearer
// example-token"; the resource-path handler asserts that header to prove the
// token fetch and attachment both succeeded. It is only emitted when no HTTP
// bearer scheme is present, because the two interceptors contest the
// Authorization header under AND semantics.
func mockOAuth2BearerCheck() ast.Stmt {
	return astgen.If(
		astgen.Binary(
			astgen.Call(astgen.Selector(astgen.Selector(astgen.Ident("r"), "Header"), "Get"), astgen.Lit("Authorization")),
			token.NEQ,
			astgen.Lit("Bearer "+mockOAuth2Token),
		),
		mockAuthRejectBody("missing oauth2 bearer token")...,
	)
}

// mockTokenEndpointStmts returns the statements that register a stub OAuth2
// token endpoint at /oauth/token when the provider declares a token-fetching
// scheme (OAuth2 client_credentials, password, or authorization_code via its
// refresh path, or OpenID Connect). The endpoint ignores the request body, so
// every grant type (client_credentials, password, refresh_token) is served,
// and returns a fixed bearer token (mockOAuth2Token) as JSON, matching the
// response shape the generated client's token source decodes (access_token /
// token_type / expires_in). It is registered on the mux before the resource
// routes; net/http's ServeMux keeps /oauth/token distinct from the resource
// path prefixes, so there is no pattern conflict. Returns nil when no
// token-fetching scheme is present, so unauthenticated and static-credential
// providers get no dead token endpoint. OpenID Connect discovery is not
// stubbed: the acceptance config overrides oidc_token_url to point at this
// endpoint, which skips discovery (the spec's discovery URL is baked into the
// provider and unreachable in tests).
func mockTokenEndpointStmts(schemes []ir.SecuritySchemeIR) []ast.Stmt {
	if !tokenFetchingSchemeInSchemes(schemes) {
		return nil
	}
	tokenJSON := fmt.Sprintf(`{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, mockOAuth2Token)
	handler := astgen.FuncLit(
		astgen.FuncType(
			astgen.Params(
				astgen.Field("w", astgen.QualExpr("http", "ResponseWriter"), ""),
				astgen.Field("r", astgen.StarExpr(astgen.QualExpr("http", "Request")), ""),
			),
			nil,
		),
		astgen.Block(
			astgen.ExprStmt(astgen.Call(
				astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"),
				astgen.Lit("Content-Type"),
				astgen.Lit("application/json"),
			)),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_"), astgen.Ident("_")},
				[]ast.Expr{astgen.Call(
					astgen.Selector(astgen.Ident("w"), "Write"),
					astgen.Call(astgen.SliceType(astgen.Ident("byte")), astgen.Lit(tokenJSON)),
				)},
				token.ASSIGN,
			),
		),
	)
	return []ast.Stmt{
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("mux"), "HandleFunc"),
			astgen.Lit("/oauth/token"),
			handler,
		)),
	}
}

// mockIDDefault returns the string the mock uses for the resource ID when the
// request path carries none (e.g. a collection POST). It must equal the string
// form of mockIDValue so the ID the mock returns is the ID the client sends on
// subsequent Read/Update/Delete requests, which the mock looks up by path.
func mockIDDefault(t ir.PrimitiveType) string {
	switch t {
	case ir.TypeInt:
		return "1"
	case ir.TypeFloat:
		return "1"
	case ir.TypeBool:
		return "true"
	default: // TypeString, TypeDynamic, TypeNull, unknown
		return "example-id"
	}
}

// mockIDValue returns the Go literal the mock assigns to body["id"] so the
// response decodes into the resource's ID attribute. String IDs use the
// canonical "example-id" placeholder; integer/number/boolean IDs use a typed
// literal, since jsonToAttrValue decodes only a JSON number into an int64 ID
// and a string would fail with "expected JSON number" (G18).
func mockIDValue(t ir.PrimitiveType) ast.Expr {
	switch t {
	case ir.TypeInt:
		return astgen.IntLit(1)
	case ir.TypeFloat:
		return astgen.FloatLit(1.0)
	case ir.TypeBool:
		return astgen.BoolLit(true)
	default: // TypeString, TypeDynamic, TypeNull, unknown
		return astgen.Lit("example-id")
	}
}

// mockWildcardCollectionKey is the object key the mock's GET branch uses for a
// trailing "*" segment of a child resource's read_collection_path. The
// generated provider's wildcard read aggregates every array value at that
// level regardless of key name, so any deterministic key satisfies it; the
// name documents that the array is a stand-in for the spec's sibling arrays
// (e.g. passRules/dropRules), which the mock cannot reconstruct.
const mockWildcardCollectionKey = "mock_collection"

// mockReadResponseExpr returns the expression the mock's GET branch encodes as
// the response. A plain read serves the stored element body directly; a
// child-resource read (readNestedPath set) must serve it wrapped in the parent
// response's shape — the envelope (when declared), then each collection-path
// segment as an object key, with the innermost segment holding a one-element
// array — so the generated read's path navigation finds the collection and its
// identifier selection matches the stored element. The shape is static, so the
// wrapper is a single composite literal.
func mockReadResponseExpr(route mockRoute) ast.Expr {
	if len(route.readNestedPath) == 0 {
		return astgen.Ident("body")
	}
	wrapped := ast.Expr(astgen.CompositeLit(astgen.ArrayType(nil, astgen.Ident("any")), astgen.Ident("body")))
	for i := len(route.readNestedPath) - 1; i >= 0; i-- {
		seg := route.readNestedPath[i]
		if seg == "*" {
			seg = mockWildcardCollectionKey
		}
		wrapped = astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.Ident("any")),
			astgen.KeyValueExpr(astgen.Lit(seg), wrapped),
		)
	}
	if route.readNestedEnvelope != "" {
		wrapped = astgen.CompositeLit(
			astgen.MapType(astgen.Ident("string"), astgen.Ident("any")),
			astgen.KeyValueExpr(astgen.Lit(route.readNestedEnvelope), wrapped),
		)
	}
	return wrapped
}

// statefulMockRouteHandler generates a handler closure with per-route state
// keyed by resource ID so that POST/PUT/PATCH echo the request body back and
// GET returns the stored body for the requested ID. DELETE removes the entry,
// causing subsequent GETs for that ID to return 404. Malformed JSON bodies
// are rejected with 400 BadRequest; an empty body is accepted as an empty map.
func statefulMockRouteHandler(route mockRoute, index int, schemes []ir.SecuritySchemeIR, idPrimitive ir.PrimitiveType, idKey string) []ast.Stmt {
	stateVar := fmt.Sprintf("state%d", index)
	muVar := fmt.Sprintf("mu%d", index)
	handlerVar := fmt.Sprintf("handler%d", index)
	// lastKey tracks the storage key of the most recent create/update so the
	// read handler can fall back to it when a direct path lookup misses. A
	// composite-identity resource (e.g. GitLab /groups/{id}/labels/{name}) is
	// created on a collection path but read/updated on an instance path, so the
	// create's storage key (the collection path tail) never equals the instance
	// read's lookup key; import reads yet another path (the imported name). The
	// single-entry (len==1) fallback could not bridge this once create and update
	// land in separate slots. lastKey deterministically returns the most recent
	// resource, which is the one an import against the mock should resolve to
	// (G-22).
	lastKeyVar := fmt.Sprintf("lastKey%d", index)
	// The create/update branches record the just-written key into lastKey only
	// when the route has a read branch: lastKey is declared (and read) solely
	// for the read fallback, and Go does not count an assignment as use, so a
	// create/update route without a read would leave the variable declared but
	// unused — and the assignment would reference an undeclared variable.
	var trackLastKey []ast.Stmt
	if route.read {
		trackLastKey = []ast.Stmt{astgen.AssignStmt(
			[]ast.Expr{astgen.Ident(lastKeyVar)},
			[]ast.Expr{astgen.Ident("id")},
			token.ASSIGN,
		)}
	}

	var cases []ast.Stmt

	// createMethod is the HTTP method the generated handler dispatches the
	// create branch on. PUT-as-create (upsert) resources issue Create as the
	// instance-path PUT, so the branch must match MethodPut, not a hard-coded
	// POST. The update branch below drops any method already handled here so the
	// two never emit duplicate switch labels (PUT-as-create: create and update
	// are the same instance PUT).
	createMethod := route.createMethod
	if createMethod == "" {
		createMethod = "POST"
	}

	if route.create {
		createBody := []ast.Stmt{}
		// A non-DELETE delete on a nested path (e.g. POST /pets/{id}/scrap)
		// shares the route prefix with the collection POST create, so the mock
		// cannot tell them apart by method alone. The nested-path marker — an id
		// containing '/' — identifies the delete; the collection create's id is
		// the bare placeholder (mockIDDefault), which never contains '/'.
		if route.delete && route.deleteMethod == "POST" {
			createBody = append(createBody, astgen.If(
				astgen.Call(astgen.QualExpr("strings", "Contains"), astgen.Ident("id"), astgen.Lit("/")),
				astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Ident(stateVar), astgen.Ident("id"))),
				astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(route.deleteStatus))),
				astgen.Return(),
			))
		}
		createBody = append(createBody,
			astgen.AssignSingle(astgen.Ident("body"), astgen.Call(astgen.Ident("make"), astgen.MapType(astgen.Ident("string"), astgen.Ident("interface{}")))),
			&ast.IfStmt{
				Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
					astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewDecoder"), astgen.Selector(astgen.Ident("r"), "Body")), "Decode"),
					astgen.UnaryPtr(astgen.Ident("body")),
				)),
				Cond: astgen.Binary(
					astgen.Binary(astgen.Ident("err"), token.NEQ, astgen.Nil()),
					token.LAND,
					astgen.Binary(astgen.Ident("err"), token.NEQ, astgen.QualExpr("io", "EOF")),
				),
				Body: astgen.Block(
					astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "Error"), astgen.Ident("w"), astgen.Call(astgen.Selector(astgen.Ident("err"), "Error")), astgen.QualExpr("http", "StatusBadRequest"))),
					astgen.Return(),
				),
			},
			// body was initialized with make(map[string]interface{}) above, so it
			// is never nil; the prior `if body == nil` re-initialization was dead
			// (L-25).
			//
			// Only synthesize the identity when the practitioner did not supply
			// it. A Required identity attribute (e.g. GitLab variable.key, Grafana
			// mute_timing.name) is sent in the create body; overwriting it with the
			// mock placeholder would make the echoed response disagree with the
			// plan ("was \"example\", but now \"example-id\""). A Computed identity
			// (server-generated id) is absent from the body, so it is synthesized
			// (G-22).
			&ast.IfStmt{
				Init: astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("_"), astgen.Ident("ok")},
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey))},
					token.DEFINE,
				),
				Cond: astgen.Unary(token.NOT, astgen.Ident("ok")),
				Body: astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey))},
						[]ast.Expr{mockIDValue(idPrimitive)},
						token.ASSIGN,
					),
				),
			},
			// Key state by the identity value (body[idKey]) rather than the
			// request path tail: a collection POST carries no id in the path, so
			// the path tail is the bare placeholder, while a practitioner-supplied
			// identity has the value the client will send on subsequent
			// Read/Update/Delete. Storing by identity value keeps create and update
			// in the same state slot (so the single-entry read fallback serves
			// import) and lets instance Read/Update/Delete look up by path
			// directly (G-22).
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("id")},
				[]ast.Expr{astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("%v"), astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey)))},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
				[]ast.Expr{astgen.Ident("body")},
				token.ASSIGN,
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(route.createStatus))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), astgen.Ident("body"))},
				token.ASSIGN,
			),
		)
		createBody = append(createBody, trackLastKey...)
		createBody = append(createBody, astgen.Return())
		cases = append(cases, caseWithBody([]ast.Expr{httpMethodExpr(createMethod)}, createBody...))
	}

	if route.read {
		readBody := []ast.Stmt{
			astgen.Assign(
				[]ast.Expr{astgen.Ident("body"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
			),
			// Fall back to the most recent create/update key when a direct
			// path-tail lookup misses. A composite-identity resource (e.g. GitLab
			// /groups/{id}/labels/{name}) is created on a collection path and read
			// on an instance path, so the create's storage key (the identity value)
			// never equals the instance read's lookup key (the path tail). lastKey
			// deterministically resolves the read to the most recent resource, which
			// is the one an import against the mock should resolve to. This supersedes
			// the single-entry (len==1) range fallback, which could not bridge create
			// and update once they land in separate slots (G-22).
			astgen.If(
				astgen.Binary(
					astgen.Unary(token.NOT, astgen.Ident("ok")),
					token.LAND,
					astgen.Binary(astgen.Ident(lastKeyVar), token.NEQ, astgen.Lit("")),
				),
				astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("body")},
					[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident(lastKeyVar))},
					token.ASSIGN,
				),
			),
			astgen.If(astgen.Equal(astgen.Ident("body"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "NotFound"), astgen.Ident("w"), astgen.Ident("r"))),
				astgen.Return(),
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(route.readStatus))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), mockReadResponseExpr(route))},
				token.ASSIGN,
			),
			astgen.Return(),
		}
		cases = append(cases, caseWithBody([]ast.Expr{astgen.QualExpr("http", "MethodGet")}, readBody...))
	}

	if route.update {
		// trackLastKey and the trailing Return are appended in the declaration
		// rather than as separate statements (prealloc).
		updateBody := append(append([]ast.Stmt{
			astgen.AssignSingle(astgen.Ident("body"), astgen.Call(astgen.Ident("make"), astgen.MapType(astgen.Ident("string"), astgen.Ident("interface{}")))),
			&ast.IfStmt{
				Init: astgen.AssignSingle(astgen.Ident("err"), astgen.Call(
					astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewDecoder"), astgen.Selector(astgen.Ident("r"), "Body")), "Decode"),
					astgen.UnaryPtr(astgen.Ident("body")),
				)),
				Cond: astgen.Binary(
					astgen.Binary(astgen.Ident("err"), token.NEQ, astgen.Nil()),
					token.LAND,
					astgen.Binary(astgen.Ident("err"), token.NEQ, astgen.QualExpr("io", "EOF")),
				),
				Body: astgen.Block(
					astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "Error"), astgen.Ident("w"), astgen.Call(astgen.Selector(astgen.Ident("err"), "Error")), astgen.QualExpr("http", "StatusBadRequest"))),
					astgen.Return(),
				),
			},
			// body was initialized with make(map[string]interface{}) above, so it
			// is never nil; the prior `if body == nil` re-initialization was dead
			// (L-25).
			//
			// Only synthesize the identity when absent, mirroring create: a
			// practitioner-supplied identity (GitLab variable.key) is already in
			// the update body and must not be overwritten, or the echoed response
			// would disagree with the plan (G-22).
			&ast.IfStmt{
				Init: astgen.AssignStmt(
					[]ast.Expr{astgen.Ident("_"), astgen.Ident("ok")},
					[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey))},
					token.DEFINE,
				),
				Cond: astgen.Unary(token.NOT, astgen.Ident("ok")),
				Body: astgen.Block(
					astgen.AssignStmt(
						[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey))},
						[]ast.Expr{mockIDValue(idPrimitive)},
						token.ASSIGN,
					),
				),
			},
			// Key state by the identity value (body[idKey]), matching create: the
			// path-derived id is the URL tail the client substituted, which can
			// differ from the identity value create stored under (a synthesized
			// placeholder for a Computed identity, or a practitioner-supplied
			// value for a Required one). Storing update under the same slot as
			// create lets the post-update refresh's direct path-tail lookup serve
			// the updated body instead of the stale create body, keeping the
			// refresh plan empty (issue #35).
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("id")},
				[]ast.Expr{astgen.Call(astgen.QualExpr("fmt", "Sprintf"), astgen.Lit("%v"), astgen.IndexExpr(astgen.Ident("body"), astgen.Lit(idKey)))},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
				[]ast.Expr{astgen.Ident("body")},
				token.ASSIGN,
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(route.updateStatus))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), astgen.Ident("body"))},
				token.ASSIGN,
			),
		}, trackLastKey...), astgen.Return())
		// The update branch dispatches on PUT and PATCH. Drop any method the
		// create branch already matches — PUT-as-create issues Create as the
		// instance PUT, the same op Update uses — so the two never emit
		// duplicate switch labels. When no methods remain (update was PUT-only
		// and create took PUT) the create branch already serves the upsert, so
		// the update case is omitted entirely.
		updateMethods := updateMethodList(route.create, createMethod)
		if len(updateMethods) > 0 {
			methodExprs := make([]ast.Expr, 0, len(updateMethods))
			for _, m := range updateMethods {
				methodExprs = append(methodExprs, httpMethodExpr(m))
			}
			cases = append(cases, caseWithBody(methodExprs, updateBody...))
		}
	}

	if route.delete {
		deleteBody := []ast.Stmt{
			astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Ident(stateVar), astgen.Ident("id"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(route.deleteStatus))),
			astgen.Return(),
		}
		cases = append(cases, caseWithBody([]ast.Expr{astgen.QualExpr("http", "MethodDelete")}, deleteBody...))
	}

	defaultCase := astgen.CaseClause()
	defaultCase.Body = []ast.Stmt{
		astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "Error"), astgen.Ident("w"), astgen.Lit("method not allowed"), astgen.QualExpr("http", "StatusMethodNotAllowed"))),
	}
	cases = append(cases, defaultCase)

	// Build the handler closure body: auth validation first (rejects requests
	// missing the expected credential with 401 before touching route state), then
	// the lock/defer, ID extraction, and method dispatch. mockAuthCheckStmts
	// returns nil when the provider declares no static-credential scheme, so the
	// generated handler is unchanged for unauthenticated providers.
	handlerBody := append(mockAuthCheckStmts(schemes),
		astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident(muVar), "Lock"))),
		astgen.Defer(astgen.Call(astgen.Selector(astgen.Ident(muVar), "Unlock"))),
		astgen.AssignSingle(astgen.Ident("id"), astgen.Call(
			astgen.QualExpr("strings", "Trim"),
			astgen.Call(
				astgen.QualExpr("strings", "TrimPrefix"),
				astgen.Selector(astgen.Selector(astgen.Ident("r"), "URL"), "Path"),
				astgen.Lit(route.path),
			),
			astgen.Lit("/"),
		)),
		astgen.If(astgen.Equal(astgen.Ident("id"), astgen.Lit("")), astgen.AssignStmt(
			[]ast.Expr{astgen.Ident("id")},
			[]ast.Expr{astgen.Lit(mockIDDefault(idPrimitive))},
			token.ASSIGN,
		)),
		astgen.SwitchStmt(astgen.Selector(astgen.Ident("r"), "Method"), astgen.Block(cases...)),
	)

	// lastKey is read only by the read branch's fallback. Declare it only when
	// route.read holds: a delete-only route (e.g. a dedicated reclaim path)
	// never references it, and a create/update-only route would assign it but
	// never read it — Go counts neither assignment as use, so either shape
	// without a read branch trips "declared and not used". The create/update
	// assignments to lastKey are gated on the same condition.
	stmts := []ast.Stmt{
		astgen.AssignSingle(astgen.Ident(stateVar), astgen.Call(astgen.Ident("make"), astgen.MapType(astgen.Ident("string"), astgen.MapType(astgen.Ident("string"), astgen.Ident("interface{}"))))),
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(muVar, astgen.QualExpr("sync", "Mutex"), nil))),
	}
	if route.read {
		stmts = append(stmts, astgen.AssignSingle(astgen.Ident(lastKeyVar), astgen.Lit("")))
	}
	return append(stmts,
		// The handler is bound to a variable so it can be registered on both the
		// exact collection path and its subtree: net/http's ServeMux pattern
		// "/pets" matches only that exact path, so instance URLs like
		// "/pets/example-id" would otherwise 404 and the wired Read would drop
		// the resource from state right after Create.
		astgen.AssignSingle(astgen.Ident(handlerVar), astgen.FuncLit(
			astgen.FuncType(
				astgen.Params(
					astgen.Field("w", astgen.QualExpr("http", "ResponseWriter"), ""),
					astgen.Field("r", astgen.StarExpr(astgen.QualExpr("http", "Request")), ""),
				),
				nil,
			),
			astgen.Block(handlerBody...),
		)),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("mux"), "HandleFunc"),
			astgen.Lit(route.path),
			astgen.Ident(handlerVar),
		)),
		astgen.ExprStmt(astgen.Call(
			astgen.Selector(astgen.Ident("mux"), "HandleFunc"),
			astgen.Lit(route.path+"/"),
			astgen.Ident(handlerVar),
		)),
	)
}
