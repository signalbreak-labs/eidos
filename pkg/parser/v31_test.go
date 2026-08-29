package parser

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"
)

//go:embed testdata/mycloud31.yaml
var mycloud31YAML []byte

// TestConvertV31MycloudRoundTrip loads the bundled mycloud 3.1 fixture and
// verifies that ConvertV31 maps the key fields into the generic Spec model. The
// assertions are semantic: it checks operation IDs, paths, component schemas,
// tuple items, and webhooks rather than merely checking JSON marshal idempotency.
func TestConvertV31MycloudRoundTrip(t *testing.T) {
	node, err := LoadFile("testdata/mycloud31.yaml", mycloud31YAML)
	if err != nil {
		t.Fatalf("LoadFile mycloud 3.1 fixture: %v", err)
	}

	spec, diags, err := ConvertV31(node)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version: got %q, want 3.1.0", spec.OpenAPI)
	}
	if spec.JSONSchemaDialect != "https://spec.openapis.org/oas/3.1/dialect/base" {
		t.Fatalf("jsonSchemaDialect: got %q, want dialect URL", spec.JSONSchemaDialect)
	}
	if spec.Info == nil || spec.Info.Title != "Mycloud" || spec.Info.Version != "1.0.0" {
		t.Fatalf("info mismatch: %+v", spec.Info)
	}

	if len(spec.Servers) != 1 || spec.Servers[0].URL != "https://api.mycloud.example/v1" {
		t.Fatalf("servers mismatch: %+v", spec.Servers)
	}

	wantPaths := []string{"/pets", "/pets/{petId}"}
	if len(spec.Paths) != len(wantPaths) {
		t.Fatalf("paths count: got %d, want %d", len(spec.Paths), len(wantPaths))
	}
	for _, p := range wantPaths {
		if spec.Paths[p] == nil {
			t.Fatalf("missing path %q", p)
		}
	}

	ops := map[string]*Operation{
		"listPets":    spec.Paths["/pets"].Get,
		"createPets":  spec.Paths["/pets"].Post,
		"showPetById": spec.Paths["/pets/{petId}"].Get,
	}
	for wantID, op := range ops {
		if op == nil || op.OperationID != wantID {
			t.Fatalf("operation %q missing or mismatched: %+v", wantID, op)
		}
	}

	listPets := spec.Paths["/pets"].Get
	if len(listPets.Parameters) != 1 || listPets.Parameters[0].Name != "limit" {
		t.Fatalf("listPets parameters mismatch: %+v", listPets.Parameters)
	}
	limitSchema := listPets.Parameters[0].Schema
	if limitSchema == nil || limitSchema.Type != "integer" || limitSchema.Maximum == nil || *limitSchema.Maximum != 100 {
		t.Fatalf("limit schema mismatch: %+v", limitSchema)
	}

	okResp := listPets.Responses["200"]
	if okResp == nil {
		t.Fatal("missing 200 response on listPets")
	}
	xnext, ok := okResp.Headers["x-next"]
	if !ok || xnext == nil || xnext.Description == "" {
		t.Fatalf("x-next header missing or empty: %+v", okResp.Headers)
	}
	if okResp.Content["application/json"].Schema == nil || okResp.Content["application/json"].Schema.Ref != "#/components/schemas/Pets" {
		t.Fatalf("200 response schema ref mismatch")
	}

	if len(spec.Webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(spec.Webhooks))
	}
	wh := spec.Webhooks["newPet"]
	if wh == nil || wh.Post == nil || wh.Post.OperationID != "newPet" {
		t.Fatalf("webhook newPet mismatch")
	}

	if spec.Components == nil {
		t.Fatal("components is nil")
	}
	for _, name := range []string{"Pet", "Pets", "Error", "PetTuple", "PetSnapshot"} {
		if spec.Components.Schemas[name] == nil {
			t.Fatalf("missing component schema %q", name)
		}
	}

	pet := spec.Components.Schemas["Pet"]
	if pet.Type != "object" || !reflect.DeepEqual(pet.Required, []string{"id", "name"}) {
		t.Fatalf("Pet schema mismatch: %+v", pet)
	}
	if pet.Properties["id"] == nil || pet.Properties["id"].Type != "integer" {
		t.Fatalf("Pet.properties.id missing or wrong type")
	}
	if pet.Properties["tag"] == nil {
		t.Fatalf("Pet.properties.tag missing")
	}
	if tagTypes, ok := pet.Properties["tag"].Type.([]any); !ok || len(tagTypes) != 2 || tagTypes[0] != "string" || tagTypes[1] != nil {
		t.Fatalf("Pet.properties.tag type mismatch: got %v", pet.Properties["tag"].Type)
	}

	petTuple := spec.Components.Schemas["PetTuple"]
	if len(petTuple.PrefixItems) != 2 {
		t.Fatalf("PetTuple prefixItems mismatch: %+v", petTuple.PrefixItems)
	}
	if itemsFalse, ok := petTuple.Items.(bool); !ok || itemsFalse {
		t.Fatalf("PetTuple.items mismatch: expected false, got %v (%T)", petTuple.Items, petTuple.Items)
	}

	petSnapshot := spec.Components.Schemas["PetSnapshot"]
	if petSnapshot.ContentMediaType != "application/json" || petSnapshot.ContentEncoding != "base64" {
		t.Fatalf("PetSnapshot content metadata mismatch: %+v", petSnapshot)
	}

	// Verify JSON round-trip still preserves the populated fields.
	first, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal converted spec: %v", err)
	}
	var intermediate Spec
	if err := json.Unmarshal(first, &intermediate); err != nil {
		t.Fatalf("unmarshal converted spec: %v", err)
	}
	second, err := json.Marshal(&intermediate)
	if err != nil {
		t.Fatalf("re-marshal converted spec: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("mycloud 3.1 round-trip mismatch:\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

// TestConvertV31Fields verifies that representative 3.1 fields are mapped
// into the generic Spec model, including type arrays, webhooks, prefixItems,
// contentMediaType, contentEncoding, numeric exclusive bounds, const, and
// boolean items.
func TestConvertV31Fields(t *testing.T) {
	data := []byte(`openapi: 3.1.0
jsonSchemaDialect: https://spec.openapis.org/oas/3.1/dialect/base
info:
  title: Field Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com/{version}
    variables:
      version:
        default: v1
paths:
  /items:
    get:
      operationId: listItems
webhooks:
  newItem:
    post:
      operationId: newItemWebhook
      responses:
        '200':
          description: ok
components:
  schemas:
    Item:
      type: object
      properties:
        id:
          type: integer
        label:
          type:
            - string
            - null
    Tuple:
      type: array
      prefixItems:
        - type: integer
        - type: string
      items: false
    AnyItems:
      type: array
      items: true
    Encoded:
      type: string
      contentMediaType: application/octet-stream
      contentEncoding: base64
    Bounds:
      type: number
      exclusiveMaximum: 100
      exclusiveMinimum: 0
    Constant:
      const: fixed-value
`)

	node, err := LoadFile("fields31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV31(node)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	if len(diags) > 0 {
		t.Logf("diagnostics: %v", diags)
	}

	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version: got %q, want 3.1.0", spec.OpenAPI)
	}
	if spec.JSONSchemaDialect != "https://spec.openapis.org/oas/3.1/dialect/base" {
		t.Fatalf("jsonSchemaDialect: got %q, want dialect URL", spec.JSONSchemaDialect)
	}
	if spec.Info == nil || spec.Info.Title != "Field Test API" {
		t.Fatalf("info title mismatch: %+v", spec.Info)
	}
	if len(spec.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(spec.Servers))
	}
	if len(spec.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(spec.Paths))
	}
	pi := spec.Paths["/items"]
	if pi == nil || pi.Get == nil || pi.Get.OperationID != "listItems" {
		t.Fatalf("path /items mismatch")
	}
	if len(spec.Webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(spec.Webhooks))
	}
	wh := spec.Webhooks["newItem"]
	if wh == nil || wh.Post == nil || wh.Post.OperationID != "newItemWebhook" {
		t.Fatalf("webhook newItem mismatch")
	}
	if spec.Components == nil || spec.Components.Schemas["Item"] == nil {
		t.Fatalf("components schemas mismatch")
	}

	item := spec.Components.Schemas["Item"]
	label := item.Properties["label"]
	if label == nil {
		t.Fatalf("Item.label missing")
	}
	types, ok := label.Type.([]any)
	if !ok || len(types) != 2 {
		t.Fatalf("expected label type to be a 2-element array, got %T %v", label.Type, label.Type)
	}
	if types[0] != "string" || types[1] != nil {
		t.Fatalf("expected [string, null] type array, got %v", types)
	}

	tuple := spec.Components.Schemas["Tuple"]
	if tuple == nil || len(tuple.PrefixItems) != 2 {
		t.Fatalf("expected Tuple with 2 prefixItems, got %+v", tuple)
	}
	if itemsFalse, ok := tuple.Items.(bool); !ok || itemsFalse {
		t.Fatalf("expected Tuple.items false, got %v (%T)", tuple.Items, tuple.Items)
	}

	anyItems := spec.Components.Schemas["AnyItems"]
	if anyItems == nil || anyItems.Type != "array" {
		t.Fatalf("expected AnyItems array schema, got %+v", anyItems)
	}
	if itemsTrue, ok := anyItems.Items.(bool); !ok || !itemsTrue {
		t.Fatalf("expected AnyItems.items true, got %v (%T)", anyItems.Items, anyItems.Items)
	}

	encoded := spec.Components.Schemas["Encoded"]
	if encoded == nil {
		t.Fatalf("Encoded schema missing")
	}
	if encoded.ContentMediaType != "application/octet-stream" {
		t.Fatalf("contentMediaType mismatch: got %q", encoded.ContentMediaType)
	}
	if encoded.ContentEncoding != "base64" {
		t.Fatalf("contentEncoding mismatch: got %q", encoded.ContentEncoding)
	}

	bounds := spec.Components.Schemas["Bounds"]
	if bounds == nil {
		t.Fatalf("Bounds schema missing")
	}
	if em, ok := bounds.ExclusiveMaximum.(float64); !ok || em != 100 {
		t.Fatalf("exclusiveMaximum mismatch: got %v (%T)", bounds.ExclusiveMaximum, bounds.ExclusiveMaximum)
	}
	if em, ok := bounds.ExclusiveMinimum.(float64); !ok || em != 0 {
		t.Fatalf("exclusiveMinimum mismatch: got %v (%T)", bounds.ExclusiveMinimum, bounds.ExclusiveMinimum)
	}

	constant := spec.Components.Schemas["Constant"]
	if constant == nil {
		t.Fatalf("Constant schema missing")
	}
	if constant.Const != "fixed-value" {
		t.Fatalf("const mismatch: got %v", constant.Const)
	}
}

// TestConvertV31OpenAPI31Fields verifies that ConvertV31 populates the full
// set of OpenAPI 3.1 JSON Schema fields from a real YAML document, not just
// through direct struct JSON round-trips.
func TestConvertV31OpenAPI31Fields(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: 3.1 Fields API
  version: "1.0.0"
components:
  schemas:
    Full31:
      type: object
      propertyNames:
        type: string
        minLength: 1
      prefixItems:
        - type: string
        - type: integer
      if:
        required: ["name"]
      then:
        required: ["id"]
      else:
        required: ["alias"]
      dependentSchemas:
        name:
          type: string
      dependentRequired:
        name: ["id"]
      const: fixed-value
      contentMediaType: application/json
      contentEncoding: base64
      unevaluatedItems:
        type: boolean
`)

	node, err := LoadFile("full31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV31(node)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	if spec.Components == nil || spec.Components.Schemas["Full31"] == nil {
		t.Fatalf("Full31 schema missing")
	}
	s := spec.Components.Schemas["Full31"]

	if s.PropertyNames == nil || s.PropertyNames.Type != "string" || s.PropertyNames.MinLength == nil || *s.PropertyNames.MinLength != 1 {
		t.Fatalf("propertyNames mismatch: %+v", s.PropertyNames)
	}
	if len(s.PrefixItems) != 2 || s.PrefixItems[0].Type != "string" || s.PrefixItems[1].Type != "integer" {
		t.Fatalf("prefixItems mismatch: %+v", s.PrefixItems)
	}
	if s.If == nil || !reflect.DeepEqual(s.If.Required, []string{"name"}) {
		t.Fatalf("if mismatch: %+v", s.If)
	}
	if s.Then == nil || !reflect.DeepEqual(s.Then.Required, []string{"id"}) {
		t.Fatalf("then mismatch: %+v", s.Then)
	}
	if s.Else == nil || !reflect.DeepEqual(s.Else.Required, []string{"alias"}) {
		t.Fatalf("else mismatch: %+v", s.Else)
	}
	if len(s.DependentSchemas) != 1 || s.DependentSchemas["name"] == nil || s.DependentSchemas["name"].Type != "string" {
		t.Fatalf("dependentSchemas mismatch: %+v", s.DependentSchemas)
	}
	if !reflect.DeepEqual(s.DependentRequired, map[string][]string{"name": {"id"}}) {
		t.Fatalf("dependentRequired mismatch: %+v", s.DependentRequired)
	}
	if s.Const != "fixed-value" {
		t.Fatalf("const mismatch: got %v", s.Const)
	}
	if s.ContentMediaType != "application/json" {
		t.Fatalf("contentMediaType mismatch: got %q", s.ContentMediaType)
	}
	if s.ContentEncoding != "base64" {
		t.Fatalf("contentEncoding mismatch: got %q", s.ContentEncoding)
	}
	if s.UnevaluatedItems == nil || s.UnevaluatedItems.Type != "boolean" {
		t.Fatalf("unevaluatedItems mismatch: %+v", s.UnevaluatedItems)
	}
}

// TestConvertV31NullSchema exercises a schema whose value is YAML null. OpenAPI
// 3.1 allows this to denote a nullable-only schema. The converter leaves the
// resulting *Schema as zero-valued and does not treat it as an error.
func TestConvertV31NullSchema(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: Null Schema API
  version: "1.0.0"
components:
  schemas:
    Nullish:
      type: 'null'
    ExplicitNull:
`)

	node, err := LoadFile("null31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV31(node)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	nullish := spec.Components.Schemas["Nullish"]
	if nullish == nil {
		t.Fatal("Nullish schema missing")
	}
	if nullish.Type != "null" {
		t.Fatalf("Nullish type mismatch: got %v", nullish.Type)
	}

	explicit := spec.Components.Schemas["ExplicitNull"]
	if explicit != nil {
		t.Fatalf("expected nil schema for explicit null value, got %+v", explicit)
	}
}

// TestConvertV31Extensions verifies that ConvertV31 populates Extensions on the
// top-level Spec and on nested objects, preserving `x-*` vendor extensions from
// OpenAPI 3.1 documents.
func TestConvertV31Extensions(t *testing.T) {
	data := []byte(`openapi: 3.1.0
x-document-ext: document-value
info:
  title: Ext API
  version: "1.0.0"
  x-info-ext: info-value
paths:
  /items:
    x-path-ext: path-value
    get:
      operationId: listItems
      x-op-ext: op-value
      responses:
        '200':
          description: ok
          x-response-ext: response-value
components:
  x-components-ext: components-value
  schemas:
    Item:
      type: object
      x-schema-ext: schema-value
`)

	node, err := LoadFile("extensions31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV31(node)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}

	if spec.Extensions["x-document-ext"] != "document-value" {
		t.Fatalf("spec extensions missing: %+v", spec.Extensions)
	}
	if spec.Info == nil || spec.Info.Extensions["x-info-ext"] != "info-value" {
		t.Fatalf("info extensions missing: %+v", spec.Info)
	}
	pi := spec.Paths["/items"]
	if pi == nil || pi.Extensions["x-path-ext"] != "path-value" {
		t.Fatalf("path extensions missing: %+v", pi)
	}
	if pi.Get == nil || pi.Get.Extensions["x-op-ext"] != "op-value" {
		t.Fatalf("operation extensions missing: %+v", pi.Get)
	}
	resp := pi.Get.Responses["200"]
	if resp == nil || resp.Extensions["x-response-ext"] != "response-value" {
		t.Fatalf("response extensions missing: %+v", resp)
	}
	if spec.Components == nil || spec.Components.Extensions["x-components-ext"] != "components-value" {
		t.Fatalf("components extensions missing: %+v", spec.Components)
	}
	if spec.Components.Schemas["Item"] == nil || spec.Components.Schemas["Item"].Extensions["x-schema-ext"] != "schema-value" {
		t.Fatalf("schema extensions missing: %+v", spec.Components.Schemas["Item"])
	}
}

// TestConvertV31Diagnostics verifies the converter's error handling for nil and
// invalid root nodes.
func TestConvertV31Diagnostics(t *testing.T) {
	t.Run("nil root", func(t *testing.T) {
		spec, diags, err := ConvertV31(nil)
		if spec != nil {
			t.Fatalf("expected nil spec, got %+v", spec)
		}
		if err == nil {
			t.Fatal("expected error for nil root")
		}
		if len(diags) != 1 || diags[0].Severity != SeverityError {
			t.Fatalf("expected one error diagnostic, got %+v", diags)
		}
	})

	t.Run("non-map root", func(t *testing.T) {
		node, err := LoadFile("bad31.yaml", []byte("not a mapping"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV31(node)
		if spec != nil {
			t.Fatalf("expected nil spec, got %+v", spec)
		}
		if err == nil {
			t.Fatal("expected error for non-map root")
		}
		if len(diags) != 1 || diags[0].Severity != SeverityError {
			t.Fatalf("expected one error diagnostic, got %+v", diags)
		}
	})
}

// TestConvertV31InfoSummary locks in the M-32 fix: info.summary (an OpenAPI 3.1
// field) must be parsed into Info.Summary rather than being silently dropped.
func TestConvertV31InfoSummary(t *testing.T) {
	data := []byte(`openapi: "3.1.0"
info:
  title: With Summary
  summary: A short description.
  version: "1.0"
paths: {}
`)
	root, err := LoadFile("info-summary.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error diagnostic: %v", d)
		}
	}
	if spec.Info == nil {
		t.Fatal("expected Info to be populated")
	}
	if spec.Info.Summary != "A short description." {
		t.Fatalf("expected Info.Summary to be parsed, got %q", spec.Info.Summary)
	}
}
