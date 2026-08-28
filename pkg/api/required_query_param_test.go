package api

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// envelopedCollectionSpec is a minimal OpenAPI document for the generic
// pattern: a required query parameter on both the collection GET and the
// create POST, a collection response that is an enveloped array
// ({maps: [...], context: {...}}), and a body property of the same name that
// is readOnly and not in the body's required list. Any spec with this shape
// is affected, not a particular vendor.
func envelopedCollectionSpec() []byte {
	return []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Cluster Maps API", "version": "1.0.0"},
		"paths": {
			"/maps": {
				"get": {
					"operationId": "loadAllMaps",
					"parameters": [
						{"name": "clusterId", "in": "query", "required": true, "schema": {"type": "string"}, "description": "id of the defining cluster"},
						{"name": "page", "in": "query", "schema": {"type": "integer"}},
						{"name": "sort", "in": "query", "schema": {"type": "string"}}
					],
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {
										"type": "object",
										"properties": {
											"maps": {
												"type": "array",
												"items": {
													"type": "object",
													"properties": {
														"id": {"type": "string"},
														"name": {"type": "string"},
														"clusterId": {"type": "string"}
													}
												}
											},
											"context": {
												"type": "object",
												"properties": {
													"offset": {"type": "integer"}
												}
											}
										}
									}
								}
							}
						}
					}
				},
				"post": {
					"operationId": "createMap",
					"parameters": [
						{"name": "clusterId", "in": "query", "required": true, "schema": {"type": "string"}, "description": "id of the defining cluster"}
					],
					"requestBody": {
						"content": {
							"application/json": {
								"schema": {
									"type": "object",
									"required": ["name"],
									"properties": {
										"name": {"type": "string"},
										"clusterId": {"type": "string", "readOnly": true, "description": "id of the defining cluster"}
									}
								}
							}
						}
					},
					"responses": {"201": {"description": "created"}}
				}
			},
			"/maps/{id}": {
				"get": {
					"operationId": "getMap",
					"parameters": [
						{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
					],
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {
										"type": "object",
										"properties": {
											"id": {"type": "string"},
											"name": {"type": "string"},
											"clusterId": {"type": "string", "readOnly": true}
										}
									}
								}
							}
						}
					}
				},
				"put": {
					"operationId": "updateMap",
					"parameters": [
						{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
						{"name": "clusterId", "in": "query", "required": true, "schema": {"type": "string"}}
					],
					"requestBody": {
						"content": {
							"application/json": {
								"schema": {
									"type": "object",
									"properties": {
										"name": {"type": "string"},
										"clusterId": {"type": "string", "readOnly": true}
									}
								}
							}
						}
					},
					"responses": {"200": {"description": "ok"}}
				},
				"delete": {
					"operationId": "deleteMap",
					"parameters": [
						{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
					],
					"responses": {"204": {"description": "deleted"}}
				}
			}
		}
	}`)
}

func attrByName(attrs []ir.AttributeIR, name string) (ir.AttributeIR, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return ir.AttributeIR{}, false
}

func findListByName(t *testing.T, lists []ir.ListResourceIR, name string) ir.ListResourceIR {
	t.Helper()
	for _, lr := range lists {
		if lr.Name == name {
			return lr
		}
	}
	var names []string
	for _, lr := range lists {
		names = append(names, lr.Name)
	}
	t.Fatalf("list resource %q not found, have %v", name, names)
	return ir.ListResourceIR{}
}

func findResourceByName(t *testing.T, resources []ir.ResourceIR, name string) ir.ResourceIR {
	t.Helper()
	for _, r := range resources {
		if r.Name == name {
			return r
		}
	}
	var names []string
	for _, r := range resources {
		names = append(names, r.Name)
	}
	t.Fatalf("resource %q not found, have %v", name, names)
	return ir.ResourceIR{}
}

// TestListResource_EnvelopedCollectionKeepsRequiredQueryParam locks in that a
// collection GET whose response is {maps: [...], context: {...}} must still
// surface the required clusterId query parameter on the list resource config
// schema, populate the item resource/identity schemas, and record the envelope
// key so the generated List body can unwrap the page.
func TestListResource_EnvelopedCollectionKeepsRequiredQueryParam(t *testing.T) {
	resp := Validate(envelopedCollectionSpec())
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}

	lr := findListByName(t, resp.IRPreview.ListResources, "load_all_maps")

	clusterID, ok := attrByName(lr.ConfigSchema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("list ConfigSchema dropped required clusterId: %+v", lr.ConfigSchema.Attributes)
	}
	if !clusterID.Required || clusterID.Optional {
		t.Errorf("cluster_id must be Required, got Required=%v Optional=%v", clusterID.Required, clusterID.Optional)
	}

	page, ok := attrByName(lr.ConfigSchema.Attributes, "page")
	if !ok {
		t.Fatalf("list ConfigSchema dropped optional page: %+v", lr.ConfigSchema.Attributes)
	}
	if !page.Optional || page.Required {
		t.Errorf("page must be Optional, got Required=%v Optional=%v", page.Required, page.Optional)
	}

	if _, ok := attrByName(lr.ConfigSchema.Attributes, "sort"); !ok {
		t.Errorf("list ConfigSchema dropped optional sort: %+v", lr.ConfigSchema.Attributes)
	}

	if lr.ListMapping.ResponseEnvelope != "maps" {
		t.Errorf("ResponseEnvelope = %q, want %q so the generated List unwraps the collection", lr.ListMapping.ResponseEnvelope, "maps")
	}

	if lr.ResourceSchema == nil || len(lr.ResourceSchema.Attributes) == 0 {
		t.Fatalf("ResourceSchema is empty; enveloped array was not unwrapped: %+v", lr.ResourceSchema)
	}
	if _, ok := attrByName(lr.ResourceSchema.Attributes, "name"); !ok {
		t.Errorf("ResourceSchema missing item property name: %+v", lr.ResourceSchema.Attributes)
	}

	if len(lr.IdentitySchema.Attributes) == 0 {
		t.Fatal("IdentitySchema is empty; list cannot wire without identity")
	}

	cfg, err := generator.GenerateConfig(*resp.IRPreview)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	found := false
	for _, lo := range cfg.ListResourceOverrides {
		for _, a := range lo.ConfigSchema {
			if a.Name == "cluster_id" {
				found = true
				if a.Optional != nil && *a.Optional {
					t.Errorf("generate-config listed cluster_id as optional, want required")
				}
			}
		}
	}
	if !found {
		t.Errorf("generate-config dropped cluster_id from list_resource_overrides config_schema: %+v", cfg.ListResourceOverrides)
	}
}

// TestListResource_NonArrayResponseStillSurfacesConfigSchema locks in that
// ConfigSchema is built from the collection operation's parameters even when
// the response is not a bare array and is not an unwrapable collection
// envelope. Required query params must not vanish just because the list stays
// honestly scaffolded.
func TestListResource_NonArrayResponseStillSurfacesConfigSchema(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Status API", "version": "1.0.0"},
		"paths": {
			"/status": {
				"get": {
					"operationId": "listStatus",
					"x-terraform-list": true,
					"parameters": [
						{"name": "clusterId", "in": "query", "required": true, "schema": {"type": "string"}},
						{"name": "page", "in": "query", "schema": {"type": "integer"}}
					],
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {
										"type": "object",
										"properties": {
											"status": {"type": "string"},
											"count": {"type": "integer"}
										}
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
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil || len(resp.IRPreview.ListResources) != 1 {
		t.Fatalf("expected exactly one list resource, got %+v", resp.IRPreview)
	}
	lr := resp.IRPreview.ListResources[0]

	clusterID, ok := attrByName(lr.ConfigSchema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("non-array list dropped required clusterId: %+v", lr.ConfigSchema.Attributes)
	}
	if !clusterID.Required {
		t.Errorf("cluster_id must be Required, got %+v", clusterID)
	}
	if _, ok := attrByName(lr.ConfigSchema.Attributes, "page"); !ok {
		t.Errorf("non-array list dropped optional page: %+v", lr.ConfigSchema.Attributes)
	}

	// The response is not a collection, so identity/resource stay empty and
	// the list remains an honest scaffold — but the filters must still exist.
	if lr.ResourceSchema != nil && len(lr.ResourceSchema.Attributes) > 0 {
		t.Errorf("non-collection response should not populate ResourceSchema, got %+v", lr.ResourceSchema)
	}
}

// TestListResource_SinglePropertyEnvelopeStillWires is the control case: a
// single-property envelope ({counters: [...]}) already unwrapped, and must
// keep required clusterId plus identity.
func TestListResource_SinglePropertyEnvelopeStillWires(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "PTP API", "version": "1.0.0"},
		"paths": {
			"/ptp/counters": {
				"get": {
					"operationId": "getAllPtpCounters",
					"x-terraform-list": true,
					"parameters": [
						{"name": "clusterId", "in": "query", "required": true, "schema": {"type": "string"}}
					],
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {
										"type": "object",
										"properties": {
											"counters": {
												"type": "array",
												"items": {
													"type": "object",
													"properties": {
														"id": {"type": "string"},
														"value": {"type": "integer"}
													}
												}
											}
										}
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
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil || len(resp.IRPreview.ListResources) != 1 {
		t.Fatalf("expected exactly one list resource, got %+v", resp.IRPreview)
	}
	lr := resp.IRPreview.ListResources[0]
	clusterID, ok := attrByName(lr.ConfigSchema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("single-property envelope list dropped clusterId: %+v", lr.ConfigSchema.Attributes)
	}
	if !clusterID.Required {
		t.Errorf("cluster_id must be Required, got %+v", clusterID)
	}
	if lr.ListMapping.ResponseEnvelope != "counters" {
		t.Errorf("ResponseEnvelope = %q, want %q", lr.ListMapping.ResponseEnvelope, "counters")
	}
	if len(lr.IdentitySchema.Attributes) == 0 {
		t.Fatal("IdentitySchema is empty; single-property envelope should still derive identity from item id")
	}
}

// TestGroupedResource_RequiredQueryParamIsRequired locks in that cluster_id
// on the inferred map resource must be Required (the create/update query
// param is required: true), not Optional+Computed.
func TestGroupedResource_RequiredQueryParamIsRequired(t *testing.T) {
	resp := Validate(envelopedCollectionSpec())
	if !resp.Valid {
		t.Fatalf("expected valid response, got diagnostics: %+v", resp.Diagnostics)
	}
	if resp.IRPreview == nil {
		t.Fatal("expected IR preview")
	}

	res := findResourceByName(t, resp.IRPreview.Resources, "map")
	clusterID, ok := attrByName(res.Schema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("managed resource dropped cluster_id: %+v", res.Schema.Attributes)
	}
	if !clusterID.Required || clusterID.Optional || clusterID.Computed {
		t.Errorf("cluster_id must be Required, got Required=%v Optional=%v Computed=%v",
			clusterID.Required, clusterID.Optional, clusterID.Computed)
	}

	cfg, err := generator.GenerateConfig(*resp.IRPreview)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	for _, ro := range cfg.ResourceOverrides {
		for _, name := range ro.ComputedAttributes {
			if name == "cluster_id" {
				t.Errorf("generate-config listed cluster_id in computed_attributes for %s: %v",
					ro.ResourceName, ro.ComputedAttributes)
			}
		}
	}
}

// TestOperations_RequiredReadOnlyPropertyWarnsThroughValidate asserts the
// fail-loud diagnostic is visible on the validate/generate path, not only
// the transformer unit test.
func TestOperations_RequiredReadOnlyPropertyWarnsThroughValidate(t *testing.T) {
	body := []byte(`{
		"openapi": "3.0.1",
		"info": {"title": "Icap API", "version": "1.0.0"},
		"paths": {
			"/icap": {
				"post": {
					"operationId": "createIcap",
					"requestBody": {
						"content": {
							"application/json": {
								"schema": {
									"type": "object",
									"required": ["clusterId", "name"],
									"properties": {
										"clusterId": {"type": "string", "readOnly": true},
										"name": {"type": "string"}
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
		t.Fatalf("expected valid response (warning, not error), got diagnostics: %+v", resp.Diagnostics)
	}
	var saw bool
	for _, d := range resp.Diagnostics {
		if d.Severity == diagnostics.Warning.String() &&
			strings.Contains(strings.ToLower(d.Summary), "readonly") &&
			strings.Contains(d.Summary, "required") {
			saw = true
			if !strings.Contains(d.Detail, "clusterId") {
				t.Errorf("warning detail should name clusterId, got: %s", d.Detail)
			}
		}
	}
	if !saw {
		t.Errorf("expected a required+readOnly warning, got diagnostics: %+v", resp.Diagnostics)
	}
}
