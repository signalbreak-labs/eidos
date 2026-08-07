package parser

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestDetectCircularSchemaRefsSelfReferencingProperty(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      type: object
      properties:
        next:
          $ref: '#/components/schemas/Node'
paths: {}
`)
	root, err := LoadFile("self.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 || diags[0].Severity != SeverityWarning {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, convertDiags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if len(convertDiags) != 1 {
		t.Fatalf("expected one conversion warning, got %v", convertDiags)
	}
	node := spec.Components.Schemas["Node"]
	if node == nil {
		t.Fatalf("Node schema missing")
	}
	next := node.Properties["next"]
	if next == nil {
		t.Fatalf("next property missing")
	}
	if !next.Opaque {
		t.Fatalf("expected next property to be Opaque")
	}
}

func TestDetectCircularSchemaRefsIndirectCycle(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    A:
      type: object
      properties:
        b:
          $ref: '#/components/schemas/B'
    B:
      type: object
      properties:
        a:
          $ref: '#/components/schemas/A'
paths: {}
`)
	root, err := LoadFile("indirect.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	want := []string{"#/components/schemas/A", "#/components/schemas/B"}
	for _, ref := range want {
		if !slices.Contains(refs, ref) {
			t.Fatalf("expected circular ref %q, got %v", ref, refs)
		}
	}
	if len(diags) != 2 {
		t.Fatalf("expected two warnings, got %v", diags)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !spec.Components.Schemas["A"].Properties["b"].Opaque {
		t.Fatalf("expected A.b to be Opaque")
	}
	if !spec.Components.Schemas["B"].Properties["a"].Opaque {
		t.Fatalf("expected B.a to be Opaque")
	}
}

func TestDetectCircularSchemaRefsNoCycle(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    A:
      type: object
      properties:
        b:
          $ref: '#/components/schemas/B'
    B:
      type: string
paths: {}
`)
	root, err := LoadFile("nocycle.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 0 {
		t.Fatalf("expected no circular refs, got %v", refs)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	spec, convertDiags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if len(convertDiags) != 0 {
		t.Fatalf("expected no conversion diagnostics, got %v", convertDiags)
	}
	if spec.Components.Schemas["A"].Properties["b"].Opaque {
		t.Fatalf("expected A.b not to be Opaque")
	}
}

func TestDetectCircularSchemaRefsSwaggerDefinitions(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test
  version: "1.0"
paths: {}
definitions:
  A:
    type: object
    properties:
      b:
        $ref: '#/definitions/B'
  B:
    type: object
    properties:
      a:
        $ref: '#/definitions/A'
`)
	root, err := LoadFile("swagger-cycle.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	want := []string{"#/definitions/A", "#/definitions/B"}
	for _, ref := range want {
		if !slices.Contains(refs, ref) {
			t.Fatalf("expected circular ref %q, got %v", ref, refs)
		}
	}
	if len(diags) != 2 {
		t.Fatalf("expected two warnings, got %v", diags)
	}

	spec, _, err := ConvertV2(root)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if !spec.Components.Schemas["A"].Properties["b"].Opaque {
		t.Fatalf("expected A.b to be Opaque")
	}
	if !spec.Components.Schemas["B"].Properties["a"].Opaque {
		t.Fatalf("expected B.a to be Opaque")
	}
}

func TestDetectCircularSchemaRefsAllOf(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      allOf:
        - $ref: '#/components/schemas/Node'
        - type: object
          properties:
            id:
              type: integer
paths: {}
`)
	root, err := LoadFile("allof.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !spec.Components.Schemas["Node"].AllOf[0].Opaque {
		t.Fatalf("expected Node.allOf[0] to be Opaque")
	}
}

func TestDetectCircularSchemaRefsNestedAncestor(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    A:
      type: object
      properties:
        wrapper:
          type: object
          properties:
            back:
              $ref: '#/components/schemas/A'
paths: {}
`)
	root, err := LoadFile("nested.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, _ := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/A" {
		t.Fatalf("expected circular ref #/components/schemas/A, got %v", refs)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	back := spec.Components.Schemas["A"].Properties["wrapper"].Properties["back"]
	if back == nil || !back.Opaque {
		t.Fatalf("expected nested back property to be Opaque")
	}
}

func TestDetectCircularSchemaRefsOneOf(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      oneOf:
        - $ref: '#/components/schemas/Node'
        - type: string
paths: {}
`)
	root, err := LoadFile("oneof.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !spec.Components.Schemas["Node"].OneOf[0].Opaque {
		t.Fatalf("expected Node.oneOf[0] to be Opaque")
	}
}

func TestDetectCircularSchemaRefsAnyOf(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      anyOf:
        - type: object
          properties:
            child:
              $ref: '#/components/schemas/Node'
        - type: string
paths: {}
`)
	root, err := LoadFile("anyof.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !spec.Components.Schemas["Node"].AnyOf[0].Properties["child"].Opaque {
		t.Fatalf("expected Node.anyOf[0].properties.child to be Opaque")
	}
}

func TestDetectCircularSchemaRefsSourceLocation(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      type: object
      properties:
        next:
          $ref: '#/components/schemas/Node'
paths: {}
`)
	root, err := LoadFile("loc.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	_, diags := DetectCircularSchemaRefs(root)
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}
	loc := diags[0].SourceLocation
	if loc == nil {
		t.Fatalf("expected diagnostic to have a source location")
	}
	if loc.File != "loc.yaml" {
		t.Fatalf("expected file loc.yaml, got %q", loc.File)
	}
	if loc.Line != 7 {
		t.Fatalf("expected line 7, got %d", loc.Line)
	}
}

func TestDetectCircularSchemaRefsOperationRequestBody(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
paths:
  /nodes:
    post:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Node'
      responses:
        "200":
          description: ok
components:
  schemas:
    Node:
      type: object
      properties:
        self:
          $ref: '#/components/schemas/Node'
`)
	root, err := LoadFile("operation.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	node := spec.Components.Schemas["Node"]
	if node == nil || !node.Properties["self"].Opaque {
		t.Fatalf("expected Node.self to be Opaque")
	}
}

func TestDetectCircularSchemaRefsWebhook(t *testing.T) {
	data := []byte(`openapi: "3.1.0"
webhooks:
  newNode:
    post:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Node'
      responses:
        "200":
          description: ok
components:
  schemas:
    Node:
      type: object
      properties:
        self:
          $ref: '#/components/schemas/Node'
`)
	root, err := LoadFile("webhook.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	if !spec.Webhooks["newNode"].Post.RequestBody.Content["application/json"].Schema.Opaque {
		t.Fatalf("expected webhook request body schema to be Opaque")
	}
}

func TestDetectCircularSchemaRefsOpaqueRoundTrip(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
components:
  schemas:
    Node:
      type: object
      properties:
        next:
          $ref: '#/components/schemas/Node'
paths: {}
`)
	root, err := LoadFile("roundtrip.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !spec.Components.Schemas["Node"].Properties["next"].Opaque {
		t.Fatalf("expected Opaque before round-trip")
	}

	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var round Spec
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !round.Components.Schemas["Node"].Properties["next"].Opaque {
		t.Fatalf("expected Opaque after JSON round-trip")
	}
}

func TestDetectCircularSchemaRefsV2AdditionalProperties(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test
  version: "1.0"
paths: {}
definitions:
  Node:
    type: object
    additionalProperties: '#/definitions/Node'
`)
	root, err := LoadFile("v2-addprops.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/definitions/Node" {
		t.Fatalf("expected circular ref #/definitions/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, _, err := ConvertV2(root)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	node := spec.Components.Schemas["Node"]
	if node == nil {
		t.Fatalf("Node schema missing")
	}
	if !node.Opaque {
		t.Fatalf("expected Node schema to be Opaque when additionalProperties is a circular $ref")
	}
}

func TestDetectCircularSchemaRefsV31(t *testing.T) {
	data := []byte(`openapi: "3.1.0"
components:
  schemas:
    Node:
      type: object
      properties:
        next:
          $ref: '#/components/schemas/Node'
paths: {}
`)
	root, err := LoadFile("v31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected circular ref #/components/schemas/Node, got %v", refs)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one warning, got %v", diags)
	}

	spec, convertDiags, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	if len(convertDiags) != 1 {
		t.Fatalf("expected one conversion warning, got %v", convertDiags)
	}
	if !spec.Components.Schemas["Node"].Properties["next"].Opaque {
		t.Fatalf("expected next property to be Opaque")
	}
}

func TestMarkParameter_MarksCircularSchema(t *testing.T) {
	circular := map[string]struct{}{"#/components/schemas/Pet": {}}
	schema := &Schema{Ref: "#/components/schemas/Pet"}
	p := &Parameter{Schema: schema}
	markParameter(p, circular)
	if !schema.Opaque {
		t.Error("expected parameter schema to be marked Opaque")
	}
}

func TestMarkParameter_Nil(t *testing.T) {
	// Should not panic.
	markParameter(nil, nil)
}

func TestMarkHeader_MarksCircularSchema(t *testing.T) {
	circular := map[string]struct{}{"#/components/schemas/Pet": {}}
	schema := &Schema{Ref: "#/components/schemas/Pet"}
	h := &Header{Schema: schema}
	markHeader(h, circular)
	if !schema.Opaque {
		t.Error("expected header schema to be marked Opaque")
	}
}

func TestMarkCallback_MarksCircularPathItems(t *testing.T) {
	circular := map[string]struct{}{"#/components/schemas/Pet": {}}
	schema := &Schema{Ref: "#/components/schemas/Pet"}
	cb := Callback{
		"newPet": &PathItem{
			Post: &Operation{
				RequestBody: &RequestBody{
					Content: map[string]*MediaType{
						"application/json": {Schema: schema},
					},
				},
			},
		},
	}
	markCallback(cb, circular)
	if !schema.Opaque {
		t.Error("expected callback request body schema to be marked Opaque")
	}
}

func TestDetectCircularSchemaRefsDepthLimit(t *testing.T) {
	// Temporarily lower the recursion limit so we can exercise the
	// depth-limit path with a small, hand-written fixture.
	oldLimit := maxReachDepth
	maxReachDepth = 5
	defer func() { maxReachDepth = oldLimit }()

	var b strings.Builder
	b.WriteString(`openapi: "3.0.3"
paths: {}
components:
  schemas:
`)
	// A1 -> A2 -> A3 -> A4 -> A5 -> A6 -> A7 -> A1 forms a cycle,
	// but with maxReachDepth=5 the search is truncated before it closes.
	for i := 1; i <= 7; i++ {
		next := i + 1
		if next > 7 {
			next = 1
		}
		fmt.Fprintf(&b, "    A%d:\n      $ref: '#/components/schemas/A%d'\n", i, next)
	}

	root, err := LoadFile("depth.yaml", []byte(b.String()))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	if len(refs) != 0 {
		t.Fatalf("expected no proven circular refs when search is truncated, got %v", refs)
	}

	var depthWarnings int
	for _, d := range diags {
		if d.Summary == "Schema reference search depth exceeded" {
			depthWarnings++
		}
	}
	if depthWarnings == 0 {
		t.Fatalf("expected a depth-limit warning, got %v", diags)
	}
}

// TestDetectCircularSchemaRefsNoSpuriousScalarWarning locks in the M-24 fix:
// a literal string scalar (an example value, a description, an enum entry)
// whose text happens to equal a local schema pointer must NOT be reported as a
// circular reference, even when the scalar sits inside the referenced schema's
// own subtree. Only schema-bearing positions (e.g. additionalProperties) hold
// bare-string refs.
func TestDetectCircularSchemaRefsNoSpuriousScalarWarning(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
paths: {}
components:
  schemas:
    Node:
      type: object
      description: '#/components/schemas/Node'
      example: '#/components/schemas/Node'
      properties:
        next:
          $ref: '#/components/schemas/Node'
`)
	root, err := LoadFile("spurious-scalar.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refs, diags := DetectCircularSchemaRefs(root)
	// The only genuine cycle is the $ref under properties.next.
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected only #/components/schemas/Node to be circular, got %v", refs)
	}
	for _, d := range diags {
		if strings.Contains(d.Detail, "example") || strings.Contains(d.Detail, "description") {
			t.Fatalf("spurious circular warning for literal scalar: %s", d.Detail)
		}
	}
}

// TestDetectCircularSchemaRefsBareRefInSequence confirms a bare schema-pointer
// string inside a schema-bearing sequence (allOf) is still detected as a ref
// after the M-24 narrowing (sequence items inherit their enclosing key).
func TestDetectCircularSchemaRefsBareRefInSequence(t *testing.T) {
	data := []byte(`openapi: "3.0.3"
paths: {}
components:
  schemas:
    Node:
      type: object
      allOf:
        - '#/components/schemas/Node'
`)
	root, err := LoadFile("bare-ref-seq.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	refs, _ := DetectCircularSchemaRefs(root)
	if len(refs) != 1 || refs[0] != "#/components/schemas/Node" {
		t.Fatalf("expected #/components/schemas/Node circular via bare allOf ref, got %v", refs)
	}
}
