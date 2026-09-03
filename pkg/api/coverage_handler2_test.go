package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// cancelAfterCalls is a context whose Err() returns nil for the first
// `remaining` calls and context.Canceled afterwards. It drives the mid-pipeline
// checkCtx branches of validateContext, which are otherwise unreachable with a
// plain canceled context (the entry check would return first).
type cancelAfterCalls struct {
	context.Context
	remaining int
}

func (c *cancelAfterCalls) Err() error {
	if c.remaining > 0 {
		c.remaining--
		return nil
	}
	return context.Canceled
}

// TestValidateContext_CanceledMidPipeline drives the checkCtx branches after
// each pipeline stage (load, extractConfig, convert, config) by canceling the
// context at a specific call count.
func TestValidateContext_CanceledMidPipeline(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining int
	}{
		{"after load", 1},
		{"after extractConfig", 2},
		{"after convert", 3},
		{"after config", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &cancelAfterCalls{Context: context.Background(), remaining: tc.remaining}
			resp := ValidateContextWithContentType(ctx, []byte(minimalSpec), "")
			if resp.Valid {
				t.Error("expected invalid response for a canceled context")
			}
			found := false
			for _, d := range resp.Diagnostics {
				if d.Summary == "Request canceled" {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a Request canceled diagnostic, got %+v", resp.Diagnostics)
			}
		})
	}
}

// TestLooksLikeSpecRoot drives the non-map, nil-key, and non-string-key branches
// of looksLikeSpecRoot.
func TestLooksLikeSpecRoot(t *testing.T) {
	if looksLikeSpecRoot(&parser.SequenceNode{}) {
		t.Error("a sequence must not look like a spec root")
	}
	// A nil key is skipped; the openapi key still matches.
	m := &parser.MapNode{Entries: []parser.MapEntry{
		{Key: nil, Value: &parser.ScalarNode{Value: "x"}},
		{Key: &parser.ScalarNode{Value: "openapi"}, Value: &parser.ScalarNode{Value: "3.0.0"}},
	}}
	if !looksLikeSpecRoot(m) {
		t.Error("map with an openapi key must look like a spec root")
	}
	// A non-string key value is skipped; the swagger key still matches.
	m2 := &parser.MapNode{Entries: []parser.MapEntry{
		{Key: &parser.ScalarNode{Value: 42}, Value: &parser.ScalarNode{Value: "x"}},
		{Key: &parser.ScalarNode{Value: "swagger"}, Value: &parser.ScalarNode{Value: "2.0"}},
	}}
	if !looksLikeSpecRoot(m2) {
		t.Error("map with a swagger key must look like a spec root")
	}
	// No version key → false.
	if looksLikeSpecRoot(&parser.MapNode{Entries: []parser.MapEntry{
		{Key: &parser.ScalarNode{Value: "a"}, Value: &parser.ScalarNode{Value: "1"}},
	}}) {
		t.Error("map without a version key must not look like a spec root")
	}
}

// TestExtractConfig_NonMapAndSkippedKeys drives the non-map root, nil-key, and
// non-string-key branches of extractConfig.
func TestExtractConfig_NonMapAndSkippedKeys(t *testing.T) {
	seq := &parser.SequenceNode{}
	cfg, root, diags := extractConfig(seq)
	if cfg != "" || root != seq || len(diags) != 0 {
		t.Errorf("non-map root = (%q, %T, %d diags)", cfg, root, len(diags))
	}
	// Nil-key and non-string-key entries are skipped; the openapi entry is kept.
	m := &parser.MapNode{Entries: []parser.MapEntry{
		{Key: nil, Value: &parser.ScalarNode{Value: "x"}},
		{Key: &parser.ScalarNode{Value: 42}, Value: &parser.ScalarNode{Value: "x"}},
		{Key: &parser.ScalarNode{Value: "openapi"}, Value: &parser.ScalarNode{Value: "3.0.0"}},
	}}
	cfg, root, diags = extractConfig(m)
	if cfg != "" || len(diags) != 0 {
		t.Errorf("no config key = (%q, %d diags)", cfg, len(diags))
	}
	// The nil-key and non-string-key entries are dropped; only the openapi key
	// survives.
	kept := root.(*parser.MapNode).Entries
	if len(kept) != 1 || kept[0].Key.Value != "openapi" {
		t.Errorf("expected only the openapi entry kept, got %+v", kept)
	}
}

// TestParseSpec_UnknownVersion drives the convertForVersion error branch of
// ParseSpec: a document that parses but declares no OpenAPI version.
func TestParseSpec_UnknownVersion(t *testing.T) {
	_, _, err := ParseSpec([]byte("a: 1\n"), "spec.yaml")
	if err == nil {
		t.Fatal("expected an error for a spec with no version key")
	}
	if !strings.Contains(err.Error(), "unsupported or unknown OpenAPI version") {
		t.Errorf("error %q does not mention the version", err)
	}
}

// TestBuildDetectedSummary_NilSpec drives the spec == nil guard of
// buildDetectedSummary.
func TestBuildDetectedSummary_NilSpec(t *testing.T) {
	ds := buildDetectedSummary(nil, nil, nil)
	if ds.Version != "" || ds.Paths != 0 {
		t.Errorf("nil spec = %+v", ds)
	}
}

// TestBuildDetectedSummary_PolymorphismStrategy drives the explicit
// cfg.Polymorphism.Strategy branch of buildDetectedSummary.
func TestBuildDetectedSummary_PolymorphismStrategy(t *testing.T) {
	spec := &parser.Spec{OpenAPI: "3.0.1", Info: &parser.Info{Title: "T", Version: "1"}, Paths: map[string]*parser.PathItem{}}
	cfg := &config.Config{Polymorphism: &config.PolymorphismConfig{Strategy: "custom"}}
	ds := buildDetectedSummary(spec, cfg, nil)
	if ds.PolymorphismStrategy != "custom" {
		t.Errorf("strategy = %q, want custom", ds.PolymorphismStrategy)
	}
}

// TestCountOperations_Webhooks drives the webhooks loop and the nil-path-item
// guard of countOperations.
func TestCountOperations_Webhooks(t *testing.T) {
	spec := &parser.Spec{
		Paths: map[string]*parser.PathItem{
			"/pets": {Get: &parser.Operation{OperationID: "listPets"}},
			"/nil":  nil,
		},
		Webhooks: map[string]*parser.PathItem{
			"hook": {Post: &parser.Operation{OperationID: "onEvent"}},
			"nil":  nil,
		},
	}
	if got := countOperations(spec); got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

// TestAnalyzeSchemas drives the nil-schema, seen-guard, Items-as-schema, and
// Not branches of analyzeSchema.
func TestAnalyzeSchemas(t *testing.T) {
	shared := &parser.Schema{Type: "string", ReadOnly: true}
	spec := &parser.Spec{Components: &parser.Components{Schemas: map[string]*parser.Schema{
		"nil": nil,
		"A": {
			Type: "object",
			Properties: map[string]*parser.Schema{
				"shared": shared,
				"items":  {Type: "array", Items: &parser.Schema{Type: "integer", WriteOnly: true}},
			},
			AllOf: []*parser.Schema{shared}, // shared → seen guard
			Not:   &parser.Schema{Type: "string", Nullable: true},
		},
	}}}
	stats := analyzeSchemas(spec)
	if stats.readOnly != 1 {
		t.Errorf("readOnly = %d, want 1 (shared counted once)", stats.readOnly)
	}
	if stats.writeOnly != 1 {
		t.Errorf("writeOnly = %d, want 1", stats.writeOnly)
	}
	if stats.nullable != 1 {
		t.Errorf("nullable = %d, want 1", stats.nullable)
	}
}

// TestApplyOperationFilters_NilCfg drives the cfg == nil guard of
// applyOperationFilters.
func TestApplyOperationFilters_NilCfg(t *testing.T) {
	if got := applyOperationFilters(&parser.Spec{}, nil); got != nil {
		t.Errorf("nil cfg = %+v, want nil", got)
	}
}

// TestApplyOperationFilters_Dropped drives the dropped > 0 branch: a
// skip_operations entry that removes an operation yields an Info diagnostic.
func TestApplyOperationFilters_Dropped(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/pets": {Get: &parser.Operation{OperationID: "listPets"}},
	}}
	cfg := &config.Config{SkipOperations: []string{"listPets"}}
	diags := applyOperationFilters(spec, cfg)
	if len(diags) != 1 || diags[0].Severity != diagnostics.Info {
		t.Fatalf("expected one Info diagnostic, got %+v", diags)
	}
	if !strings.Contains(diags[0].Detail, "1 operation") {
		t.Errorf("detail %q does not mention the dropped count", diags[0].Detail)
	}
}

// TestApplyConfigOverrides_MatchingEphemeral drives the matching-ephemeral
// branch of applyConfigOverrides (an override that matches an inferred
// ephemeral modifies it rather than appending a duplicate).
func TestApplyConfigOverrides_MatchingEphemeral(t *testing.T) {
	preview := &ir.ProviderIR{EphemeralResources: []ir.EphemeralResourceIR{
		{Name: "session", SourceOperation: "open-session"},
	}}
	cfg := &config.Config{EphemeralOverrides: []config.EphemeralOverride{
		{Operation: "open-session", RenewMapping: "POST /sessions/{id}/renew"},
	}}
	var diags diagnostics.Diagnostics
	applyConfigOverrides(preview, cfg, "acme", nil, nil, &diags)
	if len(preview.EphemeralResources) != 1 {
		t.Fatalf("expected 1 ephemeral, got %d", len(preview.EphemeralResources))
	}
	if !preview.EphemeralResources[0].HasRenew {
		t.Error("matching override must wire the renew mapping onto the existing ephemeral")
	}
}

// TestApplyConfigOverrides_MatchingList drives the matching-list-resource
// branch of applyConfigOverrides (a matching list override is a no-op).
func TestApplyConfigOverrides_MatchingList(t *testing.T) {
	preview := &ir.ProviderIR{ListResources: []ir.ListResourceIR{
		{Name: "widgets", SourceOperation: "listWidgets"},
	}}
	cfg := &config.Config{ListResourceOverrides: []config.ListResourceOverride{
		{Resource: "widgets", Operation: "listWidgets"},
	}}
	var diags diagnostics.Diagnostics
	applyConfigOverrides(preview, cfg, "acme", nil, nil, &diags)
	if len(preview.ListResources) != 1 {
		t.Fatalf("expected 1 list resource, got %d", len(preview.ListResources))
	}
}

// TestApplyGenerateDatasourceOverrides_NilPreview drives the preview == nil
// guard of applyGenerateDatasourceOverrides.
func TestApplyGenerateDatasourceOverrides_NilPreview(t *testing.T) {
	var diags diagnostics.Diagnostics
	applyGenerateDatasourceOverrides(nil, "acme", nil, nil, nil, &diags)
	if len(diags) != 0 {
		t.Errorf("nil preview must be a no-op, got %+v", diags)
	}
}

// TestApplyGenerateDatasourceOverrides_NoMatch drives the matched == nil branch:
// generate_datasource: true with no matching resource emits a warning.
func TestApplyGenerateDatasourceOverrides_NoMatch(t *testing.T) {
	preview := &ir.ProviderIR{}
	cfg := &config.Config{}
	overrides := []config.ResourceOverride{
		{Schema: "Missing", GenerateDatasource: boolPtr(true)},
	}
	var diags diagnostics.Diagnostics
	applyGenerateDatasourceOverrides(preview, "acme", nil, overrides, cfg, &diags)
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Errorf("expected one warning, got %+v", diags)
	}
}

// TestApplyGenerateDatasourceOverrides_PrefixAndDuplicate drives the
// datasource_prefix branch and the dataSourceAlreadyExists skip branch.
func TestApplyGenerateDatasourceOverrides_PrefixAndDuplicate(t *testing.T) {
	// A resource with a wired read whose data source already exists (same name)
	// is skipped; a prefix is applied to the emitted one.
	preview := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "widget", TypeName: "acme_widget", FullName: "acme_widget",
				CRUDMapping:     ir.CRUDMappingIR{Read: ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets/{id}"}},
				SourceOperation: "getWidget"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "widget", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets/{id}"}},
		},
	}
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets/{id}": {transformer.MethodGet: {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"}},
	}
	cfg := &config.Config{Naming: &config.NamingConfig{DatasourcePrefix: "ds_"}}
	overrides := []config.ResourceOverride{
		{Schema: "Widget", GenerateDatasource: boolPtr(true)},
	}
	var diags diagnostics.Diagnostics
	applyGenerateDatasourceOverrides(preview, "acme", pathOps, overrides, cfg, &diags)
	// The existing data source has the same name, so the new one is skipped.
	if len(preview.DataSources) != 1 {
		t.Fatalf("expected the duplicate data source to be skipped, got %d", len(preview.DataSources))
	}
}

// TestOverrideSeedLabel_NoSeed drives the "(no schema or operation)" fallback of
// overrideSeedLabel.
func TestOverrideSeedLabel_NoSeed(t *testing.T) {
	if got := overrideSeedLabel(config.ResourceOverride{}); got != "(no schema or operation)" {
		t.Errorf("label = %q", got)
	}
}

// TestDatasourceFromResource_NoRead drives the empty-read and unresolved-op
// branches of datasourceFromResource.
func TestDatasourceFromResource_NoRead(t *testing.T) {
	// No read mapping → nil.
	if got := datasourceFromResource("acme", ir.ResourceIR{}, config.ResourceOverride{}, nil, nil); got != nil {
		t.Errorf("no read = %+v, want nil", got)
	}
	// Read mapping present but the op is not in pathOps → nil.
	r := ir.ResourceIR{CRUDMapping: ir.CRUDMappingIR{Read: ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets/{id}"}}}
	if got := datasourceFromResource("acme", r, config.ResourceOverride{}, nil, nil); got != nil {
		t.Errorf("unresolved op = %+v, want nil", got)
	}
}

// TestApplyFunctionOverrideExtras_EmptyName drives the empty-argument-name skip
// of applyFunctionOverrideExtras.
func TestApplyFunctionOverrideExtras_EmptyName(t *testing.T) {
	f := &ir.FunctionIR{Arguments: []ir.FunctionParamIR{{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeString}}}}
	fo := config.FunctionOverride{Arguments: []config.FunctionArgument{{Name: "  ", Type: "string"}}}
	applyFunctionOverrideExtras(f, fo)
	if len(f.Arguments) != 1 {
		t.Errorf("empty name must be skipped, got %d arguments", len(f.Arguments))
	}
}

// TestFunctionArgumentIndex_EmptyTarget drives the want == "" guard of
// functionArgumentIndex.
func TestFunctionArgumentIndex_EmptyTarget(t *testing.T) {
	if got := functionArgumentIndex([]ir.FunctionParamIR{{Name: "a"}}, "  "); got != -1 {
		t.Errorf("empty target = %d, want -1", got)
	}
}

// TestEnvVarName drives the non-alphanumeric replacement branch of envVarName.
func TestEnvVarName(t *testing.T) {
	if got := envVarName("my-key.api"); got != "MY_KEY_API" {
		t.Errorf("envVarName = %q, want MY_KEY_API", got)
	}
}

// TestApplyWriteOnlyAttributesToProvider_Nil drives the provider == nil guard.
func TestApplyWriteOnlyAttributesToProvider_Nil(t *testing.T) {
	var diags diagnostics.Diagnostics
	applyWriteOnlyAttributesToProvider(nil, &diags)
	if len(diags) != 0 {
		t.Errorf("nil provider must be a no-op, got %+v", diags)
	}
}

// TestInferSensitiveAttributesToProvider_Nil drives the provider == nil guard.
func TestInferSensitiveAttributesToProvider_Nil(t *testing.T) {
	var diags diagnostics.Diagnostics
	inferSensitiveAttributesToProvider(nil, &diags)
	if len(diags) != 0 {
		t.Errorf("nil provider must be a no-op, got %+v", diags)
	}
}

// TestBuildSecurityIR_OAuthFlows drives the implicit, password,
// clientCredentials, and authorizationCode flow branches of buildSecurityIR.
func TestBuildSecurityIR_OAuthFlows(t *testing.T) {
	spec := &parser.Spec{
		Components: &parser.Components{SecuritySchemes: map[string]*parser.SecurityScheme{
			"oauth": {
				Type: "oauth2",
				Flows: &parser.OAuthFlows{
					Implicit:          &parser.OAuthFlow{AuthorizationURL: "https://a/authorize"},
					Password:          &parser.OAuthFlow{TokenURL: "https://a/password-token"},
					ClientCredentials: &parser.OAuthFlow{TokenURL: "https://a/cc-token"},
					AuthorizationCode: &parser.OAuthFlow{TokenURL: "https://a/token"},
				},
			},
		}},
	}
	var diags diagnostics.Diagnostics
	security := buildSecurityIR(spec, nil, &diags)
	if len(security.Schemes) != 1 {
		t.Fatalf("expected 1 scheme, got %d", len(security.Schemes))
	}
	flows := security.Schemes[0].Flows
	if flows == nil || flows.Implicit == nil || flows.Password == nil || flows.ClientCredentials == nil || flows.AuthorizationCode == nil {
		t.Errorf("flows = %+v", flows)
	}
}

// TestWarnPerOpORSecurity drives the nil-diags guard and the nil-pathItem skip.
func TestWarnPerOpORSecurity(t *testing.T) {
	// nil diags → no-op.
	warnPerOpORSecurity(&parser.Spec{Paths: map[string]*parser.PathItem{"/x": {}}}, nil)
	// A nil path item is skipped; a real OR-security operation warns.
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/nil": nil,
		"/pets": {Get: &parser.Operation{
			OperationID: "listPets",
			Security: []parser.SecurityRequirement{
				{Requirements: map[string][]string{"a": nil}},
				{Requirements: map[string][]string{"b": nil}},
			},
		}},
	}}
	var diags diagnostics.Diagnostics
	warnPerOpORSecurity(spec, &diags)
	if len(diags) != 1 {
		t.Fatalf("expected 1 OR-security warning, got %+v", diags)
	}
	if !strings.Contains(diags[0].Detail, "listPets") {
		t.Errorf("warning must name the operation, got %q", diags[0].Detail)
	}
}

// TestApplySecurityConfigAttributes_NilPreview drives the nil-preview guard.
func TestApplySecurityConfigAttributes_NilPreview(t *testing.T) {
	var diags diagnostics.Diagnostics
	applySecurityConfigAttributes(nil, &diags)
	if len(diags) != 0 {
		t.Errorf("nil preview must be a no-op, got %+v", diags)
	}
}

// TestApplySecurityConfigAttributes_WithSchemes drives the seen-map population
// and the attribute-merge loop of applySecurityConfigAttributes: an apiKey
// scheme maps to a provider-config attribute that is appended to the schema.
func TestApplySecurityConfigAttributes_WithSchemes(t *testing.T) {
	preview := &ir.ProviderIR{
		ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "existing"}}},
		SecurityIR: ir.SecurityIR{Schemes: []ir.SecuritySchemeIR{
			{Name: "api_key", Type: ir.SecuritySchemeAPIKey, In: "header", NameField: "X-API-Key"},
		}},
	}
	var diags diagnostics.Diagnostics
	applySecurityConfigAttributes(preview, &diags)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
	if len(preview.ConfigSchema.Attributes) != 2 {
		t.Fatalf("expected the api key attribute appended, got %+v", preview.ConfigSchema.Attributes)
	}
}

// TestApplyGenerateDatasourceOverrides_NoRead drives the ds == nil branch: a
// matched resource with no wired read operation emits a warning.
func TestApplyGenerateDatasourceOverrides_NoRead(t *testing.T) {
	preview := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "widget", TypeName: "acme_widget", FullName: "acme_widget", SourceOperation: "createWidget"},
		},
	}
	overrides := []config.ResourceOverride{
		{Schema: "widget", GenerateDatasource: boolPtr(true)},
	}
	var diags diagnostics.Diagnostics
	applyGenerateDatasourceOverrides(preview, "acme", nil, overrides, nil, &diags)
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Fatalf("expected one warning, got %+v", diags)
	}
	if !strings.Contains(diags[0].Detail, "no read operation") {
		t.Errorf("detail %q does not mention the missing read", diags[0].Detail)
	}
}

// TestBuildGroupedResources_RejectedGroup drives the groupIsResource rejection
// branch of buildGroupedResources: a complete CRUD group whose instance GET
// carries an explicit x-terraform-action extension is not emitted as a resource.
func TestBuildGroupedResources_RejectedGroup(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget", Extensions: map[string]any{"x-terraform-action": true}},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
		},
	}
	var diags diagnostics.Diagnostics
	resources, _ := buildGroupedResources(&parser.Spec{}, "acme", pathOps, nil, &diags, true)
	if len(resources) != 0 {
		t.Errorf("rejected group must not emit a resource, got %d", len(resources))
	}
}

// TestEmitPutAsCreateInfoDiagnostics_NotInSet drives the not-in-inferred-set
// skip of emitPutAsCreateInfoDiagnostics.
func TestEmitPutAsCreateInfoDiagnostics_NotInSet(t *testing.T) {
	preview := &ir.ProviderIR{Resources: []ir.ResourceIR{
		{Name: "a", CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/a"}}},
		{Name: "b", CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/b"}}},
	}}
	inferred := map[string]bool{"/b": true}
	var diags diagnostics.Diagnostics
	emitPutAsCreateInfoDiagnostics(preview, inferred, &diags)
	if len(diags) != 1 {
		t.Fatalf("expected 1 Info diagnostic (only /b), got %+v", diags)
	}
	if !strings.Contains(diags[0].Detail, "resource b") {
		t.Errorf("detail %q does not name resource b", diags[0].Detail)
	}
}

// TestApplyResourceCreationOverrides_Update drives the updatePath != "" branch:
// an override with an explicit update operation consumes the update op.
func TestApplyResourceCreationOverrides_Update(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"},
			transformer.MethodPut:    {Method: transformer.MethodPut, Path: "/widgets/{id}", OperationID: "putWidget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
		},
	}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Post: &parser.Operation{OperationID: "createWidget"}},
		"/widgets/{id}": {
			Get:    &parser.Operation{OperationID: "getWidget"},
			Put:    &parser.Operation{OperationID: "putWidget"},
			Delete: &parser.Operation{OperationID: "deleteWidget"},
		},
	}}
	preview := &ir.ProviderIR{}
	consumed := map[string]map[string]bool{}
	var diags diagnostics.Diagnostics
	applyResourceCreationOverrides(preview, spec, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), CreateOperation: "createWidget", ReadOperation: "getWidget", UpdateOperation: "putWidget", DeleteOperation: "deleteWidget"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Fatalf("expected 1 override-created resource, got %d", len(preview.Resources))
	}
	if !isConsumed(consumed, "/widgets/{id}", "PUT") {
		t.Error("the update operation must be consumed")
	}
}

// TestListResourceFromOperation_PropTypeFallback drives the propType fallback
// branch: an identity parameter that matches no item property yields a string
// identity attribute.
func TestListResourceFromOperation_PropTypeFallback(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodGet: {Method: transformer.MethodGet, Path: "/widgets", OperationID: "listWidgets",
			ResponseSchema: &transformer.SchemaSpec{
				Type: "array",
				Items: &transformer.SchemaSpec{
					Type:       "object",
					Properties: map[string]transformer.SchemaSpec{"id": {Type: "integer"}},
				},
			}}},
	}
	lr := listResourceFromOperation(&parser.Operation{OperationID: "listWidgets"}, "acme", "/widgets", "GET", pathOps, []string{"missing"}, nil)
	if len(lr.IdentitySchema.Attributes) != 1 {
		t.Fatalf("identity = %+v", lr.IdentitySchema.Attributes)
	}
	if lr.IdentitySchema.Attributes[0].Schema.Type != ir.TypeString {
		t.Errorf("unmatched identity type = %q, want string", lr.IdentitySchema.Attributes[0].Schema.Type)
	}
}

// TestSuggestPagination_NilSpec drives the nil-spec and nil-Paths guards of
// suggestPagination.
func TestSuggestPagination_NilSpec(t *testing.T) {
	if got := suggestPagination(nil); got != nil {
		t.Errorf("nil spec = %+v, want nil", got)
	}
	if got := suggestPagination(&parser.Spec{}); got != nil {
		t.Errorf("nil paths = %+v, want nil", got)
	}
}

// TestListResourceFromOverride_Pagination drives the lo.Pagination branch of
// listResourceFromOverride.
func TestListResourceFromOverride_Pagination(t *testing.T) {
	lr := listResourceFromOverride(config.ListResourceOverride{
		Resource:   "widgets",
		Operation:  "listWidgets",
		Pagination: &config.PaginationConfig{Style: "offset", PageParam: "page"},
	}, "acme")
	if lr.PaginationStyle != "offset" {
		t.Errorf("pagination style = %q, want offset", lr.PaginationStyle)
	}
}

// TestFunctionFromOverride_DerivedName drives the name-from-operation branch of
// functionFromOverride.
func TestFunctionFromOverride_DerivedName(t *testing.T) {
	fn := functionFromOverride(config.FunctionOverride{Operation: "compute-total"}, "acme")
	if fn.Name != "compute_total" {
		t.Errorf("derived name = %q, want compute_total", fn.Name)
	}
}

// TestListResourceNameFromOverride drives the name derivation of
// listResourceFromOverride: the explicit `resource` key wins, and an absent key
// falls back to the operation identifier in either form (operationId or
// "METHOD /path"), so a list_resource_override without a `resource` key never
// produces an empty construct name.
func TestListResourceNameFromOverride(t *testing.T) {
	cases := []struct {
		name string
		lo   config.ListResourceOverride
		want string
	}{
		{name: "explicit resource wins", lo: config.ListResourceOverride{Resource: "widgets", Operation: "listWidgets"}, want: "widgets"},
		{name: "operationId fallback", lo: config.ListResourceOverride{Operation: "loadAllIcapProfiles"}, want: "load_all_icap_profiles"},
		{name: "method path fallback", lo: config.ListResourceOverride{Operation: "GET /apps/icap/profiles"}, want: "get_apps_icap_profiles"},
		{name: "empty operation", lo: config.ListResourceOverride{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := listResourceNameFromOverride(tc.lo); got != tc.want {
				t.Errorf("listResourceNameFromOverride(%+v) = %q, want %q", tc.lo, got, tc.want)
			}
		})
	}
}

// TestProviderName_Fallback drives the "generated" fallback of providerName.
func TestProviderName_Fallback(t *testing.T) {
	if got := providerName(&parser.Spec{}, nil); got != "generated" {
		t.Errorf("fallback name = %q, want generated", got)
	}
}

// TestAddPathOperations_NilPathItem drives the pi == nil guard of
// addPathOperations.
func TestAddPathOperations_NilPathItem(t *testing.T) {
	preview := &ir.ProviderIR{}
	var diags diagnostics.Diagnostics
	addPathOperations(preview, &parser.Spec{}, "/nil", nil, "acme", nil, nil, nil, &diags)
	if len(preview.Resources) != 0 {
		t.Errorf("nil path item must add nothing, got %d resources", len(preview.Resources))
	}
}

// TestWarnUnwritableManagedResource_NilDiags drives the diags == nil guard.
func TestWarnUnwritableManagedResource_NilDiags(t *testing.T) {
	warnUnwritableManagedResource(nil, ir.ResourceIR{}, nil)
}

// TestBuildGroupedResources_PartialUpdate drives the PATCH-update branch of
// buildGroupedResources: a group with a partial update (no full PUT) uses PATCH
// for the Update mapping.
func TestBuildGroupedResources_PartialUpdate(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"},
			transformer.MethodPatch:  {Method: transformer.MethodPatch, Path: "/widgets/{id}", OperationID: "patchWidget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
		},
	}
	var diags diagnostics.Diagnostics
	resources, consumed := buildGroupedResources(&parser.Spec{}, "acme", pathOps, nil, &diags, true)
	if len(resources) != 1 {
		t.Fatalf("expected 1 grouped resource, got %d", len(resources))
	}
	if resources[0].CRUDMapping.Update == nil || resources[0].CRUDMapping.Update.Method != http.MethodPatch {
		t.Errorf("update mapping = %+v, want PATCH", resources[0].CRUDMapping.Update)
	}
	if !isConsumed(consumed, "/widgets/{id}", "PATCH") {
		t.Error("PATCH must be consumed")
	}
}

// TestBuildGroupedResources_TwoGroups drives the sort comparator of
// buildGroupedResources (needs ≥2 emitted resources).
func TestBuildGroupedResources_TwoGroups(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
		},
		"/gadgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/gadgets", OperationID: "createGadget"}},
		"/gadgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/gadgets/{id}", OperationID: "getGadget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/gadgets/{id}", OperationID: "deleteGadget"},
		},
	}
	var diags diagnostics.Diagnostics
	resources, _ := buildGroupedResources(&parser.Spec{}, "acme", pathOps, nil, &diags, true)
	if len(resources) != 2 {
		t.Fatalf("expected 2 grouped resources, got %d", len(resources))
	}
	if resources[0].Name > resources[1].Name {
		t.Errorf("resources not sorted: %q > %q", resources[0].Name, resources[1].Name)
	}
}

// TestEmitPutAsCreateInfoDiagnostics drives the non-PUT skip, the not-in-set
// skip, and the sort comparator of emitPutAsCreateInfoDiagnostics.
func TestEmitPutAsCreateInfoDiagnostics(t *testing.T) {
	preview := &ir.ProviderIR{Resources: []ir.ResourceIR{
		{Name: "a", CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/a"}}},
		{Name: "b", CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/b"}}},
		{Name: "c", CRUDMapping: ir.CRUDMappingIR{Create: ir.OperationMappingIR{Method: "PUT", PathTemplate: "/c"}}},
	}}
	inferred := map[string]bool{"/b": true, "/c": true}
	var diags diagnostics.Diagnostics
	emitPutAsCreateInfoDiagnostics(preview, inferred, &diags)
	if len(diags) != 2 {
		t.Fatalf("expected 2 Info diagnostics, got %+v", diags)
	}
	if diags[0].Severity != diagnostics.Info {
		t.Errorf("expected Info severity, got %+v", diags[0])
	}
}

// TestApplyResourceCreationOverrides drives the seed-fallback, no-seed skip,
// unresolved-seed skip, and consumed-seed skip branches.
func TestApplyResourceCreationOverrides(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
		},
	}
	preview := &ir.ProviderIR{}
	consumed := map[string]map[string]bool{}
	var diags diagnostics.Diagnostics

	// Seed falls back to CreateOperation.
	applyResourceCreationOverrides(preview, &parser.Spec{}, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), CreateOperation: "createWidget", ReadOperation: "getWidget", DeleteOperation: "deleteWidget"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Fatalf("expected 1 override-created resource, got %d", len(preview.Resources))
	}

	// No seed at all → skipped.
	applyResourceCreationOverrides(preview, &parser.Spec{}, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true)},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Errorf("no-seed override must be skipped, got %d resources", len(preview.Resources))
	}

	// Unresolved seed → skipped.
	applyResourceCreationOverrides(preview, &parser.Spec{}, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), Operation: "missing"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Errorf("unresolved-seed override must be skipped, got %d resources", len(preview.Resources))
	}

	// Seed already consumed → skipped.
	applyResourceCreationOverrides(preview, &parser.Spec{}, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), Operation: "createWidget"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Errorf("consumed-seed override must be skipped, got %d resources", len(preview.Resources))
	}
}

// TestResourceFromOverrideCRUD_Update drives the update branch of
// resourceFromOverrideCRUD.
func TestResourceFromOverrideCRUD_Update(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Post: &parser.Operation{OperationID: "createWidget"}},
		"/widgets/{id}": {
			Get:    &parser.Operation{OperationID: "getWidget"},
			Put:    &parser.Operation{OperationID: "putWidget"},
			Delete: &parser.Operation{OperationID: "deleteWidget"},
		},
	}}
	g := transformer.ResourceCRUD{
		Name:           "widget",
		CollectionPath: "/widgets",
		InstancePath:   "/widgets/{id}",
		Create:         &transformer.Operation{Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"},
		Read:           &transformer.Operation{Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget"},
		Update:         &transformer.Operation{Method: transformer.MethodPut, Path: "/widgets/{id}", OperationID: "putWidget"},
		Delete:         &transformer.Operation{Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
	}
	var diags diagnostics.Diagnostics
	res := resourceFromOverrideCRUD(spec, "acme", g, &diags, false, nil, "")
	if res == nil {
		t.Fatal("resourceFromOverrideCRUD returned nil")
	}
	if res.CRUDMapping.Update == nil || res.CRUDMapping.Update.Method != http.MethodPut {
		t.Errorf("update mapping = %+v, want PUT", res.CRUDMapping.Update)
	}
	if !res.OverrideCreated {
		t.Error("OverrideCreated must be true")
	}
}

// TestDetectResponseInnerPath_NonObject drives the non-object / empty-properties
// branches of detectResponseInnerPath.
func TestDetectResponseInnerPath_NonObject(t *testing.T) {
	write := &transformer.Operation{ResponseSchema: &transformer.SchemaSpec{
		Type:       "object",
		Properties: map[string]transformer.SchemaSpec{},
		RefName:    "CreateResp",
	}}
	read := &transformer.Operation{ResponseSchema: &transformer.SchemaSpec{RefName: "Widget"}}
	if got := detectResponseInnerPath(write, read); got != "" {
		t.Errorf("empty-properties response = %q, want empty", got)
	}
	// Non-object write response.
	write2 := &transformer.Operation{ResponseSchema: &transformer.SchemaSpec{Type: "array", RefName: "CreateResp"}}
	if got := detectResponseInnerPath(write2, read); got != "" {
		t.Errorf("non-object response = %q, want empty", got)
	}
}

// TestGroupedImportFormat_CompositeNoParams drives the empty-parameter-names
// branch of groupedImportFormat.
func TestGroupedImportFormat_CompositeNoParams(t *testing.T) {
	g := transformer.ResourceCRUD{ID: transformer.IDInfo{Kind: transformer.IDComposite}}
	if _, ok := groupedImportFormat(g, ir.ObjectSchemaIR{}, ""); ok {
		t.Error("composite ID with no parameter names must not be importable")
	}
}

// TestGroupIsResource_Rejected drives the action/ephemeral/function/list
// rejection branch of groupIsResource.
func TestGroupIsResource_Rejected(t *testing.T) {
	// A group whose instance GET is a provider function (path keyword "query")
	// is rejected.
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodPost: {Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"}},
		"/widgets/{id}/query": {
			transformer.MethodGet:    {Method: transformer.MethodGet, Path: "/widgets/{id}/query", OperationID: "queryWidget"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/widgets/{id}/query", OperationID: "deleteWidget"},
		},
	}
	g := transformer.ResourceCRUD{
		CollectionPath: "/widgets",
		InstancePath:   "/widgets/{id}/query",
		Create:         &transformer.Operation{OperationID: "createWidget"},
		Read:           &transformer.Operation{OperationID: "queryWidget"},
		Delete:         &transformer.Operation{OperationID: "deleteWidget"},
	}
	if groupIsResource(g, pathOps) {
		t.Error("group with a function-classified read must be rejected")
	}
}

// TestGroupDeprecationMessage drives the read and delete deprecation branches.
func TestGroupDeprecationMessage(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Post: &parser.Operation{OperationID: "createWidget"}},
		"/widgets/{id}": {
			Get:    &parser.Operation{OperationID: "getWidget", Deprecated: true},
			Delete: &parser.Operation{OperationID: "deleteWidget", Deprecated: true},
		},
	}}
	g := transformer.ResourceCRUD{
		Create: &transformer.Operation{Method: transformer.MethodPost, Path: "/widgets"},
		Read:   &transformer.Operation{Method: transformer.MethodGet, Path: "/widgets/{id}"},
		Delete: &transformer.Operation{Method: transformer.MethodDelete, Path: "/widgets/{id}"},
	}
	if got := groupDeprecationMessage(spec, g); got != "This resource is deprecated." {
		t.Errorf("read-deprecated message = %q", got)
	}
	// Create not deprecated, read not deprecated, delete deprecated.
	spec2 := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Post: &parser.Operation{OperationID: "createWidget"}},
		"/widgets/{id}": {
			Get:    &parser.Operation{OperationID: "getWidget"},
			Delete: &parser.Operation{OperationID: "deleteWidget", Deprecated: true},
		},
	}}
	if got := groupDeprecationMessage(spec2, g); got != "This resource is deprecated." {
		t.Errorf("delete-deprecated message = %q", got)
	}
}

// TestParserOp drives the PATCH and unknown-method branches of parserOp.
func TestParserOp(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets/{id}": {Patch: &parser.Operation{OperationID: "patchWidget"}},
	}}
	if op := parserOp(spec, "/widgets/{id}", "PATCH"); op == nil || op.OperationID != "patchWidget" {
		t.Errorf("PATCH lookup = %+v", op)
	}
	if op := parserOp(spec, "/widgets/{id}", "TRACE"); op != nil {
		t.Errorf("unknown method = %+v, want nil", op)
	}
}

// TestResourceFromOperation_MappingBranches drives the PUT/PATCH and DELETE
// mapping branches of resourceFromOperation.
func TestResourceFromOperation_MappingBranches(t *testing.T) {
	op := &parser.Operation{OperationID: "updateWidget"}
	res := resourceFromOperation(op, "acme", "/widgets/{id}", "PUT", nil)
	if res.CRUDMapping.Update == nil || res.CRUDMapping.Update.Method != http.MethodPut {
		t.Errorf("PUT mapping = %+v", res.CRUDMapping.Update)
	}
	res2 := resourceFromOperation(op, "acme", "/widgets/{id}", http.MethodDelete, nil)
	if res2.CRUDMapping.Delete.Method != http.MethodDelete {
		t.Errorf("DELETE mapping = %+v", res2.CRUDMapping.Delete)
	}
}

// TestSecurityRequirementsFromOp drives the nil-Requirements branch (an empty
// requirement object {} preserved as an empty map).
func TestSecurityRequirementsFromOp(t *testing.T) {
	op := &parser.Operation{Security: []parser.SecurityRequirement{
		{Requirements: nil},
		{Requirements: map[string][]string{"api_key": nil}},
	}}
	got := securityRequirementsFromOp(op)
	if len(got) != 2 {
		t.Fatalf("expected 2 requirements, got %+v", got)
	}
	if got[0] == nil || len(got[0]) != 0 {
		t.Errorf("empty requirement = %+v, want empty map", got[0])
	}
	if _, ok := got[1]["api_key"]; !ok {
		t.Errorf("api_key requirement missing from %+v", got[1])
	}
}

// TestMergePathParams_AllDuplicates drives the all-duplicates branch of
// mergePathParams (returns the original operation).
func TestMergePathParams_AllDuplicates(t *testing.T) {
	op := &parser.Operation{OperationID: "getWidget", Parameters: []parser.Parameter{{Name: "id", In: "path"}}}
	pathParams := []parser.Parameter{{Name: "id", In: "path"}}
	if got := mergePathParams(op, pathParams); got != op {
		t.Error("all-duplicate path params must return the original operation")
	}
}

// TestParamsFromOperation_Nil drives the nil-op guard of paramsFromOperation.
func TestParamsFromOperation_Nil(t *testing.T) {
	q, h, c, f := paramsFromOperation(nil)
	if q != nil || h != nil || c != nil || f != nil {
		t.Errorf("nil op = (%v, %v, %v, %v), want all nil", q, h, c, f)
	}
}

// TestSuccessCodesFromResponses drives the non-2xx skip and the no-2xx fallback
// branches of successCodesFromResponses.
func TestSuccessCodesFromResponses(t *testing.T) {
	op := &parser.Operation{Responses: map[string]*parser.Response{
		"200": {Description: "ok"},
		"404": {Description: "not found"},
	}}
	if got := successCodesFromResponses(op, "GET"); len(got) != 1 || got[0] != 200 {
		t.Errorf("codes = %v, want [200]", got)
	}
	op2 := &parser.Operation{Responses: map[string]*parser.Response{"404": {Description: "not found"}}}
	if got := successCodesFromResponses(op2, "POST"); len(got) != 2 || got[0] != 201 || got[1] != 200 {
		t.Errorf("no-2xx fallback = %v, want [201 200]", got)
	}
}

// TestExtensionKind_Nil drives the nil-op guard of extensionKind.
func TestExtensionKind_Nil(t *testing.T) {
	if k, ok := extensionKind(nil); ok {
		t.Errorf("nil op = %q, want no kind", k)
	}
}

// TestExtensionBool_String drives the string branch of extensionBool.
func TestExtensionBool_String(t *testing.T) {
	if !extensionBool("true") {
		t.Error("string true must be truthy")
	}
	if !extensionBool("TRUE") {
		t.Error("string TRUE must be truthy (case-insensitive)")
	}
	if extensionBool("false") {
		t.Error("string false must be falsy")
	}
}

// TestMethodKind drives the OPTIONS/HEAD, verb-fallback, and unknown branches.
func TestMethodKind(t *testing.T) {
	if k := methodKind("OPTIONS", "/pets", nil, nil, false); k != kindUnknown {
		t.Errorf("OPTIONS = %q, want unknown", k)
	}
	if k := methodKind("TRACE", "/pets/reboot", nil, nil, false); k != kindAction {
		t.Errorf("verb segment = %q, want action", k)
	}
	if k := methodKind("TRACE", "/pets", nil, nil, false); k != kindUnknown {
		t.Errorf("non-verb unknown method = %q, want unknown", k)
	}
}

// TestLastPathSegment_NoSlash drives the no-slash branch of lastPathSegment.
func TestLastPathSegment_NoSlash(t *testing.T) {
	if got := lastPathSegment("pets"); got != "pets" {
		t.Errorf("no-slash segment = %q, want pets", got)
	}
}

// TestOperationDescription drives the nil-op and description-with-whitespace
// branches.
func TestOperationDescription(t *testing.T) {
	if got := operationDescription(nil, "fallback"); got != "fallback" {
		t.Errorf("nil op = %q, want fallback", got)
	}
	op := &parser.Operation{Description: "A real description.", Summary: "short"}
	if got := operationDescription(op, "fb"); got != "A real description." {
		t.Errorf("description = %q", got)
	}
	// A leaked PascalCase title (no whitespace) is skipped in favor of the summary.
	op2 := &parser.Operation{Description: "GigaAlarmBulkAcknowledgementSpec", Summary: "Acknowledge"}
	if got := operationDescription(op2, "fb"); got != "Acknowledge" {
		t.Errorf("leaked title = %q, want summary", got)
	}
}

// TestHumanizeConstructName_Empty drives the empty-name branch.
func TestHumanizeConstructName_Empty(t *testing.T) {
	if got := humanizeConstructName("  "); got != "resource" {
		t.Errorf("empty name = %q, want resource", got)
	}
}

// TestActionConfigAttributes_Nil drives the nil-op guard.
func TestActionConfigAttributes_Nil(t *testing.T) {
	if got := actionConfigAttributes(nil); got != nil {
		t.Errorf("nil op = %+v, want nil", got)
	}
}

// TestEphemeralFromOperation_RevokeFallback drives the revoke fallback branch:
// an ephemeral with no close op but a revoke op wires Close from revoke.
func TestEphemeralFromOperation_RevokeFallback(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/sessions":        {Post: &parser.Operation{OperationID: "openSession"}},
		"/sessions/revoke": {Delete: &parser.Operation{OperationID: "revokeSession"}},
	}}
	op := &parser.Operation{OperationID: "openSession"}
	er := ephemeralFromOperation(spec, op, "acme", "/sessions", "POST", nil, nil)
	if !er.HasClose || er.CloseMapping == nil || er.CloseMapping.PathTemplate != "/sessions/revoke" {
		t.Errorf("revoke fallback not wired: %+v", er.CloseMapping)
	}
}

// TestListResourceFromOperation drives the propType branches and the id
// fallback of listResourceFromOperation.
func TestListResourceFromOperation(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodGet: {Method: transformer.MethodGet, Path: "/widgets", OperationID: "listWidgets",
			ResponseSchema: &transformer.SchemaSpec{
				Type: "array",
				Items: &transformer.SchemaSpec{
					Type: "object",
					Properties: map[string]transformer.SchemaSpec{
						"id":    {Type: "integer"},
						"count": {Type: "number"},
						"ok":    {Type: "boolean"},
						"name":  {Type: "string"},
					},
				},
			}}},
	}
	// No identity params → id fallback.
	lr := listResourceFromOperation(&parser.Operation{OperationID: "listWidgets"}, "acme", "/widgets", "GET", pathOps, nil, nil)
	if len(lr.IdentitySchema.Attributes) != 1 || lr.IdentitySchema.Attributes[0].Name != "id" {
		t.Fatalf("id fallback = %+v", lr.IdentitySchema.Attributes)
	}
	if lr.IdentitySchema.Attributes[0].Schema.Type != ir.TypeInt {
		t.Errorf("id type = %q, want int", lr.IdentitySchema.Attributes[0].Schema.Type)
	}
	// Identity params drive the propType branches.
	lr2 := listResourceFromOperation(&parser.Operation{OperationID: "listWidgets"}, "acme", "/widgets", "GET", pathOps, []string{"count", "ok", "name", "id"}, nil)
	if len(lr2.IdentitySchema.Attributes) != 4 {
		t.Fatalf("identity params = %+v", lr2.IdentitySchema.Attributes)
	}
	types := map[string]ir.PrimitiveType{}
	for _, a := range lr2.IdentitySchema.Attributes {
		types[a.Name] = a.Schema.Type
	}
	if types["count"] != ir.TypeFloat || types["ok"] != ir.TypeBool || types["name"] != ir.TypeString || types["id"] != ir.TypeInt {
		t.Errorf("prop types = %+v", types)
	}
}

// TestListResourceFromOperation_IdentityDescriptions covers the identity
// attribute descriptions: the matching item property's description documents
// what each identity attribute identifies, so it must be carried onto the
// identity attribute for both the identity-params path and the id fallback.
// Undescribed item properties leave the identity attribute undescribed.
func TestListResourceFromOperation_IdentityDescriptions(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/widgets": {transformer.MethodGet: {Method: transformer.MethodGet, Path: "/widgets", OperationID: "listWidgets",
			ResponseSchema: &transformer.SchemaSpec{
				Type: "array",
				Items: &transformer.SchemaSpec{
					Type: "object",
					Properties: map[string]transformer.SchemaSpec{
						"id":   {Type: "integer", Description: "Widget identifier."},
						"name": {Type: "string", Description: "Widget name."},
						"ok":   {Type: "boolean"},
					},
				},
			}}},
	}
	// Identity params take the description of the matched item property.
	lr := listResourceFromOperation(&parser.Operation{OperationID: "listWidgets"}, "acme", "/widgets", "GET", pathOps, []string{"name", "ok"}, nil)
	descs := map[string]string{}
	for _, a := range lr.IdentitySchema.Attributes {
		descs[a.Name] = a.Description
	}
	if descs["name"] != "Widget name." {
		t.Errorf("name description = %q, want %q", descs["name"], "Widget name.")
	}
	if descs["ok"] != "" {
		t.Errorf("undescribed ok = %q, want empty", descs["ok"])
	}
	// The id fallback branch also carries the item property's description.
	lr2 := listResourceFromOperation(&parser.Operation{OperationID: "listWidgets"}, "acme", "/widgets", "GET", pathOps, nil, nil)
	if len(lr2.IdentitySchema.Attributes) != 1 || lr2.IdentitySchema.Attributes[0].Name != "id" {
		t.Fatalf("id fallback = %+v", lr2.IdentitySchema.Attributes)
	}
	if got := lr2.IdentitySchema.Attributes[0].Description; got != "Widget identifier." {
		t.Errorf("id description = %q, want %q", got, "Widget identifier.")
	}
}

// TestFunctionReturnSchema drives the integer/boolean primitives, array-of-
// primitives, non-flat object, and default branches.
func TestFunctionReturnSchema(t *testing.T) {
	if got := functionReturnSchema(&transformer.SchemaSpec{Type: "integer"}); got.Type != ir.TypeInt {
		t.Errorf("integer = %+v", got)
	}
	if got := functionReturnSchema(&transformer.SchemaSpec{Type: "boolean"}); got.Type != ir.TypeBool {
		t.Errorf("boolean = %+v", got)
	}
	if got := functionReturnSchema(&transformer.SchemaSpec{Type: "array", Items: &transformer.SchemaSpec{Type: "string"}}); got.Collection == nil || got.Collection.ElementType.Type != ir.TypeString {
		t.Errorf("array of strings = %+v", got)
	}
	// Non-flat object (a nested object property) → Dynamic.
	nonFlat := &transformer.SchemaSpec{Type: "object", Properties: map[string]transformer.SchemaSpec{
		"nested": {Type: "object"},
	}}
	if got := functionReturnSchema(nonFlat); got.Type != ir.TypeDynamic {
		t.Errorf("non-flat object = %+v, want Dynamic", got)
	}
	// Unknown type → Dynamic.
	if got := functionReturnSchema(&transformer.SchemaSpec{Type: "weird"}); got.Type != ir.TypeDynamic {
		t.Errorf("unknown type = %+v, want Dynamic", got)
	}
}

// TestBuildProviderIR_LoadError drives the load-error branch of buildProviderIR.
func TestBuildProviderIR_LoadError(t *testing.T) {
	_, _, _, err := buildProviderIR([]byte(""), "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to load spec") {
		t.Errorf("empty body error = %v", err)
	}
}

// TestBuildProviderIR_DetectVersionError drives the version-detection error
// branch of buildProviderIR: a document that parses but declares no OpenAPI
// version.
func TestBuildProviderIR_DetectVersionError(t *testing.T) {
	_, _, _, err := buildProviderIR([]byte("a: 1\n"), "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to detect OpenAPI version") {
		t.Errorf("no-version error = %v", err)
	}
}

// TestSuggestAuth_NilScheme drives the nil-scheme skip of suggestAuth.
func TestSuggestAuth_NilScheme(t *testing.T) {
	spec := &parser.Spec{Components: &parser.Components{SecuritySchemes: map[string]*parser.SecurityScheme{
		"nil": nil,
	}}}
	if got := suggestAuth(spec); len(got) != 0 {
		t.Errorf("nil scheme = %+v, want none", got)
	}
}

// TestAuthConfigFromScheme drives the openIdConnect and unknown-type branches.
func TestAuthConfigFromScheme(t *testing.T) {
	if _, ok := authConfigFromScheme("oidc", &parser.SecurityScheme{Type: "openIdConnect"}); ok {
		t.Error("openIdConnect must not be representable")
	}
	if _, ok := authConfigFromScheme("weird", &parser.SecurityScheme{Type: "unsupported"}); ok {
		t.Error("unknown scheme type must not be representable")
	}
}

// TestSuggestPagination drives the offset-param, cursor-param, cursor-only, and
// offset-without-perPage branches.
func TestSuggestPagination(t *testing.T) {
	// offset param + limit param → offset style with both.
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Get: &parser.Operation{Parameters: []parser.Parameter{
			{Name: "offset", In: "query"},
			{Name: "limit", In: "query"},
		}}},
	}}
	pg := suggestPagination(spec)
	if pg == nil || pg.Style != "offset" || pg.PageParam != "offset" || pg.PerPageParam != "limit" {
		t.Errorf("offset pagination = %+v", pg)
	}
	// cursor param only → cursor style.
	spec2 := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Get: &parser.Operation{Parameters: []parser.Parameter{
			{Name: "cursor", In: "query"},
		}}},
	}}
	pg2 := suggestPagination(spec2)
	if pg2 == nil || pg2.Style != "cursor" || pg2.CursorField != "cursor" {
		t.Errorf("cursor pagination = %+v", pg2)
	}
	// offset param without perPage → offset style with page only.
	spec3 := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets": {Get: &parser.Operation{Parameters: []parser.Parameter{
			{Name: "page", In: "query"},
		}}},
	}}
	pg3 := suggestPagination(spec3)
	if pg3 == nil || pg3.Style != "offset" || pg3.PageParam != "page" || pg3.PerPageParam != "" {
		t.Errorf("offset-only pagination = %+v", pg3)
	}
	// No pagination params → nil.
	if got := suggestPagination(&parser.Spec{Paths: map[string]*parser.PathItem{"/widgets": {Get: &parser.Operation{}}}}); got != nil {
		t.Errorf("no pagination = %+v, want nil", got)
	}
}

// TestResourceFromOverrideCRUD_CollectionReadFlag drives the placeholder-free
// array-read detection (G39): an override-created resource whose read fetches
// the collection (no path placeholder) and whose response is an array of
// instances (the GigaVUE-FM diameter_whitelist shape: read_operation
// loadDiameterWhitelists, delete_operation on /{alias}) gets
// ResponseIsCollection on its Read mapping, so the generated readRemote
// selects the matching element by identifier. An instance read (placeholder
// path) whose response is a get-one array wrapper keeps the flag clear.
// TestResourceFromOverrideCRUD_ReadCollectionPath drives the
// read_collection_path branch of resourceFromOverrideCRUD: a valid path sets
// the nested-collection read mapping and switches the schema to the
// create-body state shape; an invalid path is dropped fail-loud (Warning) and
// the mapping keeps a plain read.
func TestResourceFromOverrideCRUD_ReadCollectionPath(t *testing.T) {
	childGroup := func() transformer.ResourceCRUD {
		ruleSpec := &transformer.SchemaSpec{Type: "object", Properties: map[string]transformer.SchemaSpec{
			"ruleId": {Type: "string"},
			"match":  {Type: "string"},
		}}
		return transformer.ResourceCRUD{
			Name:           "port_filter_rule",
			CollectionPath: "/ports/{portId}/filters/rules",
			InstancePath:   "/ports/{portId}/filters/rules/{ruleId}",
			Create: &transformer.Operation{Method: transformer.MethodPost, Path: "/ports/{portId}/filters/rules", OperationID: "createRule",
				RequestSchema: ruleSpec},
			Read: &transformer.Operation{Method: transformer.MethodGet, Path: "/ports/{portId}/filters/rules", OperationID: "listRules",
				ResponseSchema: &transformer.SchemaSpec{Type: "object", Properties: map[string]transformer.SchemaSpec{
					"rules": {Type: "array", Items: ruleSpec},
				}}},
			Update: &transformer.Operation{Method: transformer.MethodPut, Path: "/ports/{portId}/filters/rules/{ruleId}", OperationID: "updateRule"},
			Delete: &transformer.Operation{Method: transformer.MethodDelete, Path: "/ports/{portId}/filters/rules/{ruleId}", OperationID: "deleteRule"},
		}
	}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/ports/{portId}/filters/rules": {
			Post: &parser.Operation{OperationID: "createRule"},
			Get:  &parser.Operation{OperationID: "listRules"},
		},
		"/ports/{portId}/filters/rules/{ruleId}": {
			Put:    &parser.Operation{OperationID: "updateRule"},
			Delete: &parser.Operation{OperationID: "deleteRule"},
		},
	}}

	t.Run("valid nested path wires the child read", func(t *testing.T) {
		var diags diagnostics.Diagnostics
		res := resourceFromOverrideCRUD(spec, "acme", childGroup(), &diags, false, nil, "rules.*")
		if res == nil {
			t.Fatal("resourceFromOverrideCRUD returned nil")
		}
		if got := res.CRUDMapping.Read.NestedCollectionPath; got != "rules.*" {
			t.Errorf("NestedCollectionPath = %q, want rules.*", got)
		}
		if !res.CRUDMapping.Read.ResponseIsCollection {
			t.Error("ResponseIsCollection must be true for a nested collection read")
		}
		// The state shape comes from the create request body, not the parent
		// read response: "match" is present, "rules" is not.
		names := map[string]bool{}
		for _, a := range res.Schema.Attributes {
			names[a.Name] = true
		}
		if !names["match"] {
			t.Errorf("create-body attribute match expected, got %v", names)
		}
		if names["rules"] {
			t.Errorf("parent-read attribute rules must be absent, got %v", names)
		}
	})

	t.Run("invalid nested path is dropped with a warning", func(t *testing.T) {
		var diags diagnostics.Diagnostics
		res := resourceFromOverrideCRUD(spec, "acme", childGroup(), &diags, false, nil, "rules.*.x")
		if res == nil {
			t.Fatal("resourceFromOverrideCRUD returned nil")
		}
		if got := res.CRUDMapping.Read.NestedCollectionPath; got != "" {
			t.Errorf("NestedCollectionPath = %q, want dropped", got)
		}
		if res.CRUDMapping.Read.ResponseIsCollection {
			t.Error("ResponseIsCollection must stay clear when the path is dropped")
		}
		found := false
		for _, d := range diags {
			if d.Severity == diagnostics.Warning && strings.Contains(d.Detail, "read_collection_path") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a read_collection_path warning, got %+v", diags)
		}
	})
}

func TestResourceFromOverrideCRUD_CollectionReadFlag(t *testing.T) {
	arrayResponse := &transformer.SchemaSpec{Type: "array", Items: &transformer.SchemaSpec{
		Type: "object",
		Properties: map[string]transformer.SchemaSpec{
			"alias": {Type: "string"},
		},
	}}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/whitelists": {
			Post: &parser.Operation{OperationID: "createWhitelist"},
			Get:  &parser.Operation{OperationID: "loadWhitelists"},
		},
		"/whitelists/{alias}": {Delete: &parser.Operation{OperationID: "deleteWhitelist"}},
	}}
	g := transformer.ResourceCRUD{
		Name:           "whitelist",
		CollectionPath: "/whitelists",
		InstancePath:   "/whitelists/{alias}",
		Create: &transformer.Operation{Method: transformer.MethodPost, Path: "/whitelists", OperationID: "createWhitelist",
			RequestSchema: &transformer.SchemaSpec{Properties: map[string]transformer.SchemaSpec{"alias": {Type: "string"}}}},
		Read:   &transformer.Operation{Method: transformer.MethodGet, Path: "/whitelists", OperationID: "loadWhitelists", ResponseSchema: arrayResponse},
		Delete: &transformer.Operation{Method: transformer.MethodDelete, Path: "/whitelists/{alias}", OperationID: "deleteWhitelist"},
	}
	var diags diagnostics.Diagnostics
	res := resourceFromOverrideCRUD(spec, "acme", g, &diags, false, nil, "")
	if res == nil {
		t.Fatal("resourceFromOverrideCRUD returned nil")
	}
	if !res.CRUDMapping.Read.ResponseIsCollection {
		t.Errorf("placeholder-free array read must set ResponseIsCollection, got %+v", res.CRUDMapping.Read)
	}

	// An instance read (placeholder path) with the same array response is a
	// get-one wrapper: the flag stays clear.
	instanceSpec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/widgets":      {Post: &parser.Operation{OperationID: "createWidget"}},
		"/widgets/{id}": {Get: &parser.Operation{OperationID: "getWidget"}, Delete: &parser.Operation{OperationID: "deleteWidget"}},
	}}
	g2 := transformer.ResourceCRUD{
		Name:           "widget",
		CollectionPath: "/widgets",
		InstancePath:   "/widgets/{id}",
		Create:         &transformer.Operation{Method: transformer.MethodPost, Path: "/widgets", OperationID: "createWidget"},
		Read:           &transformer.Operation{Method: transformer.MethodGet, Path: "/widgets/{id}", OperationID: "getWidget", ResponseSchema: arrayResponse},
		Delete:         &transformer.Operation{Method: transformer.MethodDelete, Path: "/widgets/{id}", OperationID: "deleteWidget"},
	}
	res2 := resourceFromOverrideCRUD(instanceSpec, "acme", g2, &diags, false, nil, "")
	if res2 == nil {
		t.Fatal("resourceFromOverrideCRUD returned nil for instance read")
	}
	if res2.CRUDMapping.Read.ResponseIsCollection {
		t.Errorf("instance read with a get-one array wrapper must keep ResponseIsCollection clear, got %+v", res2.CRUDMapping.Read)
	}
}
