package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// testOp builds a transformer.Operation with Method and Path populated, mirroring
// how the real pipeline constructs pathOps (handler.go) so CRUD-group helpers
// like transformer.HasFullCRUD behave as they do on a real spec.
func testOp(method transformer.HTTPMethod, path string) transformer.Operation {
	return transformer.Operation{Method: method, Path: path}
}

func TestValidate_ValidOpenAPI3JSON(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Pet Store", "version": "1.0.0"},
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			},
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.Detected.Version != "3.0.1" {
		t.Errorf("expected version 3.0.1, got %q", resp.Detected.Version)
	}
	if resp.Detected.Operations != 2 {
		t.Errorf("expected 2 operations, got %d", resp.Detected.Operations)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}
	if resp.IRPreview.Name != "pet-store" {
		t.Errorf("expected provider name pet-store, got %q", resp.IRPreview.Name)
	}
	if len(resp.IRPreview.DataSources) != 1 {
		t.Errorf("expected 1 data source, got %d", len(resp.IRPreview.DataSources))
	}
	// The spec has a create (POST /pets) and a read (GET /pets/{id}) but no
	// delete, so the group is not full CRUD: the create is reclassified as an
	// action rather than scaffolded as an empty resource.
	if len(resp.IRPreview.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resp.IRPreview.Resources))
	}
	if len(resp.IRPreview.Actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(resp.IRPreview.Actions))
	}
	if !strings.Contains(resp.SuggestedConfig, "name: pet-store") {
		t.Errorf("expected suggested config to contain provider name, got:\n%s", resp.SuggestedConfig)
	}
}

func TestValidate_ValidOpenAPI2YAML(t *testing.T) {
	body := []byte(`swagger: "2.0"
info:
  title: Widget API
  version: "1.0"
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.Detected.Version != "2.0" {
		t.Errorf("expected version 2.0, got %q", resp.Detected.Version)
	}
	if resp.Detected.DataSources != 1 {
		t.Errorf("expected 1 data source, got %d", resp.Detected.DataSources)
	}
}

func TestValidate_EmptyBody(t *testing.T) {
	resp := Validate([]byte{})
	if resp.Valid {
		t.Error("expected empty body to be invalid")
	}
	if len(resp.Diagnostics) == 0 {
		t.Fatal("expected diagnostics for empty body")
	}
	if resp.Diagnostics[0].Severity != diagnostics.Error.String() {
		t.Errorf("expected error severity, got %q", resp.Diagnostics[0].Severity)
	}
}

func TestValidate_MissingVersion(t *testing.T) {
	body := []byte(`{"info": {"title": "No Version"}}`)

	resp := Validate(body)
	if resp.Valid {
		t.Error("expected missing version to be invalid")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if d.Summary == "Missing OpenAPI version" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected missing version diagnostic, got: %+v", resp.Diagnostics)
	}
}

func TestValidate_WithConfig(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Override API", "version": "1.0.0"},
		"config": "provider:\n  name: override_provider\n  version: \"2.0.0\"\n",
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil || resp.IRPreview.Name != "override_provider" {
		t.Errorf("expected provider name override_provider, got %+v", resp.IRPreview)
	}
	if !strings.Contains(resp.SuggestedConfig, "name: override_provider") {
		t.Errorf("expected suggested config to use override name, got:\n%s", resp.SuggestedConfig)
	}
}

func TestValidate_ContentTypeRouting(t *testing.T) {
	yamlBody := []byte(`openapi: 3.0.0
info:
  title: YAML API
  version: "1.0.0"
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: ok
`)
	jsonBody := []byte(`{"openapi": "3.0.0", "info": {"title": "JSON API", "version": "1.0.0"}, "paths": {"/items": {"get": {"operationId": "listItems", "responses": {"200": {"description": "ok"}}}}}}`)

	yamlResp := ValidateWithContentType(yamlBody, "application/yaml")
	if !yamlResp.Valid {
		t.Fatalf("expected YAML Content-Type to parse, got diagnostics: %+v", yamlResp.Diagnostics)
	}
	if yamlResp.Detected.Version != "3.0.0" {
		t.Errorf("expected version 3.0.0, got %q", yamlResp.Detected.Version)
	}

	jsonResp := ValidateWithContentType(jsonBody, "application/json")
	if !jsonResp.Valid {
		t.Fatalf("expected JSON Content-Type to parse, got diagnostics: %+v", jsonResp.Diagnostics)
	}
	if jsonResp.Detected.Version != "3.0.0" {
		t.Errorf("expected version 3.0.0, got %q", jsonResp.Detected.Version)
	}

	// A flow-style YAML document that starts with '{' should be routed to the YAML
	// parser when the caller declares an application/yaml media type. The scenario
	// is confirmed by the absence of any request.json error: either the document
	// parses (valid) or it fails inside the YAML parser with a request.yaml error.
	// Both prove YAML routing; only a request.json error would mean JSON routing.
	// Previously the valid case returned early and asserted nothing (L-17).
	flowYAML := []byte(`{openapi: "3.0.0", info: {title: Flow, version: "1.0.0"}, paths: {}}`)
	flowResp := ValidateWithContentType(flowYAML, "application/yaml")
	for _, d := range flowResp.Diagnostics {
		if d.Summary == "Failed to parse request body" && strings.Contains(d.Detail, "request.json") {
			t.Errorf("flow-style YAML with application/yaml Content-Type was routed to JSON parser: %s", d.Detail)
		}
	}
}

func TestValidate_OpenAPI31_SchemaAndSecurityFeatures(t *testing.T) {
	body := []byte(`{
		"openapi": "3.1.0",
		"info": {"title": "Schema Feature API", "version": "1.0.0"},
		"paths": {
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"components": {
			"schemas": {
				"Pet": {
					"oneOf": [
						{"$ref": "#/components/schemas/Cat"},
						{"$ref": "#/components/schemas/Dog"}
					],
					"discriminator": {"propertyName": "petType"}
				},
				"Cat": {
					"allOf": [{"$ref": "#/components/schemas/PetBase"}],
					"properties": {"name": {"type": "string"}}
				},
				"Dog": {
					"anyOf": [{"$ref": "#/components/schemas/PetBase"}],
					"properties": {"breed": {"type": "string"}}
				},
				"PetBase": {
					"type": "object",
					"properties": {
						"secret": {"type": "string", "writeOnly": true},
						"id": {"type": "string", "readOnly": true},
						"nickname": {"type": ["string", "null"], "nullable": true}
					}
				}
			},
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
				"oauth2": {
					"type": "oauth2",
					"flows": {
						"clientCredentials": {"tokenUrl": "https://example.com/token", "scopes": {"read": "read access"}}
					}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.Detected.Version != "3.1.0" {
		t.Errorf("expected version 3.1.0, got %q", resp.Detected.Version)
	}
	if resp.Detected.Operations != 1 {
		t.Errorf("expected 1 operation, got %d", resp.Detected.Operations)
	}
	if resp.Detected.DataSources != 1 {
		t.Errorf("expected 1 data source, got %d", resp.Detected.DataSources)
	}
	if resp.Detected.Schemas != 4 {
		t.Errorf("expected 4 schemas, got %d", resp.Detected.Schemas)
	}
	if resp.Detected.SchemasWithOneOf != 1 {
		t.Errorf("expected 1 schema with oneOf, got %d", resp.Detected.SchemasWithOneOf)
	}
	if resp.Detected.SchemasWithAllOf != 1 {
		t.Errorf("expected 1 schema with allOf, got %d", resp.Detected.SchemasWithAllOf)
	}
	if resp.Detected.SchemasWithAnyOf != 1 {
		t.Errorf("expected 1 schema with anyOf, got %d", resp.Detected.SchemasWithAnyOf)
	}
	if resp.Detected.WriteOnlyAttributes != 1 {
		t.Errorf("expected 1 write-only attribute, got %d", resp.Detected.WriteOnlyAttributes)
	}
	if resp.Detected.ReadOnlyAttributes != 1 {
		t.Errorf("expected 1 read-only attribute, got %d", resp.Detected.ReadOnlyAttributes)
	}
	if resp.Detected.NullableAttributes != 1 {
		t.Errorf("expected 1 nullable attribute, got %d", resp.Detected.NullableAttributes)
	}
	if resp.Detected.SecuritySchemes != 2 {
		t.Errorf("expected 2 security schemes, got %d", resp.Detected.SecuritySchemes)
	}
	if resp.Detected.PolymorphismStrategy != "dynamic_union" {
		t.Errorf("expected polymorphism strategy dynamic_union, got %q", resp.Detected.PolymorphismStrategy)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}
	if len(resp.IRPreview.SecurityIR.Schemes) != 2 {
		t.Errorf("expected 2 security schemes in IR preview, got %d", len(resp.IRPreview.SecurityIR.Schemes))
	}
	if len(resp.IRPreview.SecurityIR.Schemes) == 2 {
		if resp.IRPreview.SecurityIR.Schemes[0].Name != "apiKey" {
			t.Errorf("expected first scheme apiKey, got %q", resp.IRPreview.SecurityIR.Schemes[0].Name)
		}
		if resp.IRPreview.SecurityIR.Schemes[1].Name != "oauth2" {
			t.Errorf("expected second scheme oauth2, got %q", resp.IRPreview.SecurityIR.Schemes[1].Name)
		}
	}
}

func TestValidate_OptInConstructsAndProviderSettings(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Opt-In API", "version": "1.0.0"},
		"config": "provider:\n  name: optin_provider\n  version: \"2.0.0\"\npagination:\n  style: offset\n  page_param: page\nlogging:\n  enabled: true\ngenerate_terraform_tests: true\nresource_overrides:\n  - schema: Pet\n    import_format: \"{id}\"\n    state_upgrades:\n      - from: 0\n        renames:\n          old_name: name\naction_overrides:\n  - operation: adoptPet\n    name: adopt\nephemeral_resource_overrides:\n  - operation: getToken\n    name: api_token\nlist_resource_overrides:\n  - resource: Pet\n    operation: listPets\nfunction_overrides:\n  - operation: lookupPet\n    name: lookup\n    arguments:\n      - name: pet_id\n        type: string\n",
		"paths": {
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil || resp.IRPreview.Name != "optin_provider" {
		t.Errorf("expected provider name optin_provider, got %+v", resp.IRPreview)
	}
	if resp.Detected.Actions != 1 {
		t.Errorf("expected 1 action, got %d", resp.Detected.Actions)
	}
	if resp.Detected.EphemeralResources != 1 {
		t.Errorf("expected 1 ephemeral resource, got %d", resp.Detected.EphemeralResources)
	}
	if resp.Detected.ListResources != 1 {
		t.Errorf("expected 1 list resource, got %d", resp.Detected.ListResources)
	}
	if resp.Detected.Functions != 1 {
		t.Errorf("expected 1 function, got %d", resp.Detected.Functions)
	}
	if resp.Detected.StateUpgraders != 1 {
		t.Errorf("expected 1 state upgrader, got %d", resp.Detected.StateUpgraders)
	}
	if resp.Detected.PaginationStyle != "offset" {
		t.Errorf("expected pagination style offset, got %q", resp.Detected.PaginationStyle)
	}
	if resp.Detected.ImportableResources != 1 {
		t.Errorf("expected 1 importable resource, got %d", resp.Detected.ImportableResources)
	}
	if !resp.Detected.GenerateTerraformTests {
		t.Error("expected generate_terraform_tests to be true")
	}
	if !resp.Detected.LoggingEnabled {
		t.Error("expected logging_enabled to be true")
	}
	if len(resp.IRPreview.Actions) != 1 || resp.IRPreview.Actions[0].Name != "adopt" {
		t.Errorf("expected action preview adopt, got %+v", resp.IRPreview.Actions)
	}
	if len(resp.IRPreview.EphemeralResources) != 1 || resp.IRPreview.EphemeralResources[0].Name != "api_token" {
		t.Errorf("expected ephemeral resource preview api_token, got %+v", resp.IRPreview.EphemeralResources)
	}
	if len(resp.IRPreview.ListResources) != 1 || resp.IRPreview.ListResources[0].Name != "pet" {
		t.Errorf("expected list resource preview pet, got %+v", resp.IRPreview.ListResources)
	}
	if len(resp.IRPreview.Functions) != 1 || resp.IRPreview.Functions[0].Name != "lookup" {
		t.Errorf("expected function preview lookup, got %+v", resp.IRPreview.Functions)
	}
	if len(resp.IRPreview.Functions[0].Arguments) != 1 || resp.IRPreview.Functions[0].Arguments[0].Name != "pet_id" {
		t.Errorf("expected function argument pet_id, got %+v", resp.IRPreview.Functions[0].Arguments)
	}
	if resp.IRPreview.Functions[0].Arguments[0].Schema.Type != "string" {
		t.Errorf("expected function argument type string, got %q", resp.IRPreview.Functions[0].Arguments[0].Schema.Type)
	}
}

// TestValidate_DuplicateOperationIdFailsLoud verifies that two operations
// sharing an operationId (which normalize to the same construct name) produce
// an error diagnostic instead of a confusing "duplicate output path" error from
// the generator.
func TestValidate_DuplicateOperationIdFailsLoud(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Dup API", "version": "1.0.0"},
		"paths": {
			"/snmp/throttle": {
				"put": {
					"operationId": "redefineSnmpThrottleConfig",
					"responses": {"200": {"description": "ok"}}
				}
			},
			"/system/snmp/throttle": {
				"put": {
					"operationId": "redefineSnmpThrottleConfig",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if resp.Valid {
		t.Fatalf("expected invalid response for duplicate operationId, got valid: %+v", resp.Diagnostics)
	}
	found := false
	for _, d := range resp.Diagnostics {
		if d.Severity == diagnostics.Error.String() && strings.Contains(d.Summary, "duplicate action name") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate action name error diagnostic, got %+v", resp.Diagnostics)
	}
}

// TestValidate_DuplicateOperationIdResolvedByMethodPathOverride verifies that a
// generator.yaml action override matching by "METHOD /path" renames one of two
// colliding operations (which share an operationId) so the duplicate diagnostic
// clears and both actions are emitted with distinct names.
func TestValidate_DuplicateOperationIdResolvedByMethodPathOverride(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Dup API", "version": "1.0.0"},
		"config": "provider:\n  name: dup_provider\n  version: \"1.0.0\"\naction_overrides:\n  - operation: \"PUT /system/snmp/throttle\"\n    name: redefine_system_snmp_throttle_config\n",
		"paths": {
			"/snmp/throttle": {
				"put": {
					"operationId": "redefineSnmpThrottleConfig",
					"responses": {"200": {"description": "ok"}}
				}
			},
			"/system/snmp/throttle": {
				"put": {
					"operationId": "redefineSnmpThrottleConfig",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(resp.IRPreview.Actions))
	}
	names := make(map[string]bool, 2)
	for _, a := range resp.IRPreview.Actions {
		names[a.Name] = true
	}
	if !names["redefine_snmp_throttle_config"] || !names["redefine_system_snmp_throttle_config"] {
		t.Errorf("expected both disambiguated action names, got %v", names)
	}
}

func TestValidate_ResourceOverridesAreApplied(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Override API", "version": "1.0.0"},
		"config": "provider:\n  name: override_api\n  version: \"1.0.0\"\nresource_overrides:\n  - operation: createPet\n    id_attribute: pet_id\n    import_format: \"{id}\"\n",
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			},
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				},
				"delete": {
					"operationId": "deletePet",
					"responses": {"204": {"description": "deleted"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}
	// The spec is full CRUD (create + read + delete), so the resource is
	// inferred and the id_attribute/import_format overrides apply to it.
	if len(resp.IRPreview.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resp.IRPreview.Resources))
	}
	res := resp.IRPreview.Resources[0]
	if res.IDAttribute != "pet_id" {
		t.Errorf("expected id_attribute override pet_id, got %q", res.IDAttribute)
	}
	if res.ImportIDFormat != "{id}" {
		t.Errorf("expected import_format override {id}, got %q", res.ImportIDFormat)
	}
}

// TestValidate_ActionOverrideDoubleClaimedWarnsAndSkips asserts that an action
// override whose operation is already consumed by a managed resource fails loud
// (a Warning naming the operation) and does NOT append an empty scaffold action
// for it. This is the regression for the SpaceTraders config bug where
// purchase-ship / scrap-ship were declared as both resource operations and
// action overrides: the resource consumed the operations, so no action was
// inferred, and the bare override emitted a body-less scaffold that dropped
// practitioner input at runtime.
func TestValidate_ActionOverrideDoubleClaimedWarnsAndSkips(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Double Claim API", "version": "1.0.0"},
		"config": "provider:\n  name: double_claim_api\n  version: \"1.0.0\"\naction_overrides:\n  - operation: createPet\n    name: create_pet\n",
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			},
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				},
				"delete": {
					"operationId": "deletePet",
					"responses": {"204": {"description": "deleted"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	// A warning is non-blocking: the provider is still generated.
	if !resp.Valid {
		t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected an IR preview")
	}

	var sawDoubleClaim bool
	for _, d := range resp.Diagnostics {
		if d.Severity == diagnostics.Warning.String() && strings.Contains(d.Summary, "already claimed by a resource") {
			sawDoubleClaim = true
			if !strings.Contains(d.Detail, "createPet") {
				t.Errorf("expected the warning to name the double-claimed operation, got: %s", d.Detail)
			}
		}
	}
	if !sawDoubleClaim {
		t.Errorf("expected a double-claim warning, got diagnostics: %+v", resp.Diagnostics)
	}

	// The grouped resource owns the operation; the scaffold action must not be
	// emitted.
	if len(resp.IRPreview.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(resp.IRPreview.Resources))
	}
	for _, a := range resp.IRPreview.Actions {
		if a.SourceOperation == "createPet" {
			t.Errorf("double-claimed operation must not become an action, got action %q", a.Name)
		}
	}
}

// TestValidate_ActionOverrideUnclaimedStillEmitted asserts that an action
// override for an operation no resource consumes still appends an action (the
// override is a legitimate declaration, not a double-claim).
func TestValidate_ActionOverrideUnclaimedStillEmitted(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Unclaimed Action API", "version": "1.0.0"},
		"config": "provider:\n  name: unclaimed_action_api\n  version: \"1.0.0\"\naction_overrides:\n  - operation: resetPet\n    name: reset_pet\n",
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			},
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				},
				"delete": {
					"operationId": "deletePet",
					"responses": {"204": {"description": "deleted"}}
				}
			},
			"/pets/{id}:reset": {
				"post": {
					"operationId": "resetPet",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected an IR preview")
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "already claimed by a resource") {
			t.Fatalf("an unclaimed operation must not trigger the double-claim warning, got: %+v", d)
		}
	}
	var sawReset bool
	for _, a := range resp.IRPreview.Actions {
		if a.SourceOperation == "resetPet" {
			sawReset = true
		}
	}
	if !sawReset {
		t.Errorf("expected an action for unclaimed operation resetPet, got actions: %+v", resp.IRPreview.Actions)
	}
}

// TestValidate_MultiBearerSchemesQualifyAuthAttributes asserts that a spec
// declaring several HTTP bearer schemes yields distinct, scheme-qualified
// provider-config attributes (e.g. account_token + agent_token) instead of
// collapsing onto a single bearer_token. This is the SpaceTraders auth fix: two
// bearer schemes map to two attributes so per-scheme tokens are set
// independently and per-operation WithSchemes selection is meaningful.
func TestValidate_MultiBearerSchemesQualifyAuthAttributes(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Multi Bearer API", "version": "1.0.0"},
		"paths": {
			"/me": {
				"get": {
					"operationId": "getMe",
					"security": [{"AccountToken": []}],
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"components": {
			"securitySchemes": {
				"AccountToken": {"type": "http", "scheme": "bearer"},
				"AgentToken": {"type": "http", "scheme": "bearer"}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected an IR preview")
	}

	var sawAccount, sawAgent bool
	for _, a := range resp.IRPreview.ConfigSchema.Attributes {
		switch a.Name {
		case "account_token":
			sawAccount = true
			if !a.Sensitive {
				t.Error("account_token must be a sensitive attribute")
			}
		case "agent_token":
			sawAgent = true
			if !a.Sensitive {
				t.Error("agent_token must be a sensitive attribute")
			}
		case "bearer_token":
			t.Error("multi-bearer spec must not expose a generic bearer_token attribute")
		}
	}
	if !sawAccount || !sawAgent {
		t.Errorf("expected both account_token and agent_token config attributes, got: %+v", resp.IRPreview.ConfigSchema.Attributes)
	}
}

// TestValidate_DetectedMatchesIRPreviewConstructCounts asserts that the
// `detected` construct counts and the `ir_preview` construct slices report the
// same numbers for a spec whose operations classify into resources, data
// sources, actions, ephemeral resources, and functions — the consistency
// regression described in M-5.
func TestValidate_DetectedMatchesIRPreviewConstructCounts(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Mixed API", "version": "1.0.0"},
		"paths": {
			"/pets": {
				"get": {"operationId": "listPets", "responses": {"200": {"description": "ok"}}},
				"post": {"operationId": "createPet", "responses": {"201": {"description": "ok"}}}
			},
			"/pets/{id}": {
				"get": {"operationId": "getPet", "responses": {"200": {"description": "ok"}}}
			},
			"/pets/{id}:adopt": {
				"post": {"operationId": "adoptPet", "responses": {"200": {"description": "ok"}}}
			},
			"/pets/search": {
				"get": {"operationId": "searchPets", "responses": {"200": {"description": "ok"}}}
			},
			"/tokens": {
				"post": {"operationId": "createToken", "responses": {"201": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}
	if resp.Detected.Resources != len(resp.IRPreview.Resources) {
		t.Errorf("detected.Resources=%d != ir_preview.Resources=%d", resp.Detected.Resources, len(resp.IRPreview.Resources))
	}
	if resp.Detected.DataSources != len(resp.IRPreview.DataSources) {
		t.Errorf("detected.DataSources=%d != ir_preview.DataSources=%d", resp.Detected.DataSources, len(resp.IRPreview.DataSources))
	}
	if resp.Detected.Actions != len(resp.IRPreview.Actions) {
		t.Errorf("detected.Actions=%d != ir_preview.Actions=%d", resp.Detected.Actions, len(resp.IRPreview.Actions))
	}
	if resp.Detected.EphemeralResources != len(resp.IRPreview.EphemeralResources) {
		t.Errorf("detected.Ephemeral=%d != ir_preview.Ephemeral=%d", resp.Detected.EphemeralResources, len(resp.IRPreview.EphemeralResources))
	}
	if resp.Detected.Functions != len(resp.IRPreview.Functions) {
		t.Errorf("detected.Functions=%d != ir_preview.Functions=%d", resp.Detected.Functions, len(resp.IRPreview.Functions))
	}
	// Sanity: the spec must actually exercise the non-resource kinds, else the
	// test above passes vacuously.
	if len(resp.IRPreview.Functions) == 0 && len(resp.IRPreview.EphemeralResources) == 0 && len(resp.IRPreview.Actions) == 0 {
		t.Fatal("spec failed to produce any action/ephemeral/function constructs; test is vacuous")
	}
}

// TestValidateContextWithContentType_CanceledAborts verifies that a canceled
// request context short-circuits the pipeline rather than running the full
// parse (M-6).
func TestValidateContextWithContentType_CanceledAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body := []byte(`{"openapi":"3.0.1","info":{"title":"X","version":"1"},"paths":{}}`)
	resp := ValidateContextWithContentType(ctx, body, "application/json")

	if resp.Valid {
		t.Fatal("expected canceled context to produce an invalid response")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if d.Summary == "Request canceled" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a Request canceled diagnostic, got %+v", resp.Diagnostics)
	}
}

func TestOperationMappingFromString(t *testing.T) {
	tests := []struct {
		input      string
		wantPath   string
		wantMethod string
	}{
		{"GET /pets/{id}", "/pets/{id}", "GET"},
		{"post /pets/{id}", "/pets/{id}", "POST"},
		{"/pets/{id}", "/pets/{id}", ""},
		{"", "", ""},
		{"GET", "GET", ""},
	}
	for _, tc := range tests {
		got := operationMappingFromString(tc.input)
		if got.PathTemplate != tc.wantPath {
			t.Errorf("operationMappingFromString(%q).PathTemplate = %q, want %q", tc.input, got.PathTemplate, tc.wantPath)
		}
		if got.Method != tc.wantMethod {
			t.Errorf("operationMappingFromString(%q).Method = %q, want %q", tc.input, got.Method, tc.wantMethod)
		}
	}
}

func TestPrimitiveTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"string", "string"},
		{"STRING", "string"},
		{"integer", "integer"},
		{"int", "integer"},
		{"number", "number"},
		{"float", "number"},
		{"boolean", "boolean"},
		{"bool", "boolean"},
		{"null", "null"},
		{"", "dynamic"},
		{"unknown", "dynamic"},
		{"  Int  ", "integer"},
	}
	for _, tc := range tests {
		got := primitiveTypeFromString(tc.input)
		if string(got) != tc.want {
			t.Errorf("primitiveTypeFromString(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidate_InvalidConfig(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Bad Config", "version": "1.0.0"},
		"config": "provider:\n  version: \"1.0.0\"\n",
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if resp.Valid {
		t.Error("expected invalid config to make response invalid")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if d.Summary == "Invalid generator.yaml config" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected invalid config diagnostic, got: %+v", resp.Diagnostics)
	}
}

func TestValidate_ConfigTypeMismatch(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"object config", `{"provider": {"name": "x"}}`, "mapping/object"},
		{"array config", `[]`, "array"},
		{"number config", `42`, "non-string scalar"},
		{"boolean config", `true`, "non-string scalar"},
		{"null config", `null`, "non-string scalar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{
				"openapi": "3.0.1",
				"info": {"title": "Type Mismatch", "version": "1.0.0"},
				"config": %s,
				"paths": {
					"/items": {
						"get": {"operationId": "listItems", "responses": {"200": {"description": "ok"}}}
					}
				}
			}`, tt.config))

			resp := Validate(body)
			if resp.Valid {
				t.Error("expected non-string config to make response invalid")
			}
			found := false
			for _, d := range resp.Diagnostics {
				if d.Summary == "Invalid config field type" && strings.Contains(d.Detail, tt.want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected config type mismatch diagnostic for %s, got: %+v", tt.want, resp.Diagnostics)
			}
		})
	}

	t.Run("string scalar with null byte", func(t *testing.T) {
		// json.Marshal encodes a NUL byte as the escaped JSON \u0000 sequence,
		// keeping the illegal source byte out of the test file while still
		// sending a control character through the validate pipeline.
		configValue, err := json.Marshal("\x00")
		if err != nil {
			t.Fatalf("failed to marshal control-char config value: %v", err)
		}
		body := []byte(fmt.Sprintf(`{
			"openapi": "3.0.1",
			"info": {"title": "Control Char", "version": "1.0.0"},
			"config": %s,
			"paths": {
				"/items": {
					"get": {"operationId": "listItems", "responses": {"200": {"description": "ok"}}}
				}
			}
		}`, configValue))

		resp := Validate(body)
		if resp.Valid {
			t.Error("expected control-character config to make response invalid")
		}
		found := false
		for _, d := range resp.Diagnostics {
			if d.Summary == "Invalid generator.yaml config" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected invalid config diagnostic for control-character string, got: %+v", resp.Diagnostics)
		}
	})
}

func TestValidate_CRUDMappingByMethod(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "CRUD API", "version": "1.0.0"},
		"paths": {
			"/pets": {
				"post": {"operationId": "createPet", "responses": {"201": {"description": "created"}}}
			},
			"/pets/{id}": {
				"get": {"operationId": "getPet", "responses": {"200": {"description": "ok"}}},
				"put": {"operationId": "updatePet", "responses": {"200": {"description": "ok"}}},
				"patch": {"operationId": "patchPet", "responses": {"200": {"description": "ok"}}},
				"delete": {"operationId": "deletePet", "responses": {"204": {"description": "deleted"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	// A full CRUD group (create + read + update + delete) becomes a single
	// grouped resource whose CRUD mapping binds each lifecycle method to the
	// right HTTP verb.
	if len(resp.IRPreview.Resources) != 1 {
		t.Fatalf("expected 1 grouped resource, got %d", len(resp.IRPreview.Resources))
	}
	r := resp.IRPreview.Resources[0]
	if r.CRUDMapping.Create.Method != http.MethodPost {
		t.Errorf("expected POST create mapping, got %+v", r.CRUDMapping.Create)
	}
	if r.CRUDMapping.Read.Method != http.MethodGet {
		t.Errorf("expected GET read mapping, got %+v", r.CRUDMapping.Read)
	}
	if r.CRUDMapping.Update == nil || r.CRUDMapping.Update.Method != http.MethodPut {
		t.Errorf("expected PUT update mapping, got %+v", r.CRUDMapping.Update)
	}
	if r.CRUDMapping.Delete.Method != http.MethodDelete {
		t.Errorf("expected DELETE delete mapping, got %+v", r.CRUDMapping.Delete)
	}
	// The PATCH is the partial update; with a PUT present it is subsumed by the
	// grouped resource's Update and consumed, so it is not double-emitted as a
	// separate action or scaffolded as an empty resource.
	if len(resp.IRPreview.Actions) != 0 {
		t.Fatalf("expected 0 actions, got %d", len(resp.IRPreview.Actions))
	}
}

func TestValidate_DetectedSummaryOmitsEmptyOptInFields(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Simple API", "version": "1.0.0"},
		"paths": {
			"/items": {
				"get": {"operationId": "listItems", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	data, err := json.Marshal(resp.Detected)
	if err != nil {
		t.Fatalf("failed to marshal detected summary: %v", err)
	}
	for _, field := range []string{"actions", "ephemeral_resources", "list_resources", "functions"} {
		if strings.Contains(string(data), fmt.Sprintf("%q", field)) {
			t.Errorf("expected %q to be omitted, got %s", field, data)
		}
	}
}

func TestNewValidateHandler_MethodNotAllowed(t *testing.T) {
	handler := NewValidateHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	methods := []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), method, server.URL+"/api/v1/validate", http.NoBody)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("failed to reach server: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("expected 405, got %d", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected application/json error, got %q", ct)
			}
			// HEAD responses have no body by convention; only the status code and
			// content type need to be asserted.
			if method == http.MethodHead {
				return
			}
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				t.Fatalf("failed to read error response body: %v", readErr)
			}
			var errBody errorBody
			if err := json.Unmarshal(body, &errBody); err != nil {
				t.Fatalf("expected JSON error body, got %s: %v", body, err)
			}
			if errBody.Error == "" {
				t.Errorf("expected non-empty error field in body, got %+v", errBody)
			}
			if errBody.Code != http.StatusText(http.StatusMethodNotAllowed) {
				t.Errorf("expected code %q, got %q", http.StatusText(http.StatusMethodNotAllowed), errBody.Code)
			}
		})
	}
}

func TestNewValidateHandler_EmptyBody(t *testing.T) {
	handler := NewValidateHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/api/v1/validate", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to reach server: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation result in body), got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json response, got %q", ct)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("failed to read response body: %v", readErr)
	}
	var vr ValidateResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		t.Fatalf("expected JSON response, got %s: %v", body, err)
	}
	if vr.Valid {
		t.Error("expected empty body to be invalid")
	}
}

func TestGenerateStarterConfig_SpecVersionFixtures(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantVer parser.Version
	}{
		{"OpenAPI 2.0", `swagger: "2.0"
info:
  title: Widget API
  version: "1.0"
paths: {}
`, parser.Version2_0},
		{"OpenAPI 3.0", `openapi: 3.0.3
info:
  title: Widget API
  version: "1.0"
paths: {}
`, parser.Version3_0},
		{"OpenAPI 3.1", `openapi: 3.1.0
info:
  title: Widget API
  version: "1.0"
paths: {}
`, parser.Version3_1},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg, version, diags, err := GenerateStarterConfig([]byte(tt.spec), "")
			if err != nil {
				t.Fatalf("GenerateStarterConfig error: %v", err)
			}
			if version != tt.wantVer {
				t.Errorf("version = %q, want %q", version, tt.wantVer)
			}
			for _, d := range diags {
				if d.Severity == parser.SeverityError {
					t.Errorf("unexpected error diagnostic: %v", d)
				}
			}
			if cfg.Provider.Name == "" {
				t.Error("expected non-empty provider name")
			}
		})
	}
}

func TestGenerateStarterConfig_VersionDiagnosticsOnError(t *testing.T) {
	spec := []byte(`info:
  title: Widget API
  version: "1.0"
paths: {}
`)
	_, version, diags, err := GenerateStarterConfig(spec, "")
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if version != parser.VersionUnknown {
		t.Errorf("version = %q, want unknown", version)
	}
	if len(diags) == 0 {
		t.Fatal("expected version diagnostics on error")
	}
}

func TestValidate_SuggestedConfigAuthAndPagination(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Secure Paginated API", "version": "1.0.0"},
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"parameters": [
						{"name": "page", "in": "query", "schema": {"type": "integer"}},
						{"name": "limit", "in": "query", "schema": {"type": "integer"}}
					],
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"components": {
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
				"bearer": {"type": "http", "scheme": "bearer"}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if !strings.Contains(resp.SuggestedConfig, "auth:") {
		t.Errorf("expected suggested config to contain auth section, got:\n%s", resp.SuggestedConfig)
	}
	if !strings.Contains(resp.SuggestedConfig, "pagination:") {
		t.Errorf("expected suggested config to contain pagination section, got:\n%s", resp.SuggestedConfig)
	}
	if !strings.Contains(resp.SuggestedConfig, "page_param: page") {
		t.Errorf("expected pagination page_param, got:\n%s", resp.SuggestedConfig)
	}
	if !strings.Contains(resp.SuggestedConfig, "per_page_param: limit") {
		t.Errorf("expected pagination per_page_param, got:\n%s", resp.SuggestedConfig)
	}
}

func TestValidate_SuggestedConfigNoAuthWhenMissing(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Simple API", "version": "1.0.0"},
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if strings.Contains(resp.SuggestedConfig, "auth:") {
		t.Errorf("expected no auth section when no security schemes, got:\n%s", resp.SuggestedConfig)
	}
	if strings.Contains(resp.SuggestedConfig, "pagination:") {
		t.Errorf("expected no pagination section when no pagination params, got:\n%s", resp.SuggestedConfig)
	}
}

// TestValidate_ORSecurityRequirementsWarns asserts that a spec declaring more
// than one global security requirement (OR semantics — any one suffices) surfaces
// a Warning diagnostic rather than silently mis-resolving, and that the global
// requirements are carried into SecurityIR.DefaultRequirements instead of being
// dropped. eidos applies every declared scheme (AND of all), which is stricter
// than OR; the warning is the fail-loud surface for that gap (REMAINING_GAPS §1.2).
func TestValidate_ORSecurityRequirementsWarns(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "OR Auth API", "version": "1.0.0"},
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"security": [
			{"apiKey": []},
			{"bearer": []}
		],
		"components": {
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
				"bearer": {"type": "http", "scheme": "bearer"}
			}
		}
	}`)

	resp := Validate(body)
	// A warning is non-blocking: the spec is still valid.
	if !resp.Valid {
		t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
	}

	var sawORWarning bool
	for _, d := range resp.Diagnostics {
		if d.Severity == diagnostics.Warning.String() && strings.Contains(d.Summary, "OR security-requirement resolution not modeled") {
			sawORWarning = true
			if !strings.Contains(d.Detail, "OR") {
				t.Errorf("expected warning detail to explain OR semantics, got: %s", d.Detail)
			}
		}
	}
	if !sawORWarning {
		t.Errorf("expected an OR security-requirement warning, got diagnostics: %+v", resp.Diagnostics)
	}

	// The two global requirements must be carried into the IR, not dropped.
	if resp.IRPreview == nil {
		t.Fatalf("expected an IR preview, got nil")
	}
	if got := len(resp.IRPreview.SecurityIR.DefaultRequirements); got != 2 {
		t.Errorf("expected 2 DefaultRequirements (one per OR alternative), got %d", got)
	}
}

// TestValidate_SingleSecurityRequirementNoORWarning asserts that a single global
// security requirement (AND-of-one, the common case) does NOT trigger the OR
// warning — eidos applying all declared schemes matches that semantics, so
// there is nothing to warn about. The requirement is still carried into
// DefaultRequirements.
func TestValidate_SingleSecurityRequirementNoORWarning(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Single Auth API", "version": "1.0.0"},
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"security": [
			{"apiKey": []}
		],
		"components": {
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "OR security-requirement resolution not modeled") {
			t.Errorf("a single security requirement must not trigger the OR warning, got: %+v", d)
		}
	}
	if resp.IRPreview == nil {
		t.Fatalf("expected an IR preview, got nil")
	}
	if got := len(resp.IRPreview.SecurityIR.DefaultRequirements); got != 1 {
		t.Errorf("expected 1 DefaultRequirement, got %d", got)
	}
}

// TestValidate_PerOpORSecurityRequirementsWarns_OperationLevel declares a spec
// whose operation carries two security requirements (operation-level OR) and
// asserts the per-operation OR warning names that operation. The generated
// provider applies every configured scheme (AND) on such an operation rather
// than choosing one alternative, so the warning is the fail-loud surface
// (REMAINING_GAPS §1).
func TestValidate_PerOpORSecurityRequirementsWarns_OperationLevel(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Per-Op OR API", "version": "1.0.0"},
		"paths": {
			"/items/{id}": {
				"delete": {
					"operationId": "deleteItem",
					"security": [
						{"apiKey": []},
						{"bearer": []}
					],
					"responses": {"204": {"description": "no content"}}
				}
			}
		},
		"components": {
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
				"bearer": {"type": "http", "scheme": "bearer"}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
	}
	var sawPerOpOR bool
	for _, d := range resp.Diagnostics {
		if d.Severity == diagnostics.Warning.String() && strings.Contains(d.Summary, "OR security-requirement resolution not modeled") {
			if strings.Contains(d.Detail, "deleteItem") && strings.Contains(d.Detail, "2 security requirements") {
				sawPerOpOR = true
			}
		}
	}
	if !sawPerOpOR {
		t.Errorf("expected a per-operation OR warning naming deleteItem, got diagnostics: %+v", resp.Diagnostics)
	}
}

// TestValidate_PerOpSingleSecurityRequirementNoORWarning asserts that an
// operation declaring a single security requirement (per-op AND-of-one, which
// eidos wires exactly via client.WithSchemes) does NOT trigger the per-op OR
// warning, and the requirement is carried into the operation's
// SecurityRequirements in the IR.
func TestValidate_PerOpSingleSecurityRequirementNoORWarning(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Per-Op Single API", "version": "1.0.0"},
		"paths": {
			"/items/{id}": {
				"delete": {
					"operationId": "deleteItem",
					"security": [
						{"apiKey": []}
					],
					"responses": {"204": {"description": "no content"}}
				}
			}
		},
		"components": {
			"securitySchemes": {
				"apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "OR security-requirement resolution not modeled") {
			t.Errorf("a single per-operation security requirement must not trigger the OR warning, got: %+v", d)
		}
	}
}

// TestValidate_SuggestedAuthAlwaysValidates asserts that suggested auth
// configuration round-trips through config.LoadBytes + config.Validate for
// shapes that previously produced invalid starter config: an apiKey scheme
// whose map key is not the literal "apiKey" and whose location is "query"
// (M-3), and an oauth2 scheme declaring only the implicit flow, which has no
// token URL and cannot be represented (M-4).
func TestValidate_SuggestedAuthAlwaysValidates(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Mixed Auth API", "version": "1.0.0"},
		"paths": {},
		"components": {
			"securitySchemes": {
				"MyKey": {"type": "apiKey", "in": "query", "name": "api_key"},
				"cc": {
					"type": "oauth2",
					"flows": {
						"clientCredentials": {"tokenUrl": "https://example.com/token"}
					}
				},
				"implicitOnly": {
					"type": "oauth2",
					"flows": {
						"implicit": {"authorizationUrl": "https://example.com/auth"}
					}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if !strings.Contains(resp.SuggestedConfig, "auth:") {
		t.Fatalf("expected an auth section, got:\n%s", resp.SuggestedConfig)
	}

	// The suggested config must parse and pass eidos's own validation.
	cfg, err := config.LoadBytes([]byte(resp.SuggestedConfig))
	if err != nil {
		t.Fatalf("suggested config did not parse: %v\n%s", err, resp.SuggestedConfig)
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("suggested config failed validation: %v\n%s", err, resp.SuggestedConfig)
	}

	// The implicit-only oauth2 scheme (no token URL) must not be emitted, and
	// the apiKey scheme key "MyKey" must be serialized as scheme: apiKey, not
	// scheme: MyKey.
	if strings.Contains(resp.SuggestedConfig, "scheme: MyKey") {
		t.Errorf("apiKey scheme emitted with the map key instead of \"apiKey\":\n%s", resp.SuggestedConfig)
	}
	if strings.Contains(resp.SuggestedConfig, "implicit") {
		t.Errorf("implicit-only oauth2 (no token_url) should have been omitted:\n%s", resp.SuggestedConfig)
	}
	if !strings.Contains(resp.SuggestedConfig, "scheme: apiKey") {
		t.Errorf("expected an apiKey entry, got:\n%s", resp.SuggestedConfig)
	}
	if !strings.Contains(resp.SuggestedConfig, "client_id_env") {
		t.Errorf("client_credentials flow should include client_id_env:\n%s", resp.SuggestedConfig)
	}
}

// failingResponseWriter is an http.ResponseWriter whose Write always returns an
// error. It is used to exercise the error-handling paths in WriteJSONError.
type failingResponseWriter struct {
	headers  http.Header
	status   int
	writeErr error
}

func newFailingResponseWriter(err error) *failingResponseWriter {
	return &failingResponseWriter{headers: make(http.Header), writeErr: err}
}

func (f *failingResponseWriter) Header() http.Header         { return f.headers }
func (f *failingResponseWriter) WriteHeader(status int)      { f.status = status }
func (f *failingResponseWriter) Write(p []byte) (int, error) { return 0, f.writeErr }

func TestWriteJSONError_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := WriteJSONError(rec, http.StatusBadRequest, "bad request"); err != nil {
		t.Fatalf("WriteJSONError success path returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal error body: %v", err)
	}
	if body.Error != "bad request" {
		t.Errorf("error = %q, want bad request", body.Error)
	}
	if body.Code != http.StatusText(http.StatusBadRequest) {
		t.Errorf("code = %q, want %q", body.Code, http.StatusText(http.StatusBadRequest))
	}
}

func TestWriteJSONError_WriteError(t *testing.T) {
	wantErr := fmt.Errorf("simulated write failure")
	w := newFailingResponseWriter(wantErr)
	err := WriteJSONError(w, http.StatusBadRequest, "bad request")
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Errorf("error = %q, want containing %q", err.Error(), wantErr.Error())
	}
}

func TestClassifyOperation_Heuristics(t *testing.T) {
	cases := []struct {
		path, method, opID string
		extensions         map[string]any
		want               operationKind
	}{
		{"/pets", "GET", "listPets", nil, kindDataSource},
		{"/pets/{id}", "GET", "getPet", nil, kindDataSource},
		{"/pets", "POST", "createPet", nil, kindResource},
		{"/pets/{id}", "PUT", "updatePet", nil, kindResource},
		// PATCH is the partial update; when the group also has a PUT, the PUT is
		// consumed as the resource Update and the PATCH is not part of the CRUD
		// group, so it is reclassified as an action rather than scaffolded as an
		// empty resource.
		{"/pets/{id}", "PATCH", "patchPet", nil, kindAction},
		{"/pets/{id}", "DELETE", "deletePet", nil, kindResource},
		{"/pets/{id}/reboot", "POST", "rebootPet", nil, kindAction},
		{"/pets", "DELETE", "deleteAllPets", nil, kindAction},
		{"/credentials/temporary", "POST", "createCredentials", nil, kindEphemeral},
		{"/pets/search", "GET", "searchPets", nil, kindFunction},
		{"/pets/{id}", "POST", "customAction", map[string]any{"x-terraform-action": true}, kindAction},
		{"/sessions", "POST", "createSession", map[string]any{"x-terraform-ephemeral": true}, kindEphemeral},
		{"/items", "GET", "listItems", map[string]any{"x-terraform-list": true}, kindListResource},
		{"/convert", "GET", "convert", map[string]any{"x-terraform-function": true}, kindFunction},
	}
	// pathOps mirrors the case paths so CRUD-create detection (a POST whose
	// collection has an instance subpath stays a resource) behaves as it does
	// on a real spec.
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/pets":                  {transformer.MethodPost: testOp(transformer.MethodPost, "/pets")},
		"/pets/{id}":             {transformer.MethodGet: testOp(transformer.MethodGet, "/pets/{id}"), transformer.MethodPut: testOp(transformer.MethodPut, "/pets/{id}"), transformer.MethodPatch: testOp(transformer.MethodPatch, "/pets/{id}"), transformer.MethodDelete: testOp(transformer.MethodDelete, "/pets/{id}")},
		"/pets/{id}/reboot":      {transformer.MethodPost: testOp(transformer.MethodPost, "/pets/{id}/reboot")},
		"/credentials/temporary": {transformer.MethodPost: testOp(transformer.MethodPost, "/credentials/temporary")},
		"/pets/search":           {transformer.MethodGet: testOp(transformer.MethodGet, "/pets/search")},
		"/items":                 {transformer.MethodGet: testOp(transformer.MethodGet, "/items")},
		"/convert":               {transformer.MethodGet: testOp(transformer.MethodGet, "/convert")},
	}
	for _, tc := range cases {
		t.Run(tc.path+"_"+tc.method, func(t *testing.T) {
			op := &parser.Operation{OperationID: tc.opID, Extensions: tc.extensions}
			got := classifyOperation(tc.path, tc.method, op, pathOps, true)
			if got != tc.want {
				t.Errorf("classifyOperation(%q, %q, %q) = %q, want %q", tc.path, tc.method, tc.opID, got, tc.want)
			}
		})
	}
}

// TestListResource_UniqueItemsWarnsAndFallsBack locks in A1: a list resource
// whose list endpoint response is an array with uniqueItems: true cannot be
// honored — the experimental list/schema package has no Set types, so the
// generator downgrades to List. The transformer surfaces this semantic loss as
// a fail-loud Warning (not an error, so the spec stays Valid), and the list
// resource is still built. A list endpoint whose response array does NOT set
// uniqueItems emits no such warning.
func TestListResource_UniqueItemsWarnsAndFallsBack(t *testing.T) {
	t.Run("uniqueItems true warns", func(t *testing.T) {
		body := []byte(`{
			"openapi": "3.0.1",
			"info": {"title": "List Unique API", "version": "1.0.0"},
			"paths": {
				"/items": {
					"get": {
						"operationId": "listItems",
						"x-terraform-list": true,
						"responses": {
							"200": {
								"description": "ok",
								"content": {
									"application/json": {
										"schema": {
											"type": "array",
											"uniqueItems": true,
											"items": {"type": "string"}
										}
									}
								}
							}
						}
					}
				}
			}
		}`)

		resp := Validate(body)
		if !resp.Valid {
			t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
		}
		if resp.IRPreview == nil || len(resp.IRPreview.ListResources) != 1 {
			t.Fatalf("expected exactly one list resource in the IR preview, got %+v", resp.IRPreview)
		}

		var sawUniqueItemsWarning bool
		for _, d := range resp.Diagnostics {
			if d.Severity == diagnostics.Warning.String() &&
				strings.Contains(d.Summary, "uniqueItems on a list resource is not supported by the list/schema API; falling back to List") {
				sawUniqueItemsWarning = true
				if !strings.Contains(d.Detail, "list/schema") || !strings.Contains(d.Detail, "GET /items") {
					t.Errorf("warning detail should name list/schema and the endpoint, got: %s", d.Detail)
				}
			}
		}
		if !sawUniqueItemsWarning {
			t.Errorf("expected a uniqueItems list-resource warning, got diagnostics: %+v", resp.Diagnostics)
		}
	})

	t.Run("no uniqueItems does not warn", func(t *testing.T) {
		body := []byte(`{
			"openapi": "3.0.1",
			"info": {"title": "List API", "version": "1.0.0"},
			"paths": {
				"/items": {
					"get": {
						"operationId": "listItems",
						"x-terraform-list": true,
						"responses": {
							"200": {
								"description": "ok",
								"content": {
									"application/json": {
										"schema": {
											"type": "array",
											"items": {"type": "string"}
										}
									}
								}
							}
						}
					}
				}
			}
		}`)

		resp := Validate(body)
		for _, d := range resp.Diagnostics {
			if strings.Contains(d.Summary, "uniqueItems on a list resource") {
				t.Errorf("did not expect a uniqueItems warning without uniqueItems, got: %+v", d)
			}
		}
		if resp.IRPreview == nil || len(resp.IRPreview.ListResources) != 1 {
			t.Fatalf("expected exactly one list resource, got %+v", resp.IRPreview)
		}
	})
}

// TestMultipartFormData_MediaTypePlumbedEndToEnd asserts that a Swagger 2.0 spec
// declaring a binary file formData parameter (type: file) surfaces on the
// managed resource's Create mapping as the multipart/form-data media type — the
// A2 transformer plumbing (parser → RequestMediaType → OperationMappingIR.
// MediaType) that the generator dispatches to the multipart body builder.
//
// The per-param formData decomposition (RequestBody content object schema →
// OperationMappingIR.FormDataParams) is a separate, deferred effort
// (REMAINING_GAPS §2: the v2 parser restructures formData params into a
// RequestBody content schema and drops them from op.Parameters, so
// paramsFromOperation does not surface them yet). The generator's multipart
// body builder is exercised at the IR level by TestWiredMultipartFileUpload_*
// in pkg/generator.
func TestMultipartFormData_MediaTypePlumbedEndToEnd(t *testing.T) {
	body := []byte(`{
		"swagger": "2.0",
		"info": {"title": "Upload API", "version": "1.0.0"},
		"paths": {
			"/uploads": {
				"post": {
					"operationId": "createUpload",
					"consumes": ["multipart/form-data"],
					"parameters": [
						{"name": "label", "in": "formData", "required": true, "type": "string"},
						{"name": "file", "in": "formData", "required": true, "type": "file"}
					],
					"responses": {"201": {"description": "created",
						"schema": {"type": "object", "properties": {"id": {"type": "string"}}}
					}}
				},
				"get": {"operationId": "listUploads", "responses": {"200": {"description": "ok"}}}
			},
			"/uploads/{id}": {
				"get": {"operationId": "getUpload",
					"responses": {"200": {"description": "ok",
						"schema": {"type": "object", "properties": {"id": {"type": "string"}}}
				}}},
				"delete": {"operationId": "deleteUpload", "responses": {"204": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil || len(resp.IRPreview.Resources) != 1 {
		t.Fatalf("expected exactly one managed resource, got %+v", resp.IRPreview)
	}
	res := resp.IRPreview.Resources[0]
	if res.CRUDMapping.Create.MediaType != "multipart/form-data" {
		t.Fatalf("Create.MediaType = %q, want %q (A2 media-type plumbing)", res.CRUDMapping.Create.MediaType, "multipart/form-data")
	}
}

func TestValidate_HeuristicAutoDetection(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Heuristic API", "version": "1.0.0"},
		"paths": {
			"/pets": {
				"get": {"operationId": "listPets", "responses": {"200": {"description": "ok"}}},
				"post": {"operationId": "createPet", "responses": {"201": {"description": "created"}}}
			},
			"/pets/{id}/reboot": {
				"post": {"operationId": "rebootPet", "responses": {"200": {"description": "ok"}}}
			},
			"/credentials/temporary": {
				"post": {"operationId": "createCredentials", "responses": {"200": {"description": "ok"}}}
			},
			"/search": {
				"get": {"operationId": "search", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.ListResources) != 0 {
		t.Errorf("expected 0 list resources, got %d", len(resp.IRPreview.ListResources))
	}
	if len(resp.IRPreview.DataSources) != 1 {
		t.Errorf("expected 1 data source, got %d", len(resp.IRPreview.DataSources))
	}
	// createPet (POST /pets) has no paired delete, so the group is not full CRUD
	// and the create is reclassified as an action rather than scaffolded as an
	// empty resource.
	if len(resp.IRPreview.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resp.IRPreview.Resources))
	}
	if len(resp.IRPreview.Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(resp.IRPreview.Actions))
	}
	if len(resp.IRPreview.EphemeralResources) != 1 {
		t.Errorf("expected 1 ephemeral resource, got %d", len(resp.IRPreview.EphemeralResources))
	}
	if len(resp.IRPreview.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(resp.IRPreview.Functions))
	}
}

// TestGroupedImportFormat_SimpleAndComposite covers the §3/#10 import wiring
// for grouped resources: a simple-id resource with a real id attribute is
// importable with a "{id}" format; a composite-id resource is importable only
// when every path parameter is a top-level schema attribute; a resource whose
// identifier is not a real attribute stays non-importable (honest, not silent).
func TestGroupedImportFormat_SimpleAndComposite(t *testing.T) {
	// Simple id: schema has an "id" attribute -> importable as "{id}".
	simple := transformer.ResourceCRUD{
		ID: transformer.IDInfo{Kind: transformer.IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id"},
	}
	simpleSchema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}}}
	got, ok := groupedImportFormat(simple, simpleSchema, "")
	if !ok || got != "{id}" {
		t.Errorf("simple id: got (%q,%v), want ({id},true)", got, ok)
	}

	// Simple id absent from schema -> not importable.
	got, ok = groupedImportFormat(simple, ir.ObjectSchemaIR{}, "pet_id")
	if ok {
		t.Errorf("simple id with no matching attr: got (%q,true), want not importable", got)
	}

	// Composite: both path params present as attributes -> importable as "{namespace}:{name}".
	composite := transformer.ResourceCRUD{
		ID: transformer.IDInfo{Kind: transformer.IDComposite, ParameterNames: []string{"namespace", "name"}},
	}
	compositeSchema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "namespace", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}}
	got, ok = groupedImportFormat(composite, compositeSchema, "")
	if !ok || got != "{namespace}:{name}" {
		t.Errorf("composite with both attrs: got (%q,%v), want ({namespace}:{name},true)", got, ok)
	}

	// Composite missing one path-param attribute -> not importable.
	partialSchema := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}}
	got, ok = groupedImportFormat(composite, partialSchema, "")
	if ok {
		t.Errorf("composite missing namespace attr: got (%q,true), want not importable", got)
	}
}

// TestParamsFromOperation_CookieAndFormData covers the §2/#13 parameter
// categorization: query and header params are surfaced as before, cookie params
// are surfaced on CookieParams (wired as a Cookie header), and formData params
// are surfaced on FormDataParams (kept honestly scaffolded) rather than dropped
// silently.
func TestParamsFromOperation_CookieAndFormData(t *testing.T) {
	op := &parser.Operation{
		Parameters: []parser.Parameter{
			{Name: "limit", In: "query", Schema: &parser.Schema{Type: "integer"}},
			{Name: "X-Trace-Id", In: "header", Schema: &parser.Schema{Type: "string"}},
			{Name: "session", In: "cookie", Schema: &parser.Schema{Type: "string"}},
			{Name: "upload", In: "formData", Schema: &parser.Schema{Type: "string"}},
		},
	}
	query, header, cookie, formData := paramsFromOperation(op)
	if len(query) != 1 || query[0].Name != "limit" {
		t.Errorf("query = %+v, want [limit]", query)
	}
	if len(header) != 1 || header[0].Name != "X-Trace-Id" {
		t.Errorf("header = %+v, want [X-Trace-Id]", header)
	}
	if len(cookie) != 1 || cookie[0].Name != "session" {
		t.Errorf("cookie = %+v, want [session]", cookie)
	}
	if len(formData) != 1 || formData[0].Name != "upload" {
		t.Errorf("formData = %+v, want [upload]", formData)
	}

	// operationMapping surfaces cookie and formData on the IR mapping.
	mapping := operationMapping("GET", "/items/{itemId}", op, "")
	if len(mapping.CookieParams) != 1 || mapping.CookieParams[0].Name != "session" {
		t.Errorf("mapping.CookieParams = %+v, want [session]", mapping.CookieParams)
	}
	if len(mapping.FormDataParams) != 1 || mapping.FormDataParams[0].Name != "upload" {
		t.Errorf("mapping.FormDataParams = %+v, want [upload]", mapping.FormDataParams)
	}
}

// TestLoggingConfig_PlumbedOntoClientIR asserts the generator.yaml logging
// config is translated onto ClientIR.Logging (FilePath→LogFile) so the
// generator can bake it into the provider's Configure-time client.LoggingConfig.
func TestLoggingConfig_PlumbedOntoClientIR(t *testing.T) {
	spec := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Logging API", "version": "1.0.0"},
		"paths": {"/ping": {"get": {"operationId": "ping", "responses": {"200": {"description": "ok"}}}}}
	}`)
	cfg := &config.Config{Logging: &config.LoggingConfig{
		Enabled:               true,
		FilePath:              "provider.log",
		CaptureRequestHeaders: true,
		CaptureResponseBody:   true,
		MaxBodyBytes:          8192,
		RedactHeaders:         []string{"Authorization", "X-API-Key"},
	}}

	preview, _, _, err := BuildProviderIR(spec, cfg)
	if err != nil {
		t.Fatalf("BuildProviderIR() error = %v", err)
	}
	l := preview.ClientIR.Logging
	if l == nil {
		t.Fatal("ClientIR.Logging = nil, want translated logging config")
	}
	if l.LogFile != "provider.log" {
		t.Errorf("LogFile = %q, want %q", l.LogFile, "provider.log")
	}
	if !l.CaptureRequestHeaders || !l.CaptureResponseBody {
		t.Errorf("capture flags = %+v, want request-headers and response-body true", l)
	}
	if l.CaptureRequestBody || l.CaptureResponseHeaders {
		t.Errorf("capture flags = %+v, want request-body and response-headers false", l)
	}
	if l.MaxBodyBytes != 8192 {
		t.Errorf("MaxBodyBytes = %d, want 8192", l.MaxBodyBytes)
	}
	if len(l.RedactHeaders) != 2 || l.RedactHeaders[0] != "Authorization" {
		t.Errorf("RedactHeaders = %v, want [Authorization X-API-Key]", l.RedactHeaders)
	}
}

// TestLoggingConfig_DisabledDropsFilePath asserts a disabled logging config
// bakes no default log file: logging is enabled iff LogFile is non-empty,
// matching the generated client's New guard.
func TestLoggingConfig_DisabledDropsFilePath(t *testing.T) {
	spec := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Logging API", "version": "1.0.0"},
		"paths": {"/ping": {"get": {"operationId": "ping", "responses": {"200": {"description": "ok"}}}}}
	}`)
	cfg := &config.Config{Logging: &config.LoggingConfig{
		Enabled:  false,
		FilePath: "provider.log",
	}}

	preview, _, _, err := BuildProviderIR(spec, cfg)
	if err != nil {
		t.Fatalf("BuildProviderIR() error = %v", err)
	}
	l := preview.ClientIR.Logging
	if l == nil {
		t.Fatal("ClientIR.Logging = nil, want translated logging config")
	}
	if l.LogFile != "" {
		t.Errorf("LogFile = %q, want empty when logging is disabled", l.LogFile)
	}
}

// TestLoggingConfig_AbsentLeavesLoggingNil asserts no logging config leaves
// ClientIR.Logging nil so no defaults are baked.
func TestLoggingConfig_AbsentLeavesLoggingNil(t *testing.T) {
	spec := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Logging API", "version": "1.0.0"},
		"paths": {"/ping": {"get": {"operationId": "ping", "responses": {"200": {"description": "ok"}}}}}
	}`)

	preview, _, _, err := BuildProviderIR(spec, nil)
	if err != nil {
		t.Fatalf("BuildProviderIR() error = %v", err)
	}
	if preview.ClientIR.Logging != nil {
		t.Errorf("ClientIR.Logging = %+v, want nil without a logging config", preview.ClientIR.Logging)
	}
}

// TestBuildProviderIR_NonLocalRefFailsLoud asserts the generate path runs the
// same $ref validation as the HTTP /validate endpoint (parser.Validate): a
// non-local $ref — e.g. a bundled spec's sibling schema file that was never
// fetched — must surface as an error diagnostic instead of being silently
// dropped into an empty schema. The converter records the ref string but never
// resolves it; only Validate rejects non-local references (fail-loud, not
// dropped-silently).
func TestBuildProviderIR_NonLocalRefFailsLoud(t *testing.T) {
	spec := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Bundled API", "version": "1.0.0"},
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"requestBody": {"content": {"application/json": {"schema": {"$ref": "source/schemas/pet.json#/definitions/Pet"}}}},
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	_, _, diags, err := BuildProviderIR(spec, nil)
	if err != nil {
		t.Fatalf("BuildProviderIR() error = %v", err)
	}
	if !diags.HasErrors() {
		t.Fatal("BuildProviderIR() diagnostics have no errors, want Non-local $ref error")
	}
	for _, d := range diags {
		if d.Severity == diagnostics.Error && d.Summary == "Non-local $ref" {
			return
		}
	}
	t.Errorf("BuildProviderIR() diagnostics = %v, want a Non-local $ref error", diags)
}

// TestClassifyOperation_ActionFromVerbOperationId locks in the G unification: a
// POST whose trailing path segment is not a recognized verb but whose
// operationId leads with one classifies as an action, and a POST that is not a
// managed-resource Create (no instance subpath) classifies as an action
// (mirroring transformer.InferActions), while a CRUD-create POST stays a
// resource.
func TestClassifyOperation_ActionFromVerbOperationId(t *testing.T) {
	pathOps := map[string]map[transformer.HTTPMethod]transformer.Operation{
		"/servers":      {transformer.MethodPost: testOp(transformer.MethodPost, "/servers")},
		"/servers/{id}": {transformer.MethodGet: testOp(transformer.MethodGet, "/servers/{id}"), transformer.MethodPost: testOp(transformer.MethodPost, "/servers/{id}"), transformer.MethodDelete: testOp(transformer.MethodDelete, "/servers/{id}")},
		"/uploads":      {transformer.MethodPost: testOp(transformer.MethodPost, "/uploads")},
		"/widgets":      {transformer.MethodPost: testOp(transformer.MethodPost, "/widgets")},
		"/widgets/{id}": {transformer.MethodGet: testOp(transformer.MethodGet, "/widgets/{id}")},
	}
	cases := []struct {
		name       string
		path, opID string
		want       operationKind
	}{
		{"verb operationId on instance path", "/servers/{id}", "rebootServer", kindAction},
		{"verb operationId with snake_case", "/servers/{id}", "restart_server", kindAction},
		{"non-verb operationId, not a CRUD create", "/uploads", "uploadFile", kindAction},
		{"non-verb operationId, not a CRUD create on instance path", "/servers/{id}", "updateServer", kindAction},
		{"CRUD create stays a resource", "/servers", "createServer", kindResource},
		{"CRUD create without full CRUD is an action", "/widgets", "createWidget", kindAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &parser.Operation{OperationID: tc.opID}
			if got := classifyOperation(tc.path, "POST", op, pathOps, true); got != tc.want {
				t.Errorf("classifyOperation(%q, POST, %q) = %q, want %q", tc.path, tc.opID, got, tc.want)
			}
		})
	}
}

// TestActionFromOperation_RequestBodySurfacesConfigSchema locks in that a
// body-bearing action's config schema includes the request-body properties, so
// a scaffolded action (the client cannot send its body) still presents its
// intended inputs to the practitioner instead of an empty schema. The register
// action on the SpaceTraders spec is the motivating case: without this, it
// would expose no symbol/faction inputs at all.
func TestActionFromOperation_RequestBodySurfacesConfigSchema(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Register API", "version": "1.0.0"},
		"components": {
			"schemas": {
				"FactionSymbol": {"type": "string", "enum": ["COSMIC", "VOID"]}
			}
		},
		"paths": {
			"/register": {
				"post": {
					"operationId": "register",
					"requestBody": {
						"required": true,
						"content": {
							"application/json": {
								"schema": {
									"type": "object",
									"required": ["symbol", "faction"],
									"properties": {
										"symbol": {"type": "string", "minLength": 3, "maxLength": 14},
										"faction": {"$ref": "#/components/schemas/FactionSymbol"}
									}
								}
							}
						}
					},
					"responses": {"201": {"description": "created"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(resp.IRPreview.Actions))
	}
	action := resp.IRPreview.Actions[0]
	if action.Name != "register" {
		t.Errorf("expected action register, got %q", action.Name)
	}
	attrs := make(map[string]ir.AttributeIR, len(action.ConfigSchema.Attributes))
	for _, a := range action.ConfigSchema.Attributes {
		attrs[a.Name] = a
	}
	for _, want := range []struct {
		name     string
		typ      ir.PrimitiveType
		required bool
	}{
		{"symbol", ir.TypeString, true},
		{"faction", ir.TypeString, true},
	} {
		a, ok := attrs[want.name]
		if !ok {
			t.Errorf("expected config attribute %q, got %v", want.name, attrs)
			continue
		}
		if a.Schema.Type != want.typ {
			t.Errorf("attribute %q type = %q, want %q", want.name, a.Schema.Type, want.typ)
		}
		if a.Required != want.required {
			t.Errorf("attribute %q Required = %v, want %v", want.name, a.Required, want.required)
		}
	}
}

// TestEphemeralFromOperation_RenewCloseFromPath locks in the G unification: an
// ephemeral resource's Renew/Close mappings point at the sibling lifecycle
// operations the spec declares (POST <path>/renew, DELETE <path>/close), never
// at the open operation as a keyword-guessed placeholder.
func TestEphemeralFromOperation_RenewCloseFromPath(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Lease API", "version": "1.0.0"},
		"paths": {
			"/sessions": {
				"post": {"operationId": "openSession", "responses": {"201": {"description": "opened"}}}
			},
			"/sessions/renew": {
				"post": {"operationId": "renewSession", "responses": {"200": {"description": "renewed"}}}
			},
			"/sessions/close": {
				"delete": {"operationId": "closeSession", "responses": {"204": {"description": "closed"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.EphemeralResources) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(resp.IRPreview.EphemeralResources))
	}
	er := resp.IRPreview.EphemeralResources[0]
	if !er.HasRenew || er.RenewMapping == nil {
		t.Fatal("expected Renew mapping from /sessions/renew")
	}
	if er.RenewMapping.Method != http.MethodPost || er.RenewMapping.PathTemplate != "/sessions/renew" {
		t.Errorf("RenewMapping = %+v, want POST /sessions/renew", er.RenewMapping)
	}
	if !er.HasClose || er.CloseMapping == nil {
		t.Fatal("expected Close mapping from /sessions/close")
	}
	if er.CloseMapping.Method != http.MethodDelete || er.CloseMapping.PathTemplate != "/sessions/close" {
		t.Errorf("CloseMapping = %+v, want DELETE /sessions/close", er.CloseMapping)
	}
}

// TestEphemeralFromOperation_NoLifecycleOpsLeavesMappingsUnset asserts the
// counterpart: without sibling renew/close operations the mappings stay unset
// rather than pointing at the open operation.
func TestEphemeralFromOperation_NoLifecycleOpsLeavesMappingsUnset(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Token API", "version": "1.0.0"},
		"paths": {
			"/tokens": {
				"post": {"operationId": "createToken", "responses": {"201": {"description": "created"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.EphemeralResources) != 1 {
		t.Fatalf("expected 1 ephemeral resource, got %d", len(resp.IRPreview.EphemeralResources))
	}
	er := resp.IRPreview.EphemeralResources[0]
	if er.HasRenew || er.RenewMapping != nil {
		t.Errorf("expected no Renew mapping without /tokens/renew, got %+v", er.RenewMapping)
	}
	if er.HasClose || er.CloseMapping != nil {
		t.Errorf("expected no Close mapping without /tokens/close, got %+v", er.CloseMapping)
	}
}

// TestInferListResources_PromotesCollectionGet locks in the G promotion: a
// collection GET paired with an instance Read keeps its data source (additive,
// existing wiring is not broken) and additionally yields a list resource.
func TestInferListResources_PromotesCollectionGet(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Widget API", "version": "1.0.0"},
		"paths": {
			"/widgets": {
				"get": {
					"operationId": "listWidgets",
					"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object", "properties": {"id": {"type": "string"}}}}}}}}
				}
			},
			"/widgets/{id}": {
				"get": {"operationId": "getWidget", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.ListResources) != 1 {
		t.Fatalf("expected 1 promoted list resource, got %d", len(resp.IRPreview.ListResources))
	}
	lr := resp.IRPreview.ListResources[0]
	if lr.ListMapping.Method != http.MethodGet || lr.ListMapping.PathTemplate != "/widgets" {
		t.Errorf("ListMapping = %+v, want GET /widgets", lr.ListMapping)
	}
	// Additive: the collection GET keeps its data source form as well.
	var foundDataSource bool
	for _, ds := range resp.IRPreview.DataSources {
		if ds.ReadMapping.Method == http.MethodGet && ds.ReadMapping.PathTemplate == "/widgets" {
			foundDataSource = true
		}
	}
	if !foundDataSource {
		t.Errorf("expected the collection GET data source to be kept (additive promotion), got %+v", resp.IRPreview.DataSources)
	}
}

// TestFunctionFromOperation_InfersSignature locks in the G function-signature
// inference: arguments come from the operation's parameters and the return
// type from the response schema (flat object of primitives here).
func TestFunctionFromOperation_InfersSignature(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Search API", "version": "1.0.0"},
		"paths": {
			"/search": {
				"get": {
					"operationId": "searchRecords",
					"parameters": [
						{"name": "q", "in": "query", "required": true, "schema": {"type": "string"}},
						{"name": "limit", "in": "query", "schema": {"type": "integer"}}
					],
					"responses": {
						"200": {
							"description": "ok",
							"content": {"application/json": {"schema": {
								"type": "object",
								"properties": {
									"answer": {"type": "string"},
									"score": {"type": "number"}
								}
							}}}
						}
					}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(resp.IRPreview.Functions))
	}
	fn := resp.IRPreview.Functions[0]
	if len(fn.Arguments) != 2 {
		t.Fatalf("expected 2 inferred arguments, got %+v", fn.Arguments)
	}
	if fn.Arguments[0].Name != "q" || fn.Arguments[0].Schema.Type != ir.TypeString {
		t.Errorf("argument[0] = %+v, want q (string)", fn.Arguments[0])
	}
	if fn.Arguments[1].Name != "limit" || fn.Arguments[1].Schema.Type != ir.TypeInt {
		t.Errorf("argument[1] = %+v, want limit (int)", fn.Arguments[1])
	}
	if len(fn.ReturnType.Attributes) != 2 {
		t.Fatalf("expected 2 return attributes, got %+v", fn.ReturnType)
	}
	got := map[string]ir.PrimitiveType{}
	for _, a := range fn.ReturnType.Attributes {
		got[a.Name] = a.Schema.Type
	}
	if got["answer"] != ir.TypeString || got["score"] != ir.TypeFloat {
		t.Errorf("return attributes = %v, want answer:string score:float", got)
	}
}

// TestFunctionFromOperation_ComplexResponseFallsBackDynamic asserts a response
// shape the function generator cannot express (array of objects) yields a
// Dynamic return type rather than a guessed one.
func TestFunctionFromOperation_ComplexResponseFallsBackDynamic(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Search API", "version": "1.0.0"},
		"paths": {
			"/search": {
				"get": {
					"operationId": "searchRecords",
					"responses": {
						"200": {
							"description": "ok",
							"content": {"application/json": {"schema": {
								"type": "array",
								"items": {"type": "object", "properties": {"id": {"type": "string"}}}
							}}}
						}
					}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(resp.IRPreview.Functions))
	}
	if got := resp.IRPreview.Functions[0].ReturnType.Type; got != ir.TypeDynamic {
		t.Errorf("ReturnType.Type = %q, want %q for an unmappable response shape", got, ir.TypeDynamic)
	}
}

// TestFunctionOverride_MergesArgumentsByName asserts that a function_override
// whose declared arguments redeclare an inferred function's parameters MERGES
// by name instead of appending duplicates. A redeclared argument replaces the
// inferred type (and preserves the inferred description/wire name); an
// argument with no inferred counterpart is appended. Blindly appending would
// duplicate the signature and surface "Parameter names must be unique" errors
// at provider load.
func TestFunctionOverride_MergesArgumentsByName(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Search API", "version": "1.0.0"},
		"config": "provider:\n  name: search_provider\n  version: \"1.0.0\"\nfunction_overrides:\n  - operation: searchRecords\n    name: search\n    arguments:\n      - name: q\n        type: string\n      - name: limit\n        type: string\n      - name: extra\n        type: boolean\n",
		"paths": {
			"/search": {
				"get": {
					"operationId": "searchRecords",
					"parameters": [
						{"name": "q", "in": "query", "required": true, "description": "The search query.", "schema": {"type": "string"}},
						{"name": "limit", "in": "query", "description": "Max results.", "schema": {"type": "integer"}}
					],
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if len(resp.IRPreview.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(resp.IRPreview.Functions))
	}
	fn := resp.IRPreview.Functions[0]
	if fn.Name != "search" {
		t.Errorf("function name = %q, want %q (override rename)", fn.Name, "search")
	}
	// Two inferred args (q, limit) plus one override-only arg (extra) => 3, not 5.
	if len(fn.Arguments) != 3 {
		t.Fatalf("expected 3 merged arguments (no duplicates), got %d: %+v", len(fn.Arguments), fn.Arguments)
	}
	byName := map[string]ir.AttributeIR{}
	for _, a := range fn.Arguments {
		if _, dup := byName[a.Name]; dup {
			t.Errorf("duplicate argument name %q in merged signature: %+v", a.Name, fn.Arguments)
		}
		byName[a.Name] = a
	}
	// The redeclared "limit" argument takes the override's type (string), not the
	// inferred integer — the override corrects the type.
	if byName["limit"].Schema.Type != ir.TypeString {
		t.Errorf("limit type = %q, want %q (override replaces inferred type)", byName["limit"].Schema.Type, ir.TypeString)
	}
	// The inferred description is preserved for a redeclared argument (the
	// override carries no description).
	if byName["q"].Description != "The search query." {
		t.Errorf("q description = %q, want inferred description preserved", byName["q"].Description)
	}
	// The override-only argument is appended with the declared type.
	if byName["extra"].Schema.Type != ir.TypeBool {
		t.Errorf("extra type = %q, want %q (override-only argument appended)", byName["extra"].Schema.Type, ir.TypeBool)
	}
}

// TestActionFromOverride_PreflightMappings asserts the F3 override surface:
// modify_plan_operation / validate_config_operation parse into the action's
// preflight mappings, and declaring modify_plan_operation implies the
// ModifyPlan method.
func TestActionFromOverride_PreflightMappings(t *testing.T) {
	a := actionFromOverride(config.ActionOverride{
		Operation:               "rebootServer",
		ModifyPlanOperation:     "POST /servers/{server_id}/reboot/preview",
		ValidateConfigOperation: "post /servers/{server_id}/reboot/validate",
	}, "mycloud")

	if !a.ModifyPlan {
		t.Error("ModifyPlan = false, want true when modify_plan_operation is declared")
	}
	if a.ModifyPlanMapping == nil {
		t.Fatal("ModifyPlanMapping = nil, want parsed mapping")
	}
	if a.ModifyPlanMapping.Method != http.MethodPost || a.ModifyPlanMapping.PathTemplate != "/servers/{server_id}/reboot/preview" {
		t.Errorf("ModifyPlanMapping = %+v, want POST /servers/{server_id}/reboot/preview", a.ModifyPlanMapping)
	}
	if a.ValidateConfigMapping == nil {
		t.Fatal("ValidateConfigMapping = nil, want parsed mapping")
	}
	if a.ValidateConfigMapping.Method != http.MethodPost {
		t.Errorf("ValidateConfigMapping.Method = %q, want POST (normalized upper-case)", a.ValidateConfigMapping.Method)
	}

	// No mappings declared: both stay unset and ModifyPlan follows the flag.
	b := actionFromOverride(config.ActionOverride{Operation: "rebootServer"}, "mycloud")
	if b.ModifyPlanMapping != nil || b.ValidateConfigMapping != nil {
		t.Errorf("mappings = %+v / %+v, want nil without override operations", b.ModifyPlanMapping, b.ValidateConfigMapping)
	}
	if b.ModifyPlan {
		t.Error("ModifyPlan = true, want false without modify_plan or modify_plan_operation")
	}
}

// TestNestedOneOf_StillWarns locks in the D1 boundary: a oneOf/anyOf
// composition nested inside an object property still emits the fail-loud
// "composition not modeled" warning (the flat attribute model cannot switch on
// alternatives there), while a top-level oneOf response no longer warns — it
// is wired through to the IR as a union instead.
func TestNestedOneOf_StillWarns(t *testing.T) {
	body := []byte(`{
		"openapi": "3.1.0",
		"info": {"title": "Nested Poly API", "version": "1.0.0"},
		"paths": {
			"/owners/{id}": {
				"get": {
					"operationId": "getOwner",
					"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"pet": {"oneOf": [
								{"type": "object", "properties": {"lives": {"type": "integer"}}},
								{"type": "object", "properties": {"bark": {"type": "integer"}}}
							]}
						}
					}}}}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
	}
	var sawNestedWarning bool
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "oneOf composition not modeled") {
			sawNestedWarning = true
		}
	}
	if !sawNestedWarning {
		t.Errorf("expected a nested oneOf composition warning, got diagnostics: %+v", resp.Diagnostics)
	}
}

// TestTopLevelOneOf_NoWarning asserts the counterpart: a top-level oneOf
// response is captured as a union (no warning) and surfaces as a Computed
// wrapper attribute named for the referenced schema.
func TestTopLevelOneOf_NoWarning(t *testing.T) {
	body := []byte(`{
		"openapi": "3.1.0",
		"info": {"title": "Poly API", "version": "1.0.0"},
		"paths": {
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
						"$ref": "#/components/schemas/Pet"
					}}}}}
				}
			}
		},
		"components": {"schemas": {
			"Pet": {
				"oneOf": [
					{"type": "object", "properties": {"lives": {"type": "integer"}}},
					{"type": "object", "properties": {"bark": {"type": "integer"}}}
				],
				"discriminator": {"propertyName": "petType"}
			}
		}}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "composition not modeled") {
			t.Errorf("top-level oneOf must not warn (it is wired as a union), got: %+v", d)
		}
	}
	if len(resp.IRPreview.DataSources) != 1 {
		t.Fatalf("expected 1 data source, got %d", len(resp.IRPreview.DataSources))
	}
	ds := resp.IRPreview.DataSources[0]
	var wrapper *ir.AttributeIR
	for i := range ds.Schema.Attributes {
		if ds.Schema.Attributes[i].Name == "pet" {
			wrapper = &ds.Schema.Attributes[i]
		}
	}
	if wrapper == nil {
		t.Fatalf("expected a 'pet' wrapper attribute (named for the $ref), got %+v", ds.Schema.Attributes)
	}
	if wrapper.Schema.Union == nil || wrapper.Schema.Union.Discriminator == nil {
		t.Errorf("wrapper must carry the union + discriminator, got %+v", wrapper.Schema)
	}
	if !wrapper.Computed {
		t.Errorf("wrapper must be Computed (output shape)")
	}
}

// TestValidate_ResourceOverrideCreatesResourceFromAction verifies G8: a
// resource_override with generate_resource and explicit CRUD operations
// promotes an action (whose create path differs from its read/delete path) to a
// managed resource, and the action is consumed (not double-emitted).
func TestValidate_ResourceOverrideCreatesResourceFromAction(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Dash API", "version": "1.0.0"},
		"config": "provider:\n  name: dash\n  version: \"1.0.0\"\nresource_overrides:\n  - operation: postDashboard\n    resource_name: dashboard\n    id_attribute: uid\n    generate_resource: true\n    create_operation: postDashboard\n    read_operation: getDashboardByUID\n    delete_operation: deleteDashboardByUID\n",
		"paths": {
			"/dashboards/db": {
				"post": {
					"operationId": "postDashboard",
					"requestBody": {"content": {"application/json": {"schema": {"type": "object", "properties": {"dashboard": {"type": "object"}, "overwrite": {"type": "boolean"}}}}}},
					"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "properties": {"uid": {"type": "string"}}}}}}}
				}
			},
			"/dashboards/uid/{uid}": {
				"get": {
					"operationId": "getDashboardByUID",
					"parameters": [{"name": "uid", "in": "path", "required": true, "schema": {"type": "string"}}],
					"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "properties": {"uid": {"type": "string"}, "title": {"type": "string"}}}}}}}
				},
				"delete": {
					"operationId": "deleteDashboardByUID",
					"parameters": [{"name": "uid", "in": "path", "required": true, "schema": {"type": "string"}}],
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("no IR preview")
	}

	var dash *ir.ResourceIR
	for i := range resp.IRPreview.Resources {
		if resp.IRPreview.Resources[i].Name == "dashboard" {
			dash = &resp.IRPreview.Resources[i]
			break
		}
	}
	if dash == nil {
		t.Fatalf("expected a dashboard resource, got %+v", resp.IRPreview.Resources)
	}
	if dash.CRUDMapping.Create.PathTemplate != "/dashboards/db" {
		t.Errorf("expected create POST /dashboards/db, got %+v", dash.CRUDMapping.Create)
	}
	if dash.CRUDMapping.Read.PathTemplate != "/dashboards/uid/{uid}" {
		t.Errorf("expected read GET /dashboards/uid/{uid}, got %+v", dash.CRUDMapping.Read)
	}
	if dash.CRUDMapping.Delete.PathTemplate != "/dashboards/uid/{uid}" {
		t.Errorf("expected delete DELETE /dashboards/uid/{uid}, got %+v", dash.CRUDMapping.Delete)
	}

	// The postDashboard action must be consumed (not emitted as an action).
	for _, a := range resp.IRPreview.Actions {
		if a.Name == "post_dashboard" {
			t.Errorf("postDashboard should be consumed by the override-created resource, but an action was emitted")
		}
	}
}

// TestValidate_SecuritySchemeSelection verifies G6: setting generator.yaml
// `security.scheme` restricts the provider to that scheme (suppressing the OR
// warning and dropping the other scheme's auth attributes).
func TestValidate_SecuritySchemeSelection(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Sec API", "version": "1.0.0"},
		"config": "provider:\n  name: sec\n  version: \"1.0.0\"\nsecurity:\n  scheme: basic\n",
		"components": {
			"securitySchemes": {
				"basic": {"type": "http", "scheme": "basic"},
				"api_key": {"type": "apiKey", "name": "Authorization", "in": "header"}
			}
		},
		"security": [{"basic": []}, {"api_key": []}],
		"paths": {
			"/pets": {
				"get": {"operationId": "listPets", "responses": {"200": {"description": "ok"}}}
			}
		}
	}`)

	resp := Validate(body)
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("no IR preview")
	}
	// Only the basic scheme should remain.
	if len(resp.IRPreview.SecurityIR.Schemes) != 1 || resp.IRPreview.SecurityIR.Schemes[0].Name != "basic" {
		t.Errorf("expected only the basic scheme, got %+v", resp.IRPreview.SecurityIR.Schemes)
	}
	// The OR warning must be suppressed.
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary, "OR security") {
			t.Errorf("expected OR security warning to be suppressed when a scheme is selected, got %+v", d)
		}
	}
}
