package parser

import (
	"testing"
)

// v30EdgeSpec exercises the v30 converter branches the mycloud fixture does not
// reach: path items carrying $ref/summary/description/servers, the trace/head/
// options verbs, parameters with every optional field (style/explode/
// allowReserved/example/examples/content), headers with the full field set,
// and response links with operationId/operationRef/parameters/requestBody/
// server.
const v30EdgeSpec = `
openapi: 3.0.3
info:
  title: Edge
  version: "1.0.0"
paths:
  /refs:
    $ref: "#/components/pathItems/Common"
    summary: ref summary
    description: ref description
    servers:
      - url: https://alt.example.com
        description: alt server
  /verbs:
    trace:
      operationId: traceOp
      responses:
        "200":
          description: ok
    options:
      operationId: optionsOp
      responses:
        "200":
          description: ok
    head:
      operationId: headOp
      responses:
        "200":
          description: ok
  /pets:
    get:
      operationId: listPets
      parameters:
        - name: id
          in: query
          description: the id
          required: true
          deprecated: false
          allowEmptyValue: true
          style: simple
          explode: false
          allowReserved: true
          example: 5
          examples:
            a:
              summary: ex a
              value: 1
          schema:
            type: integer
        - name: X-Body
          in: header
          content:
            application/json:
              schema:
                type: string
      responses:
        "200":
          description: ok
          headers:
            X-Edge:
              description: edge header
              required: true
              deprecated: false
              allowEmptyValue: true
              style: simple
              explode: false
              allowReserved: true
              example: 7
              examples:
                a:
                  summary: header example
                  value: 7
              content:
                application/json:
                  schema:
                    type: integer
              schema:
                type: integer
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Pet"
          links:
            self:
              operationId: getPet
              operationRef: "#/paths/~1pets/get"
              parameters:
                id: $response.body#/id
              requestBody: $request.body
              description: self link
              server:
                url: https://link.example.com
components:
  schemas:
    Pet:
      type: object
      properties:
        id:
          type: integer
`

func loadV30EdgeSpec(t *testing.T) *Spec {
	t.Helper()
	node, err := LoadFile("v30-edge.yaml", []byte(v30EdgeSpec))
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
	return spec
}

// TestConvertV30Edge_PathItemRef asserts a $ref-carrying path item with
// summary/description/servers parses (convertPathItem's ref/servers branches).
func TestConvertV30Edge_PathItemRef(t *testing.T) {
	spec := loadV30EdgeSpec(t)
	ref := spec.Paths["/refs"]
	if ref == nil {
		t.Fatal("missing /refs path item")
	}
	if ref.Ref != "#/components/pathItems/Common" {
		t.Errorf("ref = %q", ref.Ref)
	}
	if ref.Summary != "ref summary" || ref.Description != "ref description" {
		t.Errorf("summary/description = %q/%q", ref.Summary, ref.Description)
	}
	if len(ref.Servers) != 1 || ref.Servers[0].URL != "https://alt.example.com" || ref.Servers[0].Description != "alt server" {
		t.Errorf("servers = %+v", ref.Servers)
	}
}

// TestConvertV30Edge_ExtraVerbs asserts trace/head/options operations convert.
func TestConvertV30Edge_ExtraVerbs(t *testing.T) {
	spec := loadV30EdgeSpec(t)
	verbs := spec.Paths["/verbs"]
	if verbs.Trace == nil || verbs.Trace.OperationID != "traceOp" {
		t.Errorf("trace = %+v", verbs.Trace)
	}
	if verbs.Options == nil || verbs.Options.OperationID != "optionsOp" {
		t.Errorf("options = %+v", verbs.Options)
	}
	if verbs.Head == nil || verbs.Head.OperationID != "headOp" {
		t.Errorf("head = %+v", verbs.Head)
	}
}

// TestConvertV30Edge_ParameterFields asserts a parameter with every optional
// field and a content-carrying parameter convert.
func TestConvertV30Edge_ParameterFields(t *testing.T) {
	spec := loadV30EdgeSpec(t)
	params := spec.Paths["/pets"].Get.Parameters
	if len(params) != 2 {
		t.Fatalf("parameters = %+v", params)
	}
	p := params[0]
	if p.Name != "id" || p.In != "query" || p.Description != "the id" || !p.Required || p.Deprecated || !p.AllowEmptyValue {
		t.Errorf("param base fields = %+v", p)
	}
	if p.Style != "simple" || p.Explode || !p.AllowReserved {
		t.Errorf("param style fields = %+v", p)
	}
	if p.Schema == nil || p.Schema.Type != "integer" {
		t.Errorf("param schema = %+v", p.Schema)
	}
	if len(p.Examples) != 1 || p.Examples["a"].Summary != "ex a" {
		t.Errorf("param examples = %+v", p.Examples)
	}
	contentParam := params[1]
	if contentParam.Content == nil || contentParam.Content["application/json"] == nil {
		t.Errorf("content param = %+v", contentParam)
	}
}

// TestConvertV30Edge_HeaderFields asserts a header with every optional field
// converts via convertHeader.
func TestConvertV30Edge_HeaderFields(t *testing.T) {
	spec := loadV30EdgeSpec(t)
	h, ok := spec.Paths["/pets"].Get.Responses["200"].Headers["X-Edge"]
	if !ok || h == nil {
		t.Fatal("missing X-Edge header")
	}
	if h.Description != "edge header" || !h.Required || h.Deprecated || !h.AllowEmptyValue {
		t.Errorf("header base fields = %+v", h)
	}
	if h.Style != "simple" || h.Explode || !h.AllowReserved {
		t.Errorf("header style fields = %+v", h)
	}
	if h.Schema == nil || h.Schema.Type != "integer" {
		t.Errorf("header schema = %+v", h.Schema)
	}
	if h.Content == nil || h.Content["application/json"] == nil {
		t.Errorf("header content = %+v", h.Content)
	}
	if len(h.Examples) != 1 {
		t.Errorf("header examples = %+v", h.Examples)
	}
}

// TestConvertV30Edge_Link asserts a response link with operationId,
// operationRef, parameters, requestBody, and server converts.
func TestConvertV30Edge_Link(t *testing.T) {
	spec := loadV30EdgeSpec(t)
	l := spec.Paths["/pets"].Get.Responses["200"].Links["self"]
	if l == nil {
		t.Fatal("missing self link")
	}
	if l.OperationID != "getPet" {
		t.Errorf("operationId = %q", l.OperationID)
	}
	if l.OperationRef != "#/paths/~1pets/get" {
		t.Errorf("operationRef = %q", l.OperationRef)
	}
	if l.Parameters["id"] == nil {
		t.Errorf("parameters = %v", l.Parameters)
	}
	if l.Description != "self link" {
		t.Errorf("description = %q", l.Description)
	}
	if l.Server == nil || l.Server.URL != "https://link.example.com" {
		t.Errorf("server = %+v", l.Server)
	}
}

// TestConvertV30Edge_ZeroBounds asserts that schema constraints declared with
// a zero value parse as present pointers, not as absent: `minimum: 0`
// genuinely forbids negative values and must survive to the generated
// validator (G39).
func TestConvertV30Edge_ZeroBounds(t *testing.T) {
	data := []byte(`openapi: 3.0.1
info:
  title: Zero bounds
  version: 1.0.0
paths: {}
components:
  schemas:
    Meter:
      type: object
      properties:
        ratio:
          type: number
          minimum: 0
        retryCeiling:
          type: integer
          maximum: 0
        label:
          type: string
          minLength: 0
        code:
          type: string
          maxLength: 0
        tags:
          type: array
          minItems: 0
          items:
            type: string
`)
	node, err := LoadFile("zero-bounds.yaml", data)
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
	props := spec.Components.Schemas["Meter"].Properties
	if props == nil {
		t.Fatal("Meter properties missing")
	}
	checkFloatPtr := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s bound is nil; a declared 0 must parse as present", name)
		}
		if *got != want {
			t.Fatalf("%s = %v, want %v", name, *got, want)
		}
	}
	checkIntPtr := func(name string, got *int, want int) {
		t.Helper()
		if got == nil {
			t.Fatalf("%s bound is nil; a declared 0 must parse as present", name)
		}
		if *got != want {
			t.Fatalf("%s = %v, want %v", name, *got, want)
		}
	}
	checkFloatPtr("minimum", props["ratio"].Minimum, 0)
	checkFloatPtr("maximum", props["retryCeiling"].Maximum, 0)
	checkIntPtr("minLength", props["label"].MinLength, 0)
	checkIntPtr("maxLength", props["code"].MaxLength, 0)
	checkIntPtr("minItems", props["tags"].MinItems, 0)
	// A schema that declares no bounds parses nil pointers.
	if props["label"].Minimum != nil || props["code"].MaxItems != nil {
		t.Errorf("absent bounds must parse as nil: %+v %+v", props["label"], props["code"])
	}
}
