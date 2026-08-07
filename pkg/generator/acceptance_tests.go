package generator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ErrUnsupportedPrimitiveType is the sentinel panicked by acceptanceExampleValue
// and updatedValue when given a PrimitiveType they do not recognize. It matches
// the sentinel-wrapped panic style used elsewhere in the package (e.g. action.go)
// rather than a bare string (L-27).
var ErrUnsupportedPrimitiveType = errors.New("unsupported primitive type")

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
	path := filepath.Join("internal", "provider", fmt.Sprintf("resource_%s_acceptance_test.go", snakeCase(r.Name)))
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
// files for every ResourceIR in the provider. Files are emitted in the order
// supplied.
func ResourceAcceptanceTestFiles(pir ir.ProviderIR, cfg BuildConfig) []File {
	files := make([]File, 0, len(pir.Resources))
	for _, r := range pir.Resources {
		files = append(files, ResourceAcceptanceTestFile(pir, r, cfg))
	}
	return files
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

	paramAttr, paramType, hasParam := acceptanceParamAttribute(r)
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
	f.AddImport("strings", "")
	f.AddImport("sync", "")
	f.AddImport("testing", "")
	f.AddImport("github.com/hashicorp/terraform-plugin-testing/helper/resource", "resource")
	f.AddImport("github.com/hashicorp/terraform-plugin-go/tfprotov6", "tfprotov6")
	f.AddImport("github.com/hashicorp/terraform-plugin-framework/providerserver", "providerserver")

	needsJSON := false
	needsIO := false

	routes := mockRoutes(r.CRUDMapping)
	for _, route := range routes {
		if route.create || route.read || route.update {
			needsJSON = true
		}
		if route.create || route.update {
			needsIO = true
		}
	}
	if needsJSON {
		f.AddImport("encoding/json", "json")
	}
	if needsIO {
		f.AddImport("io", "")
	}

	generateAcceptanceConfigFunction(f, configFuncName, configTmpl, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam)
	if hasEndpoint || hasTokenURL || hasOIDCTokenURL || hasParam {
		f.AddImport("fmt", "")
	}
	generateMockServerFunction(f, r, mockFuncName, routes, pir.SecurityIR.Schemes)

	steps, err := acceptanceTestSteps(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam, paramAttr, paramType)
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
func acceptanceTestSteps(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool, paramAttr string, paramType ir.PrimitiveType) (ast.Expr, error) {
	elems := []ast.Expr{createTestStep(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam, paramAttr, paramType)}
	if hasParam {
		elems = append(elems, updateTestStep(r, resourceAddr, configFuncName, hasEndpoint, hasTokenURL, hasOIDCTokenURL, paramAttr, paramType))
	}
	if r.Importable {
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

func createTestStep(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL, hasParam bool, paramAttr string, paramType ir.PrimitiveType) ast.Expr {
	paramValue := ""
	if hasParam {
		paramValue = acceptanceExampleValue(paramType)
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
			astgen.Lit(acceptanceExampleValue(paramType)),
		))
	}
	return astgen.CompositeLit(
		astgen.QualExpr("resource", "TestStep"),
		astgen.KeyValue("Config", astgen.Call(astgen.Ident(configFuncName), args...)),
		astgen.KeyValue("Check", composeAggregateCheckFunc(checks)),
	)
}

func updateTestStep(r ir.ResourceIR, resourceAddr, configFuncName string, hasEndpoint, hasTokenURL, hasOIDCTokenURL bool, paramAttr string, paramType ir.PrimitiveType) ast.Expr {
	args := configFuncCallArgs(hasEndpoint, hasTokenURL, hasOIDCTokenURL, true, updatedValue(paramType))
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
		astgen.Lit(updatedValue(paramType)),
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
func writeHCLAcceptanceBody(h *hclBuilder, schema ir.ObjectSchemaIR, paramAttr string) {
	for _, attr := range schema.Attributes {
		if !includeInExample(attr) {
			continue
		}
		if attr.Name == paramAttr && isPrimitiveSchema(attr.Schema) {
			if attr.Schema.Type == ir.TypeString {
				h.writeLinef("%s = \"%%s\"", attr.Name)
			} else {
				h.writeLinef("%s = %%s", attr.Name)
			}
			continue
		}
		writeHCLAcceptanceAttribute(h, attr, "")
	}
	for _, block := range schema.Blocks {
		writeHCLAcceptanceBlock(h, block)
	}
}

func writeHCLAcceptanceAttribute(h *hclBuilder, attr ir.AttributeIR, _ string) {
	s := attr.Schema
	if s.Collection != nil {
		writeHCLAcceptanceCollectionAttribute(h, attr)
		return
	}
	if isObjectLike(s) {
		h.writeLinef("%s = {", attr.Name)
		h.indent++
		writeHCLAcceptanceBody(h, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, "")
		h.indent--
		h.writeLinef("}")
		return
	}
	h.writeLinef("%s = %s", attr.Name, primitiveExampleValue(s.Type))
}

func writeHCLAcceptanceCollectionAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema
	elem := s.Collection.ElementType

	switch s.Collection.Kind {
	case ir.List, ir.Set:
		if isPrimitiveSchema(elem) {
			h.writeLinef("%s = [ %s ]", attr.Name, primitiveExampleValue(elem.Type))
			return
		}
		if isObjectLike(elem) {
			h.writeLinef("%s = [{", attr.Name)
			h.indent++
			writeHCLAcceptanceBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, "")
			h.indent--
			h.writeLinef("}]")
			return
		}
	case ir.Map:
		if isPrimitiveSchema(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef(`"key" = %s`, primitiveExampleValue(elem.Type))
			h.indent--
			h.writeLinef("}")
			return
		}
		if isObjectLike(elem) {
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
// vary across the create and update test steps. It prefers a required or
// optional string attribute and falls back to any primitive attribute.
func acceptanceParamAttribute(r ir.ResourceIR) (string, ir.PrimitiveType, bool) {
	for _, attr := range r.Schema.Attributes {
		if !includeInExample(attr) {
			continue
		}
		if attr.Schema.Collection != nil || isObjectLike(attr.Schema) {
			continue
		}
		if attr.Schema.Type == ir.TypeString {
			return attr.Name, attr.Schema.Type, true
		}
	}
	for _, attr := range r.Schema.Attributes {
		if !includeInExample(attr) {
			continue
		}
		if attr.Schema.Collection != nil || isObjectLike(attr.Schema) {
			continue
		}
		if attr.Schema.Type != "" {
			return attr.Name, attr.Schema.Type, true
		}
	}
	return "", "", false
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

func acceptanceExampleValue(t ir.PrimitiveType) string {
	switch t {
	case ir.TypeString:
		return "example"
	case ir.TypeInt:
		return "1"
	case ir.TypeFloat:
		return "1.0"
	case ir.TypeBool:
		return "true"
	case ir.TypeNull, ir.TypeDynamic:
		return "null"
	}
	panic(fmt.Errorf("%w: %q in acceptanceExampleValue", ErrUnsupportedPrimitiveType, t))
}

func updatedValue(t ir.PrimitiveType) string {
	switch t {
	case ir.TypeString:
		return "updated"
	case ir.TypeInt:
		return "2"
	case ir.TypeFloat:
		return "2.0"
	case ir.TypeBool:
		return "false"
	case ir.TypeNull, ir.TypeDynamic:
		return "null"
	}
	panic(fmt.Errorf("%w: %q in updatedValue", ErrUnsupportedPrimitiveType, t))
}

// acceptanceImportID builds a deterministic import identifier for the
// acceptance import step. It mirrors the parsing logic used by the generated
// ImportState method. When the format cannot be parsed, it returns an empty id
// along with the parse error; the sole caller (acceptanceTestSteps) surfaces
// that error as a generation error rather than silently dropping the import
// step, so a malformed ImportIDFormat fails loud instead of invisibly losing
// import test coverage (L-26).
func acceptanceImportID(r ir.ResourceIR) (string, error) {
	parsed, err := parseImportIDFormat(r.ImportIDFormat, r.IDAttribute)
	if err != nil {
		return "", err
	}
	if parsed.simple {
		return "imported-" + parsed.attrs[0], nil
	}
	parts := make([]string, len(parsed.attrs))
	for i, attr := range parsed.attrs {
		parts[i] = "imported-" + attr
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
}

func generateMockServerFunction(f *astgen.File, r ir.ResourceIR, funcName string, routes []mockRoute, schemes []ir.SecuritySchemeIR) {
	body := make([]ast.Stmt, 0, len(routes)+3)
	body = append(body, astgen.AssignSingle(astgen.Ident("mux"), astgen.Call(astgen.QualExpr("http", "NewServeMux"))))
	// Register the OAuth2 token endpoint before the resource routes so a
	// client_credentials interceptor has a token to fetch during the lifecycle.
	body = append(body, mockTokenEndpointStmts(schemes)...)
	for i, route := range routes {
		body = append(body, statefulMockRouteHandler(route, i, schemes)...)
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

// mockRoutes aggregates CRUD operations into route stubs keyed by the path prefix
// up to the first '{' parameter. Operations that share that prefix are merged
// and dispatched by HTTP method in the generated handler. Nested paths such as
// POST /pets/{id}/action share the same prefix as POST /pets and would be
// merged, so only first-segment-level CRUD routes are currently stubbed.
func mockRoutes(m ir.CRUDMappingIR) []mockRoute {
	byPath := map[string]mockRoute{}

	addRoute := func(pathTemplate, method string) {
		path := pathTemplate
		if idx := strings.Index(pathTemplate, "{"); idx >= 0 {
			path = pathTemplate[:idx]
		}
		path = strings.TrimRight(path, "/")
		if path == "" {
			return
		}
		route := byPath[path]
		route.path = path
		switch method {
		case "POST":
			route.create = true
		case "GET":
			route.read = true
		case "PUT", "PATCH":
			route.update = true
		case "DELETE":
			route.delete = true
		}
		byPath[path] = route
	}

	addRoute(m.Create.PathTemplate, m.Create.Method)
	addRoute(m.Read.PathTemplate, m.Read.Method)
	if m.Update != nil {
		addRoute(m.Update.PathTemplate, m.Update.Method)
	}
	addRoute(m.Delete.PathTemplate, m.Delete.Method)

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

// mockAuthCandidates builds the per-scheme credential-check candidates. The
// hasTokenFetching and hasHTTPBearer flags carry the cross-scheme
// Authorization-header conflict: when both are present neither Authorization
// assertion is emitted (last writer wins under AND semantics), so either could
// spuriously fail depending on interceptor order.
func mockAuthCandidates(schemes []ir.SecuritySchemeIR, hasTokenFetching bool) []mockAuthCandidate {
	hasHTTPBearer := httpBearerInSchemes(schemes)
	var candidates []mockAuthCandidate
	for i, s := range schemes {
		if c, ok := mockAuthCandidateForScheme(s, i, hasTokenFetching, hasHTTPBearer); ok {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

// mockAuthCandidateForScheme returns the credential-check candidate for a single
// scheme, or ok=false when the scheme has no assertable interceptor. The
// Authorization-header conflict skips are applied here.
func mockAuthCandidateForScheme(s ir.SecuritySchemeIR, i int, hasTokenFetching, hasHTTPBearer bool) (mockAuthCandidate, bool) {
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
		return mockAuthCandidate{s.Name, mockAPIKeyAuthCheck(loc, name, i)}, true
	case ir.SecuritySchemeHTTP:
		switch strings.ToLower(s.Scheme) {
		case "basic":
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

// statefulMockRouteHandler generates a handler closure with per-route state
// keyed by resource ID so that POST/PUT/PATCH echo the request body back and
// GET returns the stored body for the requested ID. DELETE removes the entry,
// causing subsequent GETs for that ID to return 404. Malformed JSON bodies
// are rejected with 400 BadRequest; an empty body is accepted as an empty map.
func statefulMockRouteHandler(route mockRoute, index int, schemes []ir.SecuritySchemeIR) []ast.Stmt {
	stateVar := fmt.Sprintf("state%d", index)
	muVar := fmt.Sprintf("mu%d", index)
	handlerVar := fmt.Sprintf("handler%d", index)

	var cases []ast.Stmt

	if route.create {
		createBody := []ast.Stmt{
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
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit("id"))},
				[]ast.Expr{astgen.Lit("example-id")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
				[]ast.Expr{astgen.Ident("body")},
				token.ASSIGN,
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(201))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), astgen.Ident("body"))},
				token.ASSIGN,
			),
			astgen.Return(),
		}
		cases = append(cases, caseWithBody([]ast.Expr{astgen.QualExpr("http", "MethodPost")}, createBody...))
	}

	if route.read {
		readBody := []ast.Stmt{
			astgen.Assign(
				[]ast.Expr{astgen.Ident("body"), astgen.Ident("ok")},
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
			),
			astgen.If(
				astgen.Binary(
					astgen.Unary(token.NOT, astgen.Ident("ok")),
					token.LAND,
					astgen.Equal(astgen.Call(astgen.Ident("len"), astgen.Ident(stateVar)), astgen.IntLit(1)),
				),
				astgen.RangeStmt(
					astgen.Ident("_"),
					astgen.Ident("v"),
					token.DEFINE,
					astgen.Ident(stateVar),
					astgen.Block(
						astgen.AssignStmt(
							[]ast.Expr{astgen.Ident("body")},
							[]ast.Expr{astgen.Ident("v")},
							token.ASSIGN,
						),
						astgen.Break(),
					),
				),
			),
			astgen.If(astgen.Equal(astgen.Ident("body"), astgen.Nil()),
				astgen.ExprStmt(astgen.Call(astgen.QualExpr("http", "NotFound"), astgen.Ident("w"), astgen.Ident("r"))),
				astgen.Return(),
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(200))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), astgen.Ident("body"))},
				token.ASSIGN,
			),
			astgen.Return(),
		}
		cases = append(cases, caseWithBody([]ast.Expr{astgen.QualExpr("http", "MethodGet")}, readBody...))
	}

	if route.update {
		updateBody := []ast.Stmt{
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
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident("body"), astgen.Lit("id"))},
				[]ast.Expr{astgen.Ident("id")},
				token.ASSIGN,
			),
			astgen.AssignStmt(
				[]ast.Expr{astgen.IndexExpr(astgen.Ident(stateVar), astgen.Ident("id"))},
				[]ast.Expr{astgen.Ident("body")},
				token.ASSIGN,
			),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Call(astgen.Selector(astgen.Ident("w"), "Header")), "Set"), astgen.Lit("Content-Type"), astgen.Lit("application/json"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(200))),
			astgen.AssignStmt(
				[]ast.Expr{astgen.Ident("_")},
				[]ast.Expr{astgen.Call(astgen.Selector(astgen.Call(astgen.QualExpr("json", "NewEncoder"), astgen.Ident("w")), "Encode"), astgen.Ident("body"))},
				token.ASSIGN,
			),
			astgen.Return(),
		}
		cases = append(cases, caseWithBody([]ast.Expr{
			astgen.QualExpr("http", "MethodPut"),
			astgen.QualExpr("http", "MethodPatch"),
		}, updateBody...))
	}

	if route.delete {
		deleteBody := []ast.Stmt{
			astgen.ExprStmt(astgen.Call(astgen.Ident("delete"), astgen.Ident(stateVar), astgen.Ident("id"))),
			astgen.ExprStmt(astgen.Call(astgen.Selector(astgen.Ident("w"), "WriteHeader"), astgen.IntLit(204))),
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
			[]ast.Expr{astgen.Lit("example-id")},
			token.ASSIGN,
		)),
		astgen.SwitchStmt(astgen.Selector(astgen.Ident("r"), "Method"), astgen.Block(cases...)),
	)

	return []ast.Stmt{
		astgen.AssignSingle(astgen.Ident(stateVar), astgen.Call(astgen.Ident("make"), astgen.MapType(astgen.Ident("string"), astgen.MapType(astgen.Ident("string"), astgen.Ident("interface{}"))))),
		astgen.DeclStmt(astgen.VarDeclGen(astgen.VarSpec(muVar, astgen.QualExpr("sync", "Mutex"), nil))),
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
	}
}
