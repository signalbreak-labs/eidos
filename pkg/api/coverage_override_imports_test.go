package api

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// TestResourceOverrideConfiguresID covers the identifier-configuration gate: a
// matching override with id_attribute or import_format disables the
// user-settable-identifier preference; a matching override with neither, or a
// non-matching override, does not.
func TestResourceOverrideConfiguresID(t *testing.T) {
	r := ir.ResourceIR{Name: "archive_server", TypeName: "acme_archive_server", FullName: "acme_archive_server"}

	t.Run("id_attribute configures the ID", func(t *testing.T) {
		overrides := []config.ResourceOverride{{Schema: "archive_server", IDAttribute: "server_alias"}}
		if !resourceOverrideConfiguresID(overrides, r) {
			t.Error("override with id_attribute must configure the ID")
		}
	})

	t.Run("import_format configures the ID", func(t *testing.T) {
		overrides := []config.ResourceOverride{{Schema: "archive_server", ImportFormat: "{server_alias}"}}
		if !resourceOverrideConfiguresID(overrides, r) {
			t.Error("override with import_format must configure the ID")
		}
	})

	t.Run("matching override with neither field does not configure", func(t *testing.T) {
		overrides := []config.ResourceOverride{{Schema: "archive_server"}}
		if resourceOverrideConfiguresID(overrides, r) {
			t.Error("override with neither id_attribute nor import_format must not configure the ID")
		}
	})

	t.Run("non-matching override does not configure", func(t *testing.T) {
		overrides := []config.ResourceOverride{{Schema: "other", IDAttribute: "x"}}
		if resourceOverrideConfiguresID(overrides, r) {
			t.Error("non-matching override must not configure the ID")
		}
	})
}

// TestApplyResourceCreationOverride_IDFromUpdatePath drives the identifier
// fallback in applyResourceCreationOverride: when the read is a collection GET
// with no path parameters, the ID is taken from the update/delete path so the
// override-created resource wires an import against the real schema attribute.
func TestApplyResourceCreationOverride_IDFromUpdatePath(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/policies": {
			transformer.MethodPost: {Method: transformer.MethodPost, Path: "/policies", OperationID: "createPolicy",
				// The create body carries "name", so the identifier is
				// user-settable (Required + RequestInput); a Computed-only
				// import target is refused by groupedImportFormat (G39).
				RequestSchema: &transformer.SchemaSpec{
					Type:     "object",
					Required: []string{"name"},
					Properties: map[string]transformer.SchemaSpec{
						"name": {Type: "string"},
					},
				}},
			transformer.MethodGet: {Method: transformer.MethodGet, Path: "/policies", OperationID: "listPolicies",
				ResponseSchema: &transformer.SchemaSpec{
					Type:     "object",
					Required: []string{"name"},
					Properties: map[string]transformer.SchemaSpec{
						"name": {Type: "string"},
					},
				}},
		},
		"/policies/{name}": {
			transformer.MethodPut:    {Method: transformer.MethodPut, Path: "/policies/{name}", OperationID: "updatePolicy"},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/policies/{name}", OperationID: "deletePolicy"},
		},
	}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/policies": {
			Post: &parser.Operation{OperationID: "createPolicy"},
			Get:  &parser.Operation{OperationID: "listPolicies"},
		},
		"/policies/{name}": {
			Put:    &parser.Operation{OperationID: "updatePolicy"},
			Delete: &parser.Operation{OperationID: "deletePolicy"},
		},
	}}
	preview := &ir.ProviderIR{}
	consumed := map[string]map[string]bool{}
	var diags diagnostics.Diagnostics
	applyResourceCreationOverrides(preview, spec, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), CreateOperation: "createPolicy", ReadOperation: "listPolicies", UpdateOperation: "updatePolicy", DeleteOperation: "deletePolicy"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Fatalf("expected 1 override-created resource, got %d", len(preview.Resources))
	}
	res := preview.Resources[0]
	if !res.Importable {
		t.Errorf("override-created resource with a {name} path must be importable: %+v", res)
	}
	if res.ImportIDFormat != "{name}" {
		t.Errorf("ImportIDFormat = %q, want {name}", res.ImportIDFormat)
	}
	if !isConsumed(consumed, "/policies/{name}", "PUT") {
		t.Error("the update operation must be consumed")
	}
	if !isConsumed(consumed, "/policies/{name}", "DELETE") {
		t.Error("the delete operation must be consumed")
	}
}

// TestApplyResourceCreationOverride_SkipUserSettableID drives the
// skipUserSettableID gate: an override that explicitly configures the
// identifier keeps the synthetic Computed placeholder named for the path
// parameter instead of preferring a practitioner-supplied create-body attribute.
func TestApplyResourceCreationOverride_SkipUserSettableID(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/ports": {
			transformer.MethodPost: {Method: transformer.MethodPost, Path: "/ports", OperationID: "createPort",
				RequestSchema: &transformer.SchemaSpec{Type: "object", Properties: map[string]transformer.SchemaSpec{"port": {Type: "string"}}}},
		},
		"/ports/{portId}": {
			transformer.MethodGet: {Method: transformer.MethodGet, Path: "/ports/{portId}", OperationID: "getPort",
				ResponseSchema: &transformer.SchemaSpec{
					Type:     "object",
					Required: []string{"port"},
					Properties: map[string]transformer.SchemaSpec{
						"port": {Type: "string"},
					},
				}},
			transformer.MethodDelete: {Method: transformer.MethodDelete, Path: "/ports/{portId}", OperationID: "deletePort"},
		},
	}
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/ports": {Post: &parser.Operation{OperationID: "createPort"}},
		"/ports/{portId}": {
			Get:    &parser.Operation{OperationID: "getPort"},
			Delete: &parser.Operation{OperationID: "deletePort"},
		},
	}}
	preview := &ir.ProviderIR{}
	consumed := map[string]map[string]bool{}
	var diags diagnostics.Diagnostics
	applyResourceCreationOverrides(preview, spec, "acme", pathOps, []config.ResourceOverride{
		{GenerateResource: boolPtr(true), CreateOperation: "createPort", ReadOperation: "getPort", DeleteOperation: "deletePort", IDAttribute: "port_id"},
	}, consumed, &diags)
	if len(preview.Resources) != 1 {
		t.Fatalf("expected 1 override-created resource, got %d", len(preview.Resources))
	}
	res := preview.Resources[0]
	if res.IDAttribute != "port_id" {
		t.Errorf("IDAttribute = %q, want port_id (explicit override)", res.IDAttribute)
	}
	// The synthetic port_id placeholder must be present so the override's chosen
	// attribute stays in the schema.
	found := false
	for _, a := range res.Schema.Attributes {
		if a.Name == "port_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("synthetic port_id attribute expected with skipUserSettableID, got %+v", res.Schema.Attributes)
	}
}

// TestGroupedImportFormat_PathParamNameFallback drives the IDSimple fallback in
// groupedImportFormat: when a schema attribute carries the raw path parameter
// name, the import populates that attribute (what the read substitutes) rather
// than the resolved ID attribute (e.g. an "id" response echo the read does not
// use).
func TestGroupedImportFormat_PathParamNameFallback(t *testing.T) {
	g := transformer.ResourceCRUD{
		ID: transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"name"}, AttributeName: "name", ImportFormat: "%s"},
	}
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	got, ok := groupedImportFormat(g, schema, "id")
	if !ok || got != "{name}" {
		t.Errorf("groupedImportFormat = %q, %v; want {name}, true", got, ok)
	}
}

// TestGroupedImportFormat_IDAttrFallback drives the fallback to the resolved ID
// attribute when no schema attribute matches the path parameter name.
func TestGroupedImportFormat_IDAttrFallback(t *testing.T) {
	g := transformer.ResourceCRUD{
		ID: transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"policyId"}, AttributeName: "policy_id", ImportFormat: "%s"},
	}
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	got, ok := groupedImportFormat(g, schema, "id")
	if !ok || got != "{id}" {
		t.Errorf("groupedImportFormat = %q, %v; want {id}, true", got, ok)
	}
}

// TestGroupedImportFormat_RequiredReadParamsJoinsFormat locks in the G39 fix:
// a required query parameter on the read (e.g. GigaVUE-FM's clusterId on
// /portConfig/gigastreams/advHash/{slotId}) is sent from state on the refresh
// that follows every import, so the import format must populate it — the
// format becomes composite ("{slot_id}:{cluster_id}") instead of leaving the
// param null and the read failing.
func TestGroupedImportFormat_RequiredReadParamsJoinsFormat(t *testing.T) {
	g := transformer.ResourceCRUD{
		ID: transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"slotId"}, AttributeName: "slot_id"},
		Read: &transformer.Operation{
			Method: transformer.MethodGet,
			Path:   "/portConfig/gigastreams/advHash/{slotId}",
			Parameters: []transformer.Parameter{
				{Name: "clusterId", In: "query", Required: true, Type: "string"},
			},
		},
	}
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "slot_id", Required: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "cluster_id", Required: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	got, ok := groupedImportFormat(g, schema, "slot_id")
	if !ok || got != "{slot_id}:{cluster_id}" {
		t.Errorf("groupedImportFormat = %q, %v; want {slot_id}:{cluster_id}, true", got, ok)
	}
}

// TestGroupedImportFormat_ComputedOnlyReadParamSuppressesImport locks in the
// G39 refusal: when a required read parameter maps to a Computed-only
// attribute, the practitioner cannot supply it at import time, so the resource
// stays non-importable and a fail-loud warning explains why.
func TestGroupedImportFormat_ComputedOnlyReadParamSuppressesImport(t *testing.T) {
	g := transformer.ResourceCRUD{
		Name: "adv_hash",
		ID:   transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"slotId"}, AttributeName: "slot_id"},
		Read: &transformer.Operation{
			Method: transformer.MethodGet,
			Path:   "/portConfig/gigastreams/advHash/{slotId}",
			Parameters: []transformer.Parameter{
				{Name: "clusterId", In: "query", Required: true, Type: "string"},
			},
		},
	}
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "slot_id", Required: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "cluster_id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	var diags diagnostics.Diagnostics
	got, ok := groupedImportFormatWithDiagnostics(g, schema, "slot_id", &diags)
	if ok {
		t.Errorf("groupedImportFormat = (%q,true), want not importable", got)
	}
	found := false
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Detail, "cluster_id") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning surfaced for the computed-only required read parameter: %+v", diags)
	}
}

// TestGroupedImportFormat_ComputedOnlyIdentifierSuppressesImport locks in the
// G39 refusal for the identifier itself: an import may never target a
// Computed-only attribute (the practitioner cannot know the value before the
// first read), so e.g. a server-assigned {policyId} read path with a
// computed-only policy_id attribute stays non-importable with a warning.
func TestGroupedImportFormat_ComputedOnlyIdentifierSuppressesImport(t *testing.T) {
	g := transformer.ResourceCRUD{
		Name: "policy",
		ID:   transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"policyId"}, AttributeName: "policy_id"},
	}
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "policy_id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	var diags diagnostics.Diagnostics
	got, ok := groupedImportFormatWithDiagnostics(g, schema, "policy_id", &diags)
	if ok {
		t.Errorf("groupedImportFormat = (%q,true), want not importable", got)
	}
	found := false
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "computed-only") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning surfaced for the computed-only identifier: %+v", diags)
	}
}

// TestGroupedImportFormat_SingletonResource locks in §3.13 (copilot_config):
// a resource whose read substitutes nothing into the path and carries no
// required query/header parameters is importable even though its only
// identity candidate is a Computed-only id echo — the refresh after import
// ignores the stored ID and repopulates state from the response, so any
// identifier works.
func TestGroupedImportFormat_SingletonResource(t *testing.T) {
	g := transformer.ResourceCRUD{
		ID:   transformer.IDInfo{Kind: transformer.IDSimple},
		Read: &transformer.Operation{Method: transformer.MethodGet, Path: "/copilot/config"},
	}
	// The id attribute is Computed-only: Computed with neither Required nor
	// Optional, mirroring a server-assigned response echo.
	schema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "setting", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	got, ok := groupedImportFormat(g, schema, "id")
	if !ok || got != "{id}" {
		t.Errorf("groupedImportFormat(singleton) = %q, %v; want {id}, true", got, ok)
	}

	// The same Computed-only id on a resource WITH a path parameter still
	// refuses import (the practitioner cannot know the value out of band).
	gPath := transformer.ResourceCRUD{
		ID:   transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"alias"}},
		Read: &transformer.Operation{Method: transformer.MethodGet, Path: "/portConfig/{alias}"},
	}
	if _, ok := groupedImportFormat(gPath, schema, "id"); ok {
		t.Errorf("groupedImportFormat(computed id with path param) = importable; want refused")
	}
}
