package api

import (
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
			transformer.MethodPost: {Method: transformer.MethodPost, Path: "/policies", OperationID: "createPolicy"},
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
