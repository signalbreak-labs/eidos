package generator

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestNewBuildVersions asserts NewBuildVersions populates every field with the
// pinned package default, including the framework-validators version added for
// the G39 standard validators wiring. Generated resources now carry a plain
// Int64-seconds timeouts block, so no framework-timeouts version participates
// anymore (M-14 revisited).
func TestNewBuildVersions(t *testing.T) {
	v := NewBuildVersions()
	want := BuildVersions{
		FrameworkVersion:  TerraformPluginFrameworkVersion,
		PluginGoVersion:   TerraformPluginGoVersion,
		PluginLogVersion:  TerraformPluginLogVersion,
		TestingVersion:    TerraformPluginTestingVersion,
		ValidatorsVersion: TerraformPluginFrameworkValidatorsVersion,
	}
	if v != want {
		t.Errorf("NewBuildVersions() = %+v, want %+v", v, want)
	}
}

// TestQuoteEach asserts quoteEach double-quotes every element without mutating
// the input.
func TestQuoteEach(t *testing.T) {
	in := []string{"a", "b c"}
	got := quoteEach(in)
	if len(got) != 2 || got[0] != `"a"` || got[1] != `"b c"` {
		t.Errorf("quoteEach(%v) = %v", in, got)
	}
	if in[0] != "a" || in[1] != "b c" {
		t.Errorf("quoteEach mutated its input: %v", in)
	}
	if len(quoteEach(nil)) != 0 {
		t.Error("quoteEach(nil) should return an empty slice")
	}
}

// TestGoDurationExpr covers every branch of goDurationExpr from zero through
// the sub-microsecond fallback.
func TestGoDurationExpr(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0"},
		{2 * time.Hour, "2 * time.Hour"},
		{90 * time.Minute, "90 * time.Minute"}, // not a whole number of hours
		{45 * time.Second, "45 * time.Second"},
		{500 * time.Millisecond, "500 * time.Millisecond"},
		{1500 * time.Microsecond, "1500 * time.Microsecond"},
		{time.Duration(1), "1 * time.Nanosecond"}, // 1ns falls through to nanos
	}
	for _, tc := range cases {
		if got := goDurationExpr(tc.d); got != tc.want {
			t.Errorf("goDurationExpr(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestCollectionKindLabel covers the three recognized kinds plus the
// capitalize fallback.
func TestCollectionKindLabel(t *testing.T) {
	cases := []struct {
		k    ir.CollectionKind
		want string
	}{
		{ir.List, "List"},
		{ir.Set, "Set"},
		{ir.Map, "Map"},
		{ir.CollectionKind("tuple"), "Tuple"},
	}
	for _, tc := range cases {
		if got := collectionKindLabel(tc.k); got != tc.want {
			t.Errorf("collectionKindLabel(%q) = %q, want %q", tc.k, got, tc.want)
		}
	}
}

// TestValidateProviderPrimitiveType covers the accepted primitive types, the
// null/empty/unrecognized error paths.
func TestValidateProviderPrimitiveType(t *testing.T) {
	for _, typ := range []ir.PrimitiveType{ir.TypeString, ir.TypeInt, ir.TypeFloat, ir.TypeBool, ir.TypeDynamic} {
		if err := validateProviderPrimitiveType(ir.SchemaIR{Type: typ}, "test"); err != nil {
			t.Errorf("validateProviderPrimitiveType(%q) = %v, want nil", typ, err)
		}
	}
	bad := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeNull, "unsupported primitive type"},
		{"", "no recognizable type"},
		{"custom", "unsupported primitive type"},
	}
	for _, tc := range bad {
		err := validateProviderPrimitiveType(ir.SchemaIR{Type: tc.typ}, "test")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateProviderPrimitiveType(%q) = %v, want error containing %q", tc.typ, err, tc.want)
		}
	}
}

// TestTerraformTestVariableValue covers the collection and primitive branches
// of terraformTestVariableValue.
func TestTerraformTestVariableValue(t *testing.T) {
	strAttr := func(c *ir.CollectionType) ir.AttributeIR {
		return ir.AttributeIR{Schema: ir.SchemaIR{Collection: c}}
	}
	listStr := &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}
	mapStr := &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeString}}

	cases := []struct {
		name string
		attr ir.AttributeIR
		want string
	}{
		{"list-of-string", strAttr(listStr), `["example"]`},
		{"set-of-string", strAttr(&ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}}), `["example"]`},
		{"map-of-string", strAttr(mapStr), `{ key = "example" }`},
		// List/set with a non-primitive element falls through to the empty list.
		{"list-of-object", strAttr(&ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x"}}}}), "[]"},
		// Map with a non-primitive element also falls through to [].
		{"map-of-object", strAttr(&ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x"}}}}), "[]"},
		{"primitive-string", ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeString}}, `"example"`},
		{"primitive-int", ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeInt}}, "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := terraformTestVariableValue(tc.attr); got != tc.want {
				t.Errorf("terraformTestVariableValue() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAnyStringValue covers the string, nil, and non-string branches of
// anyStringValue.
func TestAnyStringValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{"", ""},
		{nil, ""},
		{42, "42"},
		{3.5, "3.5"},
		{true, "true"},
	}
	for _, tc := range cases {
		if got := anyStringValue(tc.in); got != tc.want {
			t.Errorf("anyStringValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWriteTerraformTestProviderAttribute covers the collection, object-like,
// and primitive branches of writeTerraformTestProviderAttribute.
func TestWriteTerraformTestProviderAttribute(t *testing.T) {
	render := func(attr ir.AttributeIR) string {
		var h hclBuilder
		writeTerraformTestProviderAttribute(&h, attr)
		return h.b.String()
	}

	// Collection attribute.
	got := render(ir.AttributeIR{Name: "tags", Required: true, Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}})
	if !strings.Contains(got, "tags = ") {
		t.Errorf("collection attribute = %q, want tags assignment", got)
	}
	// Object-like attribute.
	got = render(ir.AttributeIR{Name: "owner", Required: true, Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}}})
	if !strings.Contains(got, "owner = {") || !strings.Contains(got, "name = ") {
		t.Errorf("object attribute = %q, want nested body", got)
	}
	// Primitive attribute.
	got = render(ir.AttributeIR{Name: "count", Required: true, Schema: ir.SchemaIR{Type: ir.TypeInt}})
	if !strings.Contains(got, "count = ") {
		t.Errorf("primitive attribute = %q, want assignment", got)
	}
}

// TestTerraformTestVariableType covers the List/Set/Map collection branches
// and the primitive fallback of terraformTestVariableType.
func TestTerraformTestVariableType(t *testing.T) {
	col := func(kind ir.CollectionKind) ir.AttributeIR {
		return ir.AttributeIR{Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: kind, ElementType: ir.SchemaIR{Type: ir.TypeString}}}}
	}
	cases := []struct {
		attr ir.AttributeIR
		want string
	}{
		{col(ir.List), "list(string)"},
		{col(ir.Set), "set(string)"},
		{col(ir.Map), "map(string)"},
		{ir.AttributeIR{Schema: ir.SchemaIR{Type: ir.TypeString}}, "string"},
	}
	for _, tc := range cases {
		if got := terraformTestVariableType(tc.attr); got != tc.want {
			t.Errorf("terraformTestVariableType() = %q, want %q", got, tc.want)
		}
	}
}

// TestTerraformTestPrimitiveTypeName covers every branch of
// terraformTestPrimitiveTypeName including the default fallback.
func TestTerraformTestPrimitiveTypeName(t *testing.T) {
	cases := []struct {
		typ  ir.PrimitiveType
		want string
	}{
		{ir.TypeString, "string"},
		{ir.TypeInt, "number"},
		{ir.TypeFloat, "number"},
		{ir.TypeBool, "bool"},
		{ir.TypeDynamic, "any"},
		{ir.TypeNull, "string"}, // default fallback
		{"", "string"},          // default fallback
	}
	for _, tc := range cases {
		if got := terraformTestPrimitiveTypeName(tc.typ); got != tc.want {
			t.Errorf("terraformTestPrimitiveTypeName(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// TestIDBodyJSON covers every idFieldInfo branch of idBodyJSON: not found, and
// each primitive's JSON literal.
func TestIDBodyJSON(t *testing.T) {
	if got := idBodyJSON(idFieldInfo{}); got != `{}` {
		t.Errorf("not found = %q, want {}", got)
	}
	cases := []struct {
		info idFieldInfo
		want string
	}{
		{idFieldInfo{found: true, attr: "id", primitive: ir.TypeInt}, `{"id":1}`},
		{idFieldInfo{found: true, attr: "id", primitive: ir.TypeFloat}, `{"id":1.0}`},
		{idFieldInfo{found: true, attr: "id", primitive: ir.TypeBool}, `{"id":true}`},
		{idFieldInfo{found: true, attr: "id", primitive: ir.TypeString}, `{"id":"example-id"}`},
		{idFieldInfo{found: true, attr: "id", primitive: ir.TypeDynamic}, `{"id":"example-id"}`},
	}
	for _, tc := range cases {
		if got := idBodyJSON(tc.info); got != tc.want {
			t.Errorf("idBodyJSON(%+v) = %q, want %q", tc.info, got, tc.want)
		}
	}
}

// TestDatasourceAttributeExprWithPath covers the top-level non-nil path and the
// nested unrepresentable-shape nil path of datasourceAttributeExprWithPath.
func TestDatasourceAttributeExprWithPath(t *testing.T) {
	// Top-level primitive renders a StringAttribute.
	expr := datasourceAttributeExprWithPath(ir.AttributeIR{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}, "")
	if expr == nil || !strings.Contains(renderExpr(t, expr), "StringAttribute") {
		t.Errorf("top-level primitive = %v", renderExpr(t, expr))
	}
	// A nested collection with a dynamic element is unrepresentable → nil.
	if expr := datasourceAttributeExprWithPath(ir.AttributeIR{
		Name:   "blob",
		Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeDynamic}}},
	}, "parent"); expr != nil {
		t.Errorf("nested dynamic collection should be nil, got %v", renderExpr(t, expr))
	}
}

// TestPrimitiveAttrType covers every primitive branch plus the fallback.
func TestPrimitiveAttrType(t *testing.T) {
	cases := map[ir.PrimitiveType]string{
		ir.TypeString:  "types.StringType",
		ir.TypeInt:     "types.Int64Type",
		ir.TypeFloat:   "types.Float64Type",
		ir.TypeBool:    "types.BoolType",
		ir.TypeDynamic: "types.DynamicType",
		"custom":       "types.StringType", // fallback
	}
	for typ, want := range cases {
		if got := renderExpr(t, primitiveAttrType(typ)); got != want {
			t.Errorf("primitiveAttrType(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestElementValueExpr covers the int/float/bool type assertions and the string
// default of elementValueExpr.
func TestElementValueExpr(t *testing.T) {
	cases := map[ir.PrimitiveType]string{
		ir.TypeInt:    "strconv.FormatInt(elem.(types.Int64).ValueInt64(), 10)",
		ir.TypeFloat:  "strconv.FormatFloat(elem.(types.Float64).ValueFloat64(), 'f', -1, 64)",
		ir.TypeBool:   "strconv.FormatBool(elem.(types.Bool).ValueBool())",
		ir.TypeString: "elem.(types.String).ValueString()",
		"custom":      "elem.(types.String).ValueString()", // default
	}
	for typ, want := range cases {
		if got := renderExpr(t, elementValueExpr("elem", typ)); got != want {
			t.Errorf("elementValueExpr(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestComposeAggregateCheckFunc covers the empty and non-empty branches.
func TestComposeAggregateCheckFunc(t *testing.T) {
	empty := composeAggregateCheckFunc(nil)
	if got := renderExpr(t, empty); got != "resource.ComposeAggregateTestCheckFunc()" {
		t.Errorf("empty = %q", got)
	}
	one := composeAggregateCheckFunc([]ast.Expr{astgen.Ident("check1")})
	if got := renderExpr(t, one); got != "resource.ComposeAggregateTestCheckFunc(check1)" {
		t.Errorf("one = %q", got)
	}
}

// TestMockIDDefault covers every primitive branch of mockIDDefault.
func TestMockIDDefault(t *testing.T) {
	cases := map[ir.PrimitiveType]string{
		ir.TypeInt:     "1",
		ir.TypeFloat:   "1",
		ir.TypeBool:    "true",
		ir.TypeString:  "example-id",
		ir.TypeDynamic: "example-id",
		"custom":       "example-id",
	}
	for typ, want := range cases {
		if got := mockIDDefault(typ); got != want {
			t.Errorf("mockIDDefault(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestMockIDValue covers every primitive branch of mockIDValue.
func TestMockIDValue(t *testing.T) {
	cases := map[ir.PrimitiveType]string{
		ir.TypeInt:     "1",
		ir.TypeFloat:   "1.0",
		ir.TypeBool:    "true",
		ir.TypeString:  `"example-id"`,
		ir.TypeDynamic: `"example-id"`,
		"custom":       `"example-id"`,
	}
	for typ, want := range cases {
		if got := renderExpr(t, mockIDValue(typ)); got != want {
			t.Errorf("mockIDValue(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestWriteHCLAcceptanceCollectionAttribute covers the list/set primitive,
// list/set object, map primitive, and map object branches.
func TestWriteHCLAcceptanceCollectionAttribute(t *testing.T) {
	// List of primitives → single-line list.
	var h hclBuilder
	writeHCLAcceptanceCollectionAttribute(&h, ir.AttributeIR{Name: "tags", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}}})
	if got := h.b.String(); !strings.Contains(got, `tags = ["example"]`) {
		t.Errorf("list of primitives = %q", got)
	}

	// Set of objects → list-of-objects literal.
	var h2 hclBuilder
	writeHCLAcceptanceCollectionAttribute(&h2, ir.AttributeIR{Name: "endpoints", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}}}}}})
	got2 := h2.b.String()
	if !strings.Contains(got2, "endpoints = [{") || !strings.Contains(got2, `url = "example"`) {
		t.Errorf("set of objects = %q", got2)
	}

	// Map of primitives → map literal.
	var h3 hclBuilder
	writeHCLAcceptanceCollectionAttribute(&h3, ir.AttributeIR{Name: "labels", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: schemaType(ir.TypeString)}}})
	got3 := h3.b.String()
	if !strings.Contains(got3, "labels = {") || !strings.Contains(got3, `"key" = "example"`) {
		t.Errorf("map of primitives = %q", got3)
	}

	// Map of objects → nested map literal.
	var h4 hclBuilder
	writeHCLAcceptanceCollectionAttribute(&h4, ir.AttributeIR{Name: "endpoints", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}}}}}})
	got4 := h4.b.String()
	if !strings.Contains(got4, "endpoints = {") || !strings.Contains(got4, `"key" = {`) || !strings.Contains(got4, `url = "example"`) {
		t.Errorf("map of objects = %q", got4)
	}
}

// TestDocsBlockGroup covers the required and optional branches.
func TestDocsBlockGroup(t *testing.T) {
	minItems := int64(1)
	if got := docsBlockGroup(ir.BlockIR{MinItems: &minItems}); got != "Required" {
		t.Errorf("required block = %q, want Required", got)
	}
	if got := docsBlockGroup(ir.BlockIR{}); got != "Optional" {
		t.Errorf("optional block = %q, want Optional", got)
	}
}

// TestObjectArgumentPlaceholder covers the non-object-like null fallback and
// the object literal with attributes and blocks.
func TestObjectArgumentPlaceholder(t *testing.T) {
	if got := objectArgumentPlaceholder(schemaType(ir.TypeString), "label"); got != "null" {
		t.Errorf("non-object = %q, want null", got)
	}
	got := objectArgumentPlaceholder(ir.SchemaIR{Attributes: []ir.AttributeIR{
		{Name: "name", Schema: schemaType(ir.TypeString)},
		{Name: "count", Schema: schemaType(ir.TypeInt)},
	}}, "label")
	if !strings.Contains(got, "name = ") || !strings.Contains(got, "count = ") {
		t.Errorf("object placeholder = %q", got)
	}
}

// TestWriteHCLCollectionLiteral covers the list/set object, map primitive, and
// map object branches.
func TestWriteHCLCollectionLiteral(t *testing.T) {
	// List of objects → list-of-objects literal.
	var h hclBuilder
	writeHCLCollectionLiteral(&h, ir.AttributeIR{Name: "endpoints", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}}}}}})
	got := h.b.String()
	if !strings.Contains(got, "endpoints = [{") || !strings.Contains(got, `url = "example"`) {
		t.Errorf("list of objects = %q", got)
	}

	// Map of primitives → map literal with a domain key.
	var h2 hclBuilder
	writeHCLCollectionLiteral(&h2, ir.AttributeIR{Name: "labels", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: schemaType(ir.TypeString)}}})
	got2 := h2.b.String()
	if !strings.Contains(got2, "labels = {") || !strings.Contains(got2, `"labels" = "example"`) {
		t.Errorf("map of primitives = %q", got2)
	}

	// Map of objects → nested map literal.
	var h3 hclBuilder
	writeHCLCollectionLiteral(&h3, ir.AttributeIR{Name: "endpoints", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}}}}}})
	got3 := h3.b.String()
	if !strings.Contains(got3, "endpoints = {") || !strings.Contains(got3, `"endpoints" = {`) || !strings.Contains(got3, `url = "example"`) {
		t.Errorf("map of objects = %q", got3)
	}
}

// TestListPageItemsRemoteStmts covers the no-envelope and envelope branches.
func TestListPageItemsRemoteStmts(t *testing.T) {
	plain := listPageItemsRemoteStmts("Could not decode list page", "")
	if len(plain) != 2 {
		t.Fatalf("no-envelope stmts = %d, want 2", len(plain))
	}
	env := listPageItemsRemoteStmts("Could not decode list page", "data")
	if len(env) != 6 {
		t.Fatalf("envelope stmts = %d, want 6", len(env))
	}
	if got := renderStmts(t, env); !strings.Contains(got, `"data"`) {
		t.Errorf("envelope stmts missing envelope key:\n%s", got)
	}
}

// TestProviderSchemaValues covers the description, attributes, and blocks
// branches plus the error path.
func TestProviderSchemaValues(t *testing.T) {
	pir := ir.ProviderIR{
		Description: "  A provider  ",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{{Name: "api_key", Schema: schemaType(ir.TypeString)}},
			Blocks: []ir.BlockIR{{
				Name:        "endpoint",
				NestingMode: ir.NestingSingle,
				Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Schema: schemaType(ir.TypeString)}}},
			}},
		},
	}
	elems, err := providerSchemaValues(pir)
	if err != nil {
		t.Fatalf("providerSchemaValues error = %v", err)
	}
	if len(elems) != 3 {
		t.Errorf("elems = %d, want 3 (description, attributes, blocks)", len(elems))
	}

	// Unsupported block nesting mode → error.
	bad := ir.ProviderIR{ConfigSchema: ir.ObjectSchemaIR{Blocks: []ir.BlockIR{{Name: "bad", NestingMode: "exotic"}}}}
	if _, err := providerSchemaValues(bad); err == nil {
		t.Error("expected error for unsupported nesting mode")
	}
}

// TestOAuth2Stmts covers nil flows, each selected flow, the priority fallback,
// and the implicit-only nil path.
func TestOAuth2Stmts(t *testing.T) {
	// byName carries the auth config attributes the interceptor builders look
	// up; without them the builders return nil.
	byName := map[string]ir.AttributeIR{
		"client_id":     {Name: "client_id", Schema: schemaType(ir.TypeString)},
		"client_secret": {Name: "client_secret", Schema: schemaType(ir.TypeString)},
		"username":      {Name: "username", Schema: schemaType(ir.TypeString)},
		"password":      {Name: "password", Schema: schemaType(ir.TypeString)},
		"token_url":     {Name: "token_url", Schema: schemaType(ir.TypeString)},
		"scopes":        {Name: "scopes", Schema: schemaType(ir.TypeString)},
		"refresh_token": {Name: "refresh_token", Schema: schemaType(ir.TypeString)},
	}
	if got := oauth2Stmts(ir.SecuritySchemeIR{}, nil); got != nil {
		t.Errorf("nil flows = %v, want nil", got)
	}
	cc := &ir.OAuthFlowIR{TokenURL: "https://api.example.com/oauth/token"}
	pw := &ir.OAuthFlowIR{TokenURL: "https://api.example.com/oauth/password"}
	ac := &ir.OAuthFlowIR{TokenURL: "https://api.example.com/oauth/authcode"}

	// Selected flow wins.
	scheme := ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{ClientCredentials: cc, Password: pw}, SelectedFlow: "password"}
	if got := oauth2Stmts(scheme, byName); got == nil || !strings.Contains(renderStmts(t, got), "OAuth2Password") {
		t.Errorf("selected password = %v", renderStmts(t, got))
	}
	// Selected flow naming an undeclared flow falls back to priority order.
	scheme2 := ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{ClientCredentials: cc}, SelectedFlow: "password"}
	if got := oauth2Stmts(scheme2, byName); got == nil || !strings.Contains(renderStmts(t, got), "OAuth2ClientCredentials") {
		t.Errorf("fallback = %v", renderStmts(t, got))
	}
	// Priority order: client_credentials, then password, then authorization_code.
	scheme3 := ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{Password: pw, AuthorizationCode: ac}}
	if got := oauth2Stmts(scheme3, byName); got == nil || !strings.Contains(renderStmts(t, got), "OAuth2Password") {
		t.Errorf("priority password = %v", renderStmts(t, got))
	}
	scheme4 := ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{AuthorizationCode: ac}}
	if got := oauth2Stmts(scheme4, byName); got == nil || !strings.Contains(renderStmts(t, got), "OAuth2AuthorizationCodeRefresh") {
		t.Errorf("priority authcode = %v", renderStmts(t, got))
	}
	// Implicit-only → nil.
	scheme5 := ir.SecuritySchemeIR{Flows: &ir.OAuthFlowsIR{Implicit: &ir.OAuthFlowIR{AuthorizationURL: "https://api.example.com/oauth/authorize"}}}
	if got := oauth2Stmts(scheme5, byName); got != nil {
		t.Errorf("implicit-only = %v, want nil", renderStmts(t, got))
	}
}

// TestPathValueExpr covers the literal and model-field branches.
func TestPathValueExpr(t *testing.T) {
	if got := renderExpr(t, pathValueExpr("model", pathSubstitution{literal: "v1"})); got != `"v1"` {
		t.Errorf("literal = %q", got)
	}
	if got := renderExpr(t, pathValueExpr("model", pathSubstitution{field: "Id", primitive: ir.TypeString})); got != "model.Id.ValueString()" {
		t.Errorf("field = %q", got)
	}
}

// TestScalarValueExpr covers the int/float/bool formatting and the string
// default.
func TestScalarValueExpr(t *testing.T) {
	sel := astgen.Selector(astgen.Ident("m"), "Field")
	cases := map[ir.PrimitiveType]string{
		ir.TypeInt:    "strconv.FormatInt(m.Field.ValueInt64(), 10)",
		ir.TypeFloat:  "strconv.FormatFloat(m.Field.ValueFloat64(), 'f', -1, 64)",
		ir.TypeBool:   "strconv.FormatBool(m.Field.ValueBool())",
		ir.TypeString: "m.Field.ValueString()",
		"custom":      "m.Field.ValueString()",
	}
	for typ, want := range cases {
		if got := renderExpr(t, scalarValueExpr(sel, typ)); got != want {
			t.Errorf("scalarValueExpr(%q) = %q, want %q", typ, got, want)
		}
	}
}

// TestSchemaReferencesAttr covers the collection recursion, object-like, and
// primitive branches.
func TestSchemaReferencesAttr(t *testing.T) {
	if !schemaReferencesAttr(ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}) {
		t.Error("object-like schema must reference attr")
	}
	if !schemaReferencesAttr(ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: schemaType(ir.TypeString)}}}}}) {
		t.Error("collection of objects must reference attr")
	}
	if schemaReferencesAttr(schemaType(ir.TypeString)) {
		t.Error("primitive schema must not reference attr")
	}
	if schemaReferencesAttr(ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: schemaType(ir.TypeString)}}) {
		t.Error("collection of primitives must not reference attr")
	}
}

// TestUpgradeStateMethod covers the no-upgrades nil path and the with-upgrades
// method path.
func TestUpgradeStateMethod(t *testing.T) {
	if got := upgradeStateMethod(ir.ResourceIR{}); got != nil {
		t.Errorf("no upgrades = %v, want nil", got)
	}
	r := ir.ResourceIR{
		Name: "widget",
		StateUpgrades: []ir.StateUpgradeIR{
			{FromVersion: 0, Renames: map[string]string{"old": "new"}},
		},
	}
	got := upgradeStateMethod(r)
	if got == nil {
		t.Fatal("expected an UpgradeState method")
	}
	rendered := renderDecl(t, got)
	if !strings.Contains(rendered, "func (r *WidgetResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader") {
		t.Errorf("method = %q", rendered)
	}
}

// TestWriteTerraformTestProviderConfig covers required attributes and blocks.
func TestWriteTerraformTestProviderConfig(t *testing.T) {
	var h hclBuilder
	pir := ir.ProviderIR{ConfigSchema: ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "api_key", Required: true, Schema: schemaType(ir.TypeString)},
			{Name: "optional", Schema: schemaType(ir.TypeString)},
		},
		Blocks: []ir.BlockIR{{
			Name:        "endpoint",
			NestingMode: ir.NestingSingle,
			Schema:      ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "url", Required: true, Schema: schemaType(ir.TypeString)}}},
		}},
	}}
	writeTerraformTestProviderConfig(&h, pir)
	got := h.b.String()
	if !strings.Contains(got, "api_key = ") {
		t.Errorf("required attribute missing:\n%s", got)
	}
	if strings.Contains(got, "optional = ") {
		t.Errorf("optional attribute must be skipped:\n%s", got)
	}
	if !strings.Contains(got, "endpoint {") || !strings.Contains(got, `url = "example"`) {
		t.Errorf("block missing:\n%s", got)
	}
}

// renderStmts formats a statement list as Go source for substring assertions by
// wrapping it in a throwaway function body.
func renderStmts(t *testing.T, stmts []ast.Stmt) string {
	t.Helper()
	fn := astgen.FuncLit(astgen.FuncType(nil, nil), astgen.Block(stmts...))
	return renderExpr(t, fn)
}

// renderDecl formats a top-level declaration as Go source for substring
// assertions.
func renderDecl(t *testing.T, decl ast.Decl) string {
	t.Helper()
	f := astgen.NewFile("test")
	f.AddDecl(decl)
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), f.AST()); err != nil {
		t.Fatalf("format decl: %v", err)
	}
	return buf.String()
}
