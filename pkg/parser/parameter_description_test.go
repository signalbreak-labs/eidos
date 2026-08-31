package parser

import "testing"

// TestConvertV30ParameterDescriptionFallback covers the parameter-description
// fallback: a parameter may declare its description on the parameter object or
// on its schema, and the schema's must be used when the object's is absent.
// The object-level description always wins when both are present.
func TestConvertV30ParameterDescriptionFallback(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Parameter Description Test API
  version: "1.0.0"
paths:
  /items:
    get:
      operationId: listItems
      parameters:
        - name: schemaOnly
          in: query
          schema:
            type: string
            description: Described on the schema
        - name: bothLevels
          in: query
          description: Described on the object
          schema:
            type: string
            description: Described on the schema
        - name: neither
          in: query
          schema:
            type: string
      responses:
        '200':
          description: ok
`)

	node, err := LoadFile("params.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV30(node)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	pi := spec.Paths["/items"]
	if pi == nil || pi.Get == nil {
		t.Fatal("path /items missing")
	}
	byName := map[string]*Parameter{}
	for i := range pi.Get.Parameters {
		byName[pi.Get.Parameters[i].Name] = &pi.Get.Parameters[i]
	}
	cases := []struct {
		name string
		want string
	}{
		{"schemaOnly", "Described on the schema"},
		{"bothLevels", "Described on the object"},
		{"neither", ""},
	}
	for _, tc := range cases {
		p := byName[tc.name]
		if p == nil {
			t.Fatalf("parameter %q missing", tc.name)
		}
		if p.Description != tc.want {
			t.Errorf("%s: description = %q, want %q", tc.name, p.Description, tc.want)
		}
	}
}

// TestConvertV2ParameterDescriptionFallback is the Swagger 2.0 twin of the
// v30 fallback test: a schema-level description fills in for an absent
// object-level one, and the object-level description wins when both exist.
func TestConvertV2ParameterDescriptionFallback(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Parameter Description Test API
  version: "1.0.0"
paths:
  /items:
    get:
      operationId: listItems
      parameters:
        - name: schemaOnly
          in: query
          type: string
          schema:
            type: string
            description: Described on the schema
        - name: bothLevels
          in: query
          type: string
          description: Described on the object
          schema:
            type: string
            description: Described on the schema
        - name: neither
          in: query
          type: string
      responses:
        '200':
          description: ok
`)

	node, err := LoadFile("params-v2.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	pi := spec.Paths["/items"]
	if pi == nil || pi.Get == nil {
		t.Fatal("path /items missing")
	}
	byName := map[string]*Parameter{}
	for i := range pi.Get.Parameters {
		byName[pi.Get.Parameters[i].Name] = &pi.Get.Parameters[i]
	}
	cases := []struct {
		name string
		want string
	}{
		{"schemaOnly", "Described on the schema"},
		{"bothLevels", "Described on the object"},
		{"neither", ""},
	}
	for _, tc := range cases {
		p := byName[tc.name]
		if p == nil {
			t.Fatalf("parameter %q missing", tc.name)
		}
		if p.Description != tc.want {
			t.Errorf("%s: description = %q, want %q", tc.name, p.Description, tc.want)
		}
	}
}
