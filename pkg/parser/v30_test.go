package parser

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/mycloud30.yaml
var mycloud30YAML []byte

// TestConvertV30MycloudRoundTrip loads the bundled mycloud 3.0 fixture and
// verifies that ConvertV30 maps the key fields into the generic Spec model. The
// assertion is semantic: it checks operation IDs, paths, component schemas,
// refs, and headers rather than merely checking JSON marshal idempotency.
func TestConvertV30MycloudRoundTrip(t *testing.T) {
	node, err := LoadFile("testdata/mycloud30.yaml", mycloud30YAML)
	if err != nil {
		t.Fatalf("LoadFile mycloud fixture: %v", err)
	}

	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	if spec.OpenAPI != "3.0.3" {
		t.Fatalf("openapi version: got %q, want 3.0.3", spec.OpenAPI)
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
	if limitSchema == nil || limitSchema.Type != "integer" || limitSchema.Maximum != 100 {
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

	if spec.Components == nil {
		t.Fatal("components is nil")
	}
	for _, name := range []string{"Pet", "Pets", "Error"} {
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
		t.Fatalf("mycloud 3.0 round-trip mismatch:\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

// TestConvertV30Fields verifies that representative 3.0 fields are mapped
// into the generic Spec model.
func TestConvertV30Fields(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Field Test API
  description: Exercises converter field mapping
  version: "1.0.0"
servers:
  - url: https://api.example.com/{version}
    description: Production
    variables:
      version:
        default: v1
        enum:
          - v1
          - v2
paths:
  /items:
    get:
      operationId: listItems
      tags:
        - items
      parameters:
        - name: page
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: string
components:
  schemas:
    Item:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
  parameters:
    ItemId:
      name: itemId
      in: path
      required: true
      schema:
        type: string
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
      bearerFormat: JWT
`)

	node, err := LoadFile("fields.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if len(diags) > 0 {
		t.Logf("diagnostics: %v", diags)
	}

	if spec.OpenAPI != "3.0.3" {
		t.Fatalf("openapi version: got %q, want 3.0.3", spec.OpenAPI)
	}
	if spec.Info == nil || spec.Info.Title != "Field Test API" {
		t.Fatalf("info title mismatch: %+v", spec.Info)
	}
	if len(spec.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(spec.Servers))
	}
	if sv, ok := spec.Servers[0].Variables["version"]; !ok || sv == nil || sv.Default != "v1" {
		t.Fatalf("server variable mismatch: %+v", spec.Servers[0].Variables)
	}
	if len(spec.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(spec.Paths))
	}
	pi := spec.Paths["/items"]
	if pi == nil || pi.Get == nil || pi.Get.OperationID != "listItems" {
		t.Fatalf("path /items mismatch")
	}
	if len(pi.Get.Parameters) != 1 || pi.Get.Parameters[0].Name != "page" {
		t.Fatalf("parameters mismatch: %+v", pi.Get.Parameters)
	}
	if spec.Components == nil || spec.Components.Schemas["Item"] == nil {
		t.Fatalf("components schemas mismatch")
	}
	if spec.Components.SecuritySchemes["bearer"] == nil {
		t.Fatalf("security scheme bearer missing")
	}
}

func TestConvertV30Extensions(t *testing.T) {
	data := []byte(`openapi: 3.0.3
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

	node, err := LoadFile("extensions.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
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

// TestConvertV30Clusters exercises the converter surface area that the bundled
// mycloud fixture and the field test do not cover. The /items path item mixes
// a $ref with concrete child fields, exercising the converter's lenient behavior
// toward sibling fields on a reference object.
func TestConvertV30Clusters(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Cluster Test API
  version: "1.0.0"
  contact:
    name: Support
    url: https://example.com/support
    email: support@example.com
  license:
    name: Apache 2.0
    identifier: Apache-2.0
servers:
  - url: https://api.example.com/{env}
    variables:
      env:
        default: prod
        enum:
          - prod
          - staging
tags:
  - name: items
    description: Item operations
    externalDocs:
      url: https://example.com/docs
security:
  - bearer: []
paths:
  /items:
    $ref: '#/paths/~1other'
    get:
      operationId: listItems
      callbacks:
        onEvent:
          $ref: '#/components/callbacks/Other'
      security:
        - apiKey: []
      requestBody:
        $ref: '#/components/requestBodies/ItemBody'
      responses:
        '200':
          $ref: '#/components/responses/ItemList'
          description: overridden
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Item'
          links:
            next:
              operationId: listItems
              parameters:
                page: '$response.body#/nextPage'
  /upload:
    post:
      operationId: uploadItem
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
            encoding:
              file:
                contentType: application/octet-stream
                headers:
                  X-Trace:
                    description: trace header
                style: form
                explode: true
                allowReserved: false
      responses:
        '201':
          description: created
components:
  responses:
    ItemList:
      description: list response
  parameters:
    ItemId:
      name: itemId
      in: path
      required: true
      schema:
        type: string
  examples:
    ItemExample:
      summary: An example
      description: Example item
      value:
        id: 1
        name: item
      externalValue: https://example.com/example.json
  requestBodies:
    ItemBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Item'
  headers:
    X-Trace:
      description: trace header
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    apiKey:
      type: apiKey
      in: header
      name: X-API-Key
  links:
    next:
      operationId: listItems
  callbacks:
    Other:
      '{$request.body#/url}':
        post:
          operationId: otherCallback
  schemas:
    Item:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
      required:
        - id
      allOf:
        - $ref: '#/components/schemas/Base'
      oneOf:
        - type: string
      anyOf:
        - type: integer
      not:
        type: 'null'
      discriminator:
        propertyName: kind
        mapping:
          a: '#/components/schemas/A'
      xml:
        name: item
        namespace: https://example.com
        prefix: ex
        attribute: false
        wrapped: true
      additionalProperties: true
    Base:
      type: object
    Tuple:
      type: array
      prefixItems:
        - type: integer
        - type: string
      contains:
        type: integer
      minContains: 1
      maxContains: 5
      propertyNames:
        type: string
      dependentSchemas:
        name:
          type: object
      dependentRequired:
        name:
          - id
    Bounds:
      type: number
      exclusiveMaximum: 100
      exclusiveMinimum: 0
    BoundsBool:
      type: number
      maximum: 100
      exclusiveMaximum: true
      minimum: 0
      exclusiveMinimum: true
    BoundsBoolFalse:
      type: number
      maximum: 100
      exclusiveMaximum: false
      minimum: 0
      exclusiveMinimum: false
`)

	node, err := LoadFile("clusters.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	if spec.Info.Contact == nil || spec.Info.Contact.Name != "Support" {
		t.Fatalf("contact mismatch: %+v", spec.Info.Contact)
	}
	if spec.Info.License == nil || spec.Info.License.Identifier != "Apache-2.0" {
		t.Fatalf("license identifier mismatch: %+v", spec.Info.License)
	}
	if len(spec.Tags) != 1 || spec.Tags[0].ExternalDocs == nil {
		t.Fatalf("tags mismatch: %+v", spec.Tags)
	}
	if len(spec.Security) != 1 {
		t.Fatalf("security mismatch: %+v", spec.Security)
	}
	if _, ok := spec.Security[0].Requirements["bearer"]; !ok {
		t.Fatalf("security requirement mismatch: %+v", spec.Security)
	}

	pi := spec.Paths["/items"]
	if pi == nil {
		t.Fatal("missing /items path")
	}
	if pi.Ref != "#/paths/~1other" {
		t.Fatalf("path item $ref mismatch: got %q", pi.Ref)
	}
	if pi.Get == nil || pi.Get.OperationID != "listItems" {
		t.Fatalf("operation mismatch")
	}
	if len(pi.Get.Callbacks) != 1 {
		t.Fatalf("callbacks mismatch: %+v", pi.Get.Callbacks)
	}
	cb := pi.Get.Callbacks["onEvent"]
	if cb == nil {
		t.Fatal("missing onEvent callback")
	}
	if ref, ok := cb.IsRef(); !ok || ref != "#/components/callbacks/Other" {
		t.Fatalf("callback IsRef mismatch: got %q, %v", ref, ok)
	}

	if pi.Get.RequestBody == nil || pi.Get.RequestBody.Ref != "#/components/requestBodies/ItemBody" {
		t.Fatalf("request body $ref mismatch: %+v", pi.Get.RequestBody)
	}
	if len(pi.Get.Security) != 1 {
		t.Fatalf("operation security mismatch: %+v", pi.Get.Security)
	}
	if _, ok := pi.Get.Security[0].Requirements["apiKey"]; !ok {
		t.Fatalf("operation security requirement mismatch: %+v", pi.Get.Security)
	}

	resp := pi.Get.Responses["200"]
	if resp == nil {
		t.Fatal("missing 200 response")
	}
	if resp.Ref != "#/components/responses/ItemList" {
		t.Fatalf("response $ref mismatch: got %q", resp.Ref)
	}
	if len(resp.Links) != 1 || resp.Links["next"] == nil {
		t.Fatalf("response links mismatch: %+v", resp.Links)
	}
	if resp.Links["next"].Parameters["page"] != "$response.body#/nextPage" {
		t.Fatalf("link parameter mismatch: %+v", resp.Links["next"].Parameters)
	}

	upload := spec.Paths["/upload"]
	if upload == nil || upload.Post == nil {
		t.Fatal("missing /upload post")
	}
	mt := upload.Post.RequestBody.Content["multipart/form-data"]
	if mt == nil || mt.Encoding["file"] == nil {
		t.Fatalf("encoding mismatch: %+v", mt)
	}
	enc := mt.Encoding["file"]
	if enc.ContentType != "application/octet-stream" || enc.Style != "form" || !enc.Explode || enc.AllowReserved {
		t.Fatalf("encoding fields mismatch: %+v", enc)
	}
	if len(enc.Headers) != 1 || enc.Headers["X-Trace"] == nil {
		t.Fatalf("encoding headers mismatch: %+v", enc.Headers)
	}

	comp := spec.Components
	if comp == nil {
		t.Fatal("components is nil")
	}
	if comp.Responses["ItemList"] == nil {
		t.Fatalf("component response missing: %+v", comp.Responses)
	}
	if comp.Parameters["ItemId"] == nil {
		t.Fatalf("component parameter missing: %+v", comp.Parameters)
	}
	if comp.Examples["ItemExample"] == nil || comp.Examples["ItemExample"].ExternalValue == "" {
		t.Fatalf("component example missing: %+v", comp.Examples)
	}
	if comp.RequestBodies["ItemBody"] == nil {
		t.Fatalf("component request body missing: %+v", comp.RequestBodies)
	}
	if comp.Headers["X-Trace"] == nil {
		t.Fatalf("component header missing: %+v", comp.Headers)
	}
	if comp.Links["next"] == nil {
		t.Fatalf("component link missing: %+v", comp.Links)
	}
	if len(comp.Callbacks) != 1 {
		t.Fatalf("component callbacks mismatch: %+v", comp.Callbacks)
	}

	item := comp.Schemas["Item"]
	if item == nil {
		t.Fatal("Item schema missing")
	}
	if len(item.AllOf) != 1 || item.AllOf[0].Ref != "#/components/schemas/Base" {
		t.Fatalf("allOf mismatch: %+v", item.AllOf)
	}
	if len(item.OneOf) != 1 || item.OneOf[0].Type != "string" {
		t.Fatalf("oneOf mismatch: %+v", item.OneOf)
	}
	if len(item.AnyOf) != 1 || item.AnyOf[0].Type != "integer" {
		t.Fatalf("anyOf mismatch: %+v", item.AnyOf)
	}
	if item.Not == nil || item.Not.Type != "null" {
		t.Fatalf("not mismatch: %+v", item.Not)
	}
	if item.Discriminator == nil || item.Discriminator.PropertyName != "kind" {
		t.Fatalf("discriminator mismatch: %+v", item.Discriminator)
	}
	if item.XML == nil || item.XML.Name != "item" || !item.XML.Wrapped {
		t.Fatalf("xml mismatch: %+v", item.XML)
	}
	if item.AdditionalProperties != true {
		t.Fatalf("additionalProperties bool mismatch: %v", item.AdditionalProperties)
	}

	tuple := comp.Schemas["Tuple"]
	if tuple == nil {
		t.Fatal("Tuple schema missing")
	}
	if len(tuple.PrefixItems) != 2 {
		t.Fatalf("prefixItems mismatch: %+v", tuple.PrefixItems)
	}
	if tuple.Contains == nil {
		t.Fatalf("contains missing: %+v", tuple)
	}
	if tuple.MinContains != 1 || tuple.MaxContains != 5 {
		t.Fatalf("min/max contains mismatch: %d/%d", tuple.MinContains, tuple.MaxContains)
	}
	if tuple.PropertyNames == nil {
		t.Fatalf("propertyNames missing: %+v", tuple)
	}
	if len(tuple.DependentSchemas) != 1 {
		t.Fatalf("dependentSchemas mismatch: %+v", tuple.DependentSchemas)
	}
	if len(tuple.DependentRequired) != 1 {
		t.Fatalf("dependentRequired mismatch: %+v", tuple.DependentRequired)
	}

	bounds := comp.Schemas["Bounds"]
	if bounds == nil {
		t.Fatal("Bounds schema missing")
	}
	if bounds.ExclusiveMaximum != 100.0 || bounds.ExclusiveMinimum != 0.0 {
		t.Fatalf("exclusive bounds mismatch: max=%v min=%v", bounds.ExclusiveMaximum, bounds.ExclusiveMinimum)
	}

	boundsBool := comp.Schemas["BoundsBool"]
	if boundsBool == nil {
		t.Fatal("BoundsBool schema missing")
	}
	if em, ok := boundsBool.ExclusiveMaximum.(bool); !ok || !em {
		t.Fatalf("BoundsBool exclusiveMaximum bool mismatch: got %v", boundsBool.ExclusiveMaximum)
	}
	if em, ok := boundsBool.ExclusiveMinimum.(bool); !ok || !em {
		t.Fatalf("BoundsBool exclusiveMinimum bool mismatch: got %v", boundsBool.ExclusiveMinimum)
	}

	boundsBoolFalse := comp.Schemas["BoundsBoolFalse"]
	if boundsBoolFalse == nil {
		t.Fatal("BoundsBoolFalse schema missing")
	}
	if em, ok := boundsBoolFalse.ExclusiveMaximum.(bool); !ok || em {
		t.Fatalf("BoundsBoolFalse exclusiveMaximum bool mismatch: got %v", boundsBoolFalse.ExclusiveMaximum)
	}
	if em, ok := boundsBoolFalse.ExclusiveMinimum.(bool); !ok || em {
		t.Fatalf("BoundsBoolFalse exclusiveMinimum bool mismatch: got %v", boundsBoolFalse.ExclusiveMinimum)
	}
}

func TestConvertV30Diagnostics(t *testing.T) {
	t.Run("nil root", func(t *testing.T) {
		spec, diags, err := ConvertV30(nil)
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
		node, err := LoadFile("bad.yaml", []byte("not a mapping"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV30(node)
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

	t.Run("structural type mismatch", func(t *testing.T) {
		data := []byte(`openapi: 3.0.3
info: not a mapping
paths: []
`)
		node, err := LoadFile("mismatch.yaml", data)
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV30(node)
		if err != nil {
			t.Fatalf("ConvertV30: %v", err)
		}
		if spec.Info != nil {
			t.Fatalf("expected nil info, got %+v", spec.Info)
		}
		if len(spec.Paths) != 0 {
			t.Fatalf("expected empty paths, got %+v", spec.Paths)
		}
		wantSummaries := map[string]bool{
			"info has unexpected type":  false,
			"paths has unexpected type": false,
		}
		for _, d := range diags {
			wantSummaries[d.Summary] = true
		}
		for summary, seen := range wantSummaries {
			if !seen {
				t.Fatalf("expected warning %q not found in %v", summary, diags)
			}
		}
	})

	t.Run("webhooks in 3.0 warning", func(t *testing.T) {
		data := []byte(`openapi: 3.0.3
info:
  title: W
  version: "1.0.0"
webhooks:
  newItem:
    post:
      operationId: newItem
`)
		node, err := LoadFile("webhooks30.yaml", data)
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV30(node)
		if err != nil {
			t.Fatalf("ConvertV30: %v", err)
		}
		if len(spec.Webhooks) != 1 {
			t.Fatalf("expected webhooks converted: %+v", spec.Webhooks)
		}
		if len(diags) != 1 || diags[0].Severity != SeverityWarning {
			t.Fatalf("expected one webhook warning, got %+v", diags)
		}
	})

	t.Run("no webhooks warning in 3.1", func(t *testing.T) {
		data := []byte(`openapi: 3.1.0
info:
  title: W
  version: "1.0.0"
webhooks:
  newItem:
    post:
      operationId: newItem
`)
		node, err := LoadFile("webhooks31.yaml", data)
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV31(node)
		if err != nil {
			t.Fatalf("ConvertV31: %v", err)
		}
		if len(spec.Webhooks) != 1 {
			t.Fatalf("expected webhooks converted: %+v", spec.Webhooks)
		}
		for _, d := range diags {
			if d.Summary == "webhooks field in OpenAPI 3.0.x document" {
				t.Fatalf("unexpected 3.0 webhooks warning in 3.1: %s", d)
			}
		}
	})
}

func TestConvertV30DiscriminatorMissingPropertyName(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: T
  version: "1.0.0"
components:
  schemas:
    Pet:
      type: object
      discriminator:
        mapping:
          Dog: '#/components/schemas/Dog'
`)
	node, err := LoadFile("missing-property.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if spec.Components == nil || spec.Components.Schemas["Pet"] == nil {
		t.Fatal("expected Pet schema")
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && d.Summary == "Invalid discriminator" && strings.Contains(d.Detail, "propertyName must not be empty") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error diagnostic for missing discriminator propertyName, got %v", diags)
	}
}

func TestConvertV30SchemaTypeMappingEmitsWarning(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: T
  version: "1.0.0"
components:
  schemas:
    Bad:
      type:
        object: true
`)
	node, err := LoadFile("badtype-map.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if spec.Components == nil || spec.Components.Schemas["Bad"] == nil {
		t.Fatal("Bad schema missing")
	}
	if spec.Components.Schemas["Bad"].Type != nil {
		t.Errorf("mapping type should yield nil, got %v", spec.Components.Schemas["Bad"].Type)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityWarning && strings.Contains(d.Summary, "schema type") &&
			strings.Contains(d.Detail, "mapping or sequence") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning for mapping-valued schema type, got %v", diags)
	}
}

func TestConvertV30SchemaTypeValidation(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: T
  version: "1.0.0"
components:
  schemas:
    Bad:
      type: 42
    NullType:
      type:
        - string
        - 42
`)
	node, err := LoadFile("badtype.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	if spec.Components == nil || spec.Components.Schemas["Bad"] == nil {
		t.Fatal("Bad schema missing")
	}
	bad := spec.Components.Schemas["Bad"].Type
	// Non-string scalars preserve their native value (float64, since the lexer
	// parses all numbers as float64) rather than falling back to raw source
	// text, matching 3.1's behavior (L-88).
	if f, ok := bad.(float64); !ok || f != 42 {
		t.Fatalf("Bad type fallback mismatch: got %v", bad)
	}

	hasWarnings := make(map[string]int)
	for _, d := range diags {
		hasWarnings[d.Summary]++
	}
	if hasWarnings["schema type is not a string"] != 1 {
		t.Fatalf("expected one 'schema type is not a string' warning, got %v", diags)
	}
	if hasWarnings["schema type array contains non-string item"] != 1 {
		t.Fatalf("expected one array-item warning, got %v", diags)
	}
}

func TestNodeHelpers(t *testing.T) {
	node, err := LoadFile("helpers.yaml", []byte(`
int64: 9223372036854775807
uint64: 18446744073709551615
float: 3.9
strnum: "12.5"
`))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	m := node.(*MapNode)

	get := func(key string) Node {
		for _, e := range m.Entries {
			if e.Key != nil {
				k, _ := asString(e.Key)
				if k == key {
					return e.Value
				}
			}
		}
		t.Fatalf("key %q not found", key)
		return nil
	}

	t.Run("nodeFloat handles int64/uint64", func(t *testing.T) {
		if f, ok := nodeFloat(get("int64")); !ok || f != 9223372036854775807 {
			t.Fatalf("int64 nodeFloat: got %v, %v", f, ok)
		}
		if f, ok := nodeFloat(get("uint64")); !ok || f != 18446744073709551615 {
			t.Fatalf("uint64 nodeFloat: got %v, %v", f, ok)
		}
	})

	t.Run("nodeInt truncates floats", func(t *testing.T) {
		i, ok := nodeInt(get("float"))
		if !ok || i != 3 {
			t.Fatalf("nodeInt(3.9): got %d, %v", i, ok)
		}
	})

	t.Run("nodeFloat parses string numbers", func(t *testing.T) {
		f, ok := nodeFloat(get("strnum"))
		if !ok || f != 12.5 {
			t.Fatalf("nodeFloat('12.5'): got %v, %v", f, ok)
		}
	})
}

func TestCallbackIsRef(t *testing.T) {
	refOnly := Callback{"$ref": &PathItem{Ref: "#/components/callbacks/Foo"}}
	if ref, ok := refOnly.IsRef(); !ok || ref != "#/components/callbacks/Foo" {
		t.Fatalf("IsRef on ref-only callback: got %q, %v", ref, ok)
	}

	pathKeyed := Callback{"{$request.body#/url}": &PathItem{Get: &Operation{OperationID: "x"}}}
	if ref, ok := pathKeyed.IsRef(); ok {
		t.Fatalf("IsRef on path-keyed callback should be false, got %q", ref)
	}

	mixed := Callback{
		"$ref":                 &PathItem{Ref: "#/components/callbacks/Foo"},
		"{$request.body#/url}": &PathItem{},
	}
	if ref, ok := mixed.IsRef(); ok {
		t.Fatalf("IsRef on multi-key callback should be false, got %q", ref)
	}

	// Round-trip through JSON preserves IsRef semantics.
	b, err := json.Marshal(refOnly)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt Callback
	if err := json.Unmarshal(b, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ref, ok := rt.IsRef(); !ok || ref != "#/components/callbacks/Foo" {
		t.Fatalf("round-trip IsRef: got %q, %v", ref, ok)
	}
}

func ExampleCallback_IsRef() {
	cb := Callback{"$ref": &PathItem{Ref: "#/components/callbacks/OnEvent"}}
	if ref, ok := cb.IsRef(); ok {
		fmt.Println(ref)
	}
	// Output: #/components/callbacks/OnEvent
}

func TestConvertV30ScalarTypeMismatches(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Mismatch API
  version: "1.0.0"
  description: 42
paths: {}
components:
  schemas:
    Pet:
      type: object
      maxLength: "not a number"
      required:
        - name
        - 123
`)
	node, err := LoadFile("mismatches.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if spec.Info.Description != "" {
		t.Errorf("expected empty description for type mismatch, got %q", spec.Info.Description)
	}

	var foundDesc, foundMaxLength, foundRequired bool
	for _, d := range diags {
		switch d.Summary {
		case "description has unexpected type":
			foundDesc = true
		case "maxLength has unexpected type":
			foundMaxLength = true
		case "required item has unexpected type":
			foundRequired = true
		}
	}
	if !foundDesc {
		t.Error("expected diagnostic for string field with numeric value")
	}
	if !foundMaxLength {
		t.Error("expected diagnostic for integer field with string value")
	}
	if !foundRequired {
		t.Error("expected diagnostic for required array with non-string item")
	}
}
