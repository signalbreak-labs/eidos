package parser

import (
	"strings"
	"testing"
)

func TestResolveLocalRefRoot(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
`)
	node, err := LoadFile("root.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "root.yaml", Line: 1, Column: 1}
	got, diags := ResolveLocalRef(node, "#", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if got != node {
		t.Fatalf("expected root node")
	}
}

func TestResolveLocalRefComponentSchema(t *testing.T) {
	data := []byte(`openapi: 3.0.3
components:
  schemas:
    Pet:
      type: object
      properties:
        id:
          type: integer
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
`)
	node, err := LoadFile("schemas.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "schemas.yaml", Line: 10, Column: 10}
	got, diags := ResolveLocalRef(node, "#/components/schemas/Pet", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "type")
	if entry == nil {
		t.Fatalf("resolved node missing type field")
	}
	typ, _ := nodeString(entry.Value)
	if typ != "object" {
		t.Fatalf("expected type object, got %q", typ)
	}
}

func TestResolveLocalRefPathItem(t *testing.T) {
	data := []byte(`openapi: 3.0.3
paths:
  /pets:
    get:
      operationId: listPets
`)
	node, err := LoadFile("paths.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "paths.yaml", Line: 5, Column: 5}
	got, diags := ResolveLocalRef(node, "#/paths/~1pets", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "get")
	if entry == nil {
		t.Fatalf("resolved path item missing get field")
	}
}

func TestResolveLocalRefEscapedTokens(t *testing.T) {
	// A key containing '/' and '~' is encoded as '~1' and '~0' respectively.
	data := []byte(`openapi: 3.0.3
components:
  schemas:
    a/b~c:
      type: string
`)
	node, err := LoadFile("escaped.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "escaped.yaml", Line: 4, Column: 5}
	got, diags := ResolveLocalRef(node, "#/components/schemas/a~1b~0c", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "type")
	if entry == nil {
		t.Fatalf("resolved node missing type field")
	}
	typ, _ := nodeString(entry.Value)
	if typ != "string" {
		t.Fatalf("expected type string, got %q", typ)
	}
}

func TestResolveLocalRefArrayIndex(t *testing.T) {
	data := []byte(`openapi: 3.0.3
servers:
  - url: https://one.example.com
  - url: https://two.example.com
`)
	node, err := LoadFile("array.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "array.yaml", Line: 3, Column: 3}
	got, diags := ResolveLocalRef(node, "#/servers/1", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "url")
	if entry == nil {
		t.Fatalf("resolved node missing url field")
	}
	url, _ := nodeString(entry.Value)
	if url != "https://two.example.com" {
		t.Fatalf("expected second server url, got %q", url)
	}
}

func TestResolveLocalRefNonLocal(t *testing.T) {
	node, err := LoadFile("empty.yaml", []byte("openapi: 3.0.3\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "empty.yaml", Line: 2, Column: 5}
	got, diags := ResolveLocalRef(node, "other.yaml#/components/schemas/Pet", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for non-local ref")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Non-local $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
	if diags[0].SourceLocation == nil || *diags[0].SourceLocation != refLoc {
		t.Fatalf("diagnostic location mismatch: got %+v, want %+v", diags[0].SourceLocation, refLoc)
	}
}

func TestResolveLocalRefInvalidPointer(t *testing.T) {
	node, err := LoadFile("empty.yaml", []byte("openapi: 3.0.3\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "empty.yaml", Line: 2, Column: 5}
	got, diags := ResolveLocalRef(node, "#not-a-pointer", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for invalid pointer")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Invalid JSON Pointer" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestResolveLocalRefInvalidEscape(t *testing.T) {
	node, err := LoadFile("empty.yaml", []byte("openapi: 3.0.3\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "empty.yaml", Line: 2, Column: 5}
	got, diags := ResolveLocalRef(node, "#/components/schemas/foo~2", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for invalid escape")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Invalid JSON Pointer" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestResolveLocalRefMissingSchema(t *testing.T) {
	data := []byte(`openapi: 3.0.3
components:
  schemas:
    Pet:
      type: object
`)
	node, err := LoadFile("missing.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "missing.yaml", Line: 5, Column: 5}
	got, diags := ResolveLocalRef(node, "#/components/schemas/Owner", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for missing schema")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
	if diags[0].SourceLocation == nil || *diags[0].SourceLocation != refLoc {
		t.Fatalf("diagnostic location mismatch: got %+v, want %+v", diags[0].SourceLocation, refLoc)
	}
}

func TestResolveLocalRefDiagnosticLocationsAreCopied(t *testing.T) {
	node, err := LoadFile("empty.yaml", []byte("openapi: 3.0.3\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "empty.yaml", Line: 2, Column: 5}
	cases := []string{
		"other.yaml#/components/schemas/Pet",
		"#not-a-pointer",
		"#/components/schemas/Missing",
	}
	for _, ref := range cases {
		_, diags := ResolveLocalRef(node, ref, refLoc)
		if len(diags) != 1 {
			t.Fatalf("ref %q: expected one diagnostic, got %v", ref, diags)
		}
		if diags[0].SourceLocation == nil {
			t.Fatalf("ref %q: diagnostic has no source location", ref)
		}
		if *diags[0].SourceLocation != refLoc {
			t.Fatalf("ref %q: diagnostic location mismatch: got %+v, want %+v", ref, diags[0].SourceLocation, refLoc)
		}
	}
}

func TestResolveLocalRefMissingPath(t *testing.T) {
	data := []byte(`openapi: 3.0.3
paths:
  /pets:
    get:
      operationId: listPets
`)
	node, err := LoadFile("missing.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "missing.yaml", Line: 5, Column: 5}
	got, diags := ResolveLocalRef(node, "#/paths/~1owners", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for missing path")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestResolveLocalRefArrayIndexOutOfBounds(t *testing.T) {
	data := []byte(`openapi: 3.0.3
servers:
  - url: https://one.example.com
`)
	node, err := LoadFile("array.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "array.yaml", Line: 3, Column: 3}
	got, diags := ResolveLocalRef(node, "#/servers/5", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for out of bounds index")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestResolveLocalRefTraverseNullNode(t *testing.T) {
	// Construct a map whose value is a nil Node interface to exercise the
	// default-case nil branch in ResolveLocalRef.
	root := &MapNode{
		SourceLocation: SourceLocation{File: "nil-node.yaml", Line: 1, Column: 1},
		Entries: []MapEntry{{
			Key:   &ScalarNode{Value: "info", Raw: "info", SourceLocation: SourceLocation{File: "nil-node.yaml", Line: 1, Column: 1}},
			Value: nil,
		}},
	}

	refLoc := SourceLocation{File: "nil-node.yaml", Line: 1, Column: 1}
	got, diags := ResolveLocalRef(root, "#/info/title", refLoc)
	if got != nil {
		t.Fatalf("expected nil node when traversing into nil node")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
	if !strings.Contains(diags[0].Detail, "Cannot traverse into null") {
		t.Fatalf("expected null traversal detail, got %q", diags[0].Detail)
	}
}

func TestResolveLocalRefTraverseScalar(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
`)
	node, err := LoadFile("scalar.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "scalar.yaml", Line: 3, Column: 3}
	got, diags := ResolveLocalRef(node, "#/info/title/extra", refLoc)
	if got != nil {
		t.Fatalf("expected nil node when traversing into scalar")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestResolveLocalRefNilRoot(t *testing.T) {
	refLoc := SourceLocation{File: "nil.yaml", Line: 1, Column: 1}
	got, diags := ResolveLocalRef(nil, "#/components/schemas/Pet", refLoc)
	if got != nil {
		t.Fatalf("expected nil node for nil root")
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unresolvable $ref" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
	if !strings.Contains(diags[0].Detail, "nil document") {
		t.Fatalf("expected nil document detail, got %q", diags[0].Detail)
	}
}

func TestResolveLocalRefSwaggerDefinitions(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
definitions:
  Pet:
    type: object
    properties:
      id:
        type: integer
`)
	node, err := LoadFile("swagger.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "swagger.yaml", Line: 10, Column: 5}
	got, diags := ResolveLocalRef(node, "#/definitions/Pet", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "type")
	if entry == nil {
		t.Fatalf("resolved node missing type field")
	}
	typ, _ := nodeString(entry.Value)
	if typ != "object" {
		t.Fatalf("expected type object, got %q", typ)
	}
}

func TestResolveLocalRefToNull(t *testing.T) {
	data := []byte(`openapi: 3.0.3
components:
  schemas:
    Null: null
`)
	node, err := LoadFile("null.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "null.yaml", Line: 4, Column: 5}
	got, diags := ResolveLocalRef(node, "#/components/schemas/Null", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if got == nil {
		t.Fatalf("expected the null scalar node, got nil")
	}
	sn, ok := got.(*ScalarNode)
	if !ok {
		t.Fatalf("expected ScalarNode, got %T", got)
	}
	if sn.Value != nil {
		t.Fatalf("expected nil scalar value, got %v", sn.Value)
	}
}

func TestResolveLocalRefTrailingSlash(t *testing.T) {
	// Edge case: an empty-string key is not a realistic OpenAPI schema map key, but JSON Pointer
	// allows it. This fixture exercises that compliance path by resolving a trailing slash in the
	// pointer (e.g., "#/components/schemas/") to a property whose name is the empty string.
	data := []byte(`openapi: 3.0.3
components:
  schemas:
    "":
      type: string
`)
	node, err := LoadFile("empty-key.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "empty-key.yaml", Line: 4, Column: 5}
	got, diags := ResolveLocalRef(node, "#/components/schemas/", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "type")
	if entry == nil {
		t.Fatalf("resolved node missing type field")
	}
	typ, _ := nodeString(entry.Value)
	if typ != "string" {
		t.Fatalf("expected type string, got %q", typ)
	}
}

func TestResolveLocalRefDeepPath(t *testing.T) {
	data := []byte(`openapi: 3.0.3
paths:
  /pets:
    get:
      parameters:
        - name: limit
          in: query
          type: integer
`)
	node, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	refLoc := SourceLocation{File: "deep.yaml", Line: 8, Column: 12}
	got, diags := ResolveLocalRef(node, "#/paths/~1pets/get/parameters/0", refLoc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}

	m, ok := got.(*MapNode)
	if !ok {
		t.Fatalf("expected MapNode, got %T", got)
	}
	entry := findMapEntry(m, "name")
	if entry == nil {
		t.Fatalf("resolved parameter missing name field")
	}
	name, _ := nodeString(entry.Value)
	if name != "limit" {
		t.Fatalf("expected parameter name limit, got %q", name)
	}
}
