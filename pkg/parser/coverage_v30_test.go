package parser

import (
	"strings"
	"testing"
)

// TestScalarAnySliceSequence covers the sequence path of scalarAnySlice via a
// schema-level enum, which the server-variable enum fixtures do not reach.
func TestScalarAnySliceSequence(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Enum API
  version: "1.0.0"
paths: {}
components:
  schemas:
    Pet:
      type: string
      enum:
        - cat
        - dog
`)
	node, err := LoadFile("enum.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	pet := spec.Components.Schemas["Pet"]
	if len(pet.Enum) != 2 || pet.Enum[0] != "cat" || pet.Enum[1] != "dog" {
		t.Errorf("enum = %v, want [cat dog]", pet.Enum)
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %+v", diags)
	}
}

// TestWarnScalarTypeMismatchNil asserts the nil-node guard of
// warnScalarTypeMismatch: a missing optional field must not emit a diagnostic.
func TestWarnScalarTypeMismatchNil(t *testing.T) {
	c := &v30Converter{}
	c.warnScalarTypeMismatch(nil, "description", "string")
	if len(c.diags) != 0 {
		t.Errorf("nil node must not warn, got %+v", c.diags)
	}
}

// TestConvertAdditionalPropertiesSchema covers the schema branch of
// convertAdditionalProperties (a non-boolean value converts as a schema).
func TestConvertAdditionalPropertiesSchema(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: AP API
  version: "1.0.0"
paths: {}
components:
  schemas:
    Bag:
      type: object
      additionalProperties:
        type: string
`)
	node, err := LoadFile("ap.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	bag := spec.Components.Schemas["Bag"]
	ap, ok := bag.AdditionalProperties.(*Schema)
	if !ok {
		t.Fatalf("additionalProperties = %T, want *Schema", bag.AdditionalProperties)
	}
	if ap.Type != "string" {
		t.Errorf("additionalProperties type = %v, want string", ap.Type)
	}
}

// TestConvertMediaTypeBranches drives the example/examples/encoding branches of
// convertMediaType plus the non-map error path.
func TestConvertMediaTypeBranches(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Media API
  version: "1.0.0"
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          application/json:
            schema:
              type: object
            example:
              id: 1
            examples:
              one:
                value:
                  id: 1
            encoding:
              id:
                contentType: text/plain
      responses:
        "200":
          description: ok
`)
	node, err := LoadFile("media.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	mt := spec.Paths["/upload"].Post.RequestBody.Content["application/json"]
	if mt.Example == nil {
		t.Error("expected example to be preserved")
	}
	if len(mt.Examples) != 1 {
		t.Errorf("examples = %d, want 1", len(mt.Examples))
	}
	if mt.Encoding["id"] == nil || mt.Encoding["id"].ContentType != "text/plain" {
		t.Errorf("encoding = %+v", mt.Encoding)
	}

	// Non-map media type → nil with a warning.
	c := &v30Converter{}
	if got := c.convertMediaType(&ScalarNode{Value: "x"}); got != nil {
		t.Errorf("non-map media type = %+v, want nil", got)
	}
	if len(c.diags) == 0 {
		t.Error("expected a warning for non-map media type")
	}
}

// TestConvertOAuthFlowsBranches drives the password and clientCredentials
// branches of convertOAuthFlows plus the non-map error path.
func TestConvertOAuthFlowsBranches(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: OAuth API
  version: "1.0.0"
paths: {}
components:
  securitySchemes:
    oauth:
      type: oauth2
      flows:
        password:
          tokenUrl: https://example.com/token
          scopes:
            read: Read access
        clientCredentials:
          tokenUrl: https://example.com/token
        authorizationCode:
          authorizationUrl: https://example.com/authorize
          tokenUrl: https://example.com/token
`)
	node, err := LoadFile("oauth.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	flows := spec.Components.SecuritySchemes["oauth"].Flows
	if flows.Password == nil || flows.Password.TokenURL != "https://example.com/token" {
		t.Errorf("password flow = %+v", flows.Password)
	}
	if flows.ClientCredentials == nil {
		t.Error("expected clientCredentials flow")
	}
	if flows.AuthorizationCode == nil {
		t.Error("expected authorizationCode flow")
	}
	if len(flows.Password.Scopes) != 1 {
		t.Errorf("password scopes = %+v", flows.Password.Scopes)
	}

	// Non-map flows → nil with a warning.
	c := &v30Converter{}
	if got := c.convertOAuthFlows(&ScalarNode{Value: "x"}); got != nil {
		t.Errorf("non-map flows = %+v, want nil", got)
	}
	if len(c.diags) == 0 {
		t.Error("expected a warning for non-map flows")
	}
}

// TestParseHeaderBranches drives the bare-string $ref, non-map, and schema-field
// branches of parseHeader.
func TestParseHeaderBranches(t *testing.T) {
	// Bare-string $ref header.
	h, diags := parseHeader(&ScalarNode{Value: "#/headers/RateHeader"})
	if h == nil || h.Ref != "#/headers/RateHeader" {
		t.Errorf("ref header = %+v", h)
	}
	if len(diags) != 0 {
		t.Errorf("ref header diagnostics = %+v", diags)
	}

	// Non-map header → invalid-type diagnostic.
	h, diags = parseHeader(&ScalarNode{Value: 42})
	if h != nil || len(diags) == 0 {
		t.Errorf("non-map header = %+v, diags = %+v", h, diags)
	}

	// Header with an explicit schema field.
	h, diags = parseHeader(&MapNode{Entries: []MapEntry{
		{Key: &ScalarNode{Value: "schema"}, Value: &MapNode{Entries: []MapEntry{
			{Key: &ScalarNode{Value: "type"}, Value: &ScalarNode{Value: "string"}},
		}}},
	}})
	if h == nil || h.Schema == nil {
		t.Fatalf("schema header = %+v", h)
	}
	if len(diags) != 0 {
		t.Errorf("schema header diagnostics = %+v", diags)
	}
}

// TestParseHeaderMapSkipsEmptyKey covers the empty-key skip in parseHeaderMap.
func TestParseHeaderMapSkipsEmptyKey(t *testing.T) {
	headers, diags := parseHeaderMap(&MapNode{Entries: []MapEntry{
		{Key: &ScalarNode{Value: ""}, Value: &ScalarNode{Value: "#/headers/RateHeader"}},
		{Key: &ScalarNode{Value: "X-Rate"}, Value: &ScalarNode{Value: "#/headers/RateHeader"}},
	}})
	if len(headers) != 1 || headers["X-Rate"] == nil {
		t.Errorf("headers = %+v, want only X-Rate", headers)
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics = %+v", diags)
	}
}

// TestParseExampleMapNonMap covers the non-map error path of parseExampleMap.
func TestParseExampleMapNonMap(t *testing.T) {
	examples, diags := parseExampleMap(&ScalarNode{Value: "x"})
	if examples != nil || len(diags) == 0 {
		t.Errorf("non-map examples = %+v, diags = %+v", examples, diags)
	}
	if !strings.Contains(diags[0].Detail, "examples") {
		t.Errorf("diagnostic detail = %q", diags[0].Detail)
	}
}

// TestFillSequenceMappingFirstValue_BlockValue covers the block-parse path of
// fillSequenceMappingFirstValue: a sequence mapping item whose first key has no
// inline value and is followed by an indented block.
func TestFillSequenceMappingFirstValue_BlockValue(t *testing.T) {
	data := []byte("items:\n  - config:\n      enabled: true\n")
	node, err := LoadFile("seqmap.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root, ok := node.(*MapNode)
	if !ok {
		t.Fatalf("expected *MapNode, got %T", node)
	}
	items, ok := findKey(root, "items").(*SequenceNode)
	if !ok {
		t.Fatalf("expected items to be a *SequenceNode, got %T", findKey(root, "items"))
	}
	if len(items.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items.Items))
	}
	m, ok := items.Items[0].(*MapNode)
	if !ok {
		t.Fatalf("expected item to be a *MapNode, got %T", items.Items[0])
	}
	config, ok := findKey(m, "config").(*MapNode)
	if !ok {
		t.Fatalf("expected config to be a *MapNode, got %T", findKey(m, "config"))
	}
	if v := findKey(config, "enabled"); v == nil {
		t.Error("missing enabled key in nested block")
	}
}

// TestFillSequenceMappingFirstValue_NullValue covers the null-value fallback of
// fillSequenceMappingFirstValue: a sequence mapping item whose first key has no
// inline value and no following indented block.
func TestFillSequenceMappingFirstValue_NullValue(t *testing.T) {
	data := []byte("items:\n  - config:\n  - other: 1\n")
	node, err := LoadFile("seqmap-null.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	root := node.(*MapNode)
	items := findKey(root, "items").(*SequenceNode)
	if len(items.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items.Items))
	}
	m := items.Items[0].(*MapNode)
	config := findKey(m, "config")
	if s, ok := config.(*ScalarNode); !ok || s.Value != nil {
		t.Errorf("expected null scalar config, got %#v", config)
	}
}
