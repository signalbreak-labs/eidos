package parser

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateValidV30(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type: object
`)
	root, err := LoadFile("valid.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, convertDiags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range append(convertDiags, diags...) {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error diagnostic: %s", d.String())
		}
	}
}

func TestValidateMissingInfo(t *testing.T) {
	data := []byte(`openapi: 3.0.3
paths: {}
`)
	root, err := LoadFile("missing-info.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Missing required field", "missing the required 'info' object") {
		t.Fatalf("expected missing info diagnostic, got %v", diags)
	}
}

func TestValidateMissingInfoFields(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  description: An API with no title or version
paths: {}
`)
	root, err := LoadFile("missing-info-fields.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Missing required field", "info.title is required") {
		t.Fatalf("expected missing title diagnostic, got %v", diags)
	}
	if !hasDiag(diags, SeverityError, "Missing required field", "info.version is required") {
		t.Fatalf("expected missing version diagnostic, got %v", diags)
	}
	if spec.Info.SourceLocation.Line == 0 {
		t.Fatalf("expected info source location to have a line number")
	}
}

func TestValidateInvalidRefNonLocal(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: 'other.yaml#/components/schemas/Pet'
`)
	root, err := LoadFile("nonlocal.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Non-local $ref", "") {
		t.Fatalf("expected non-local ref diagnostic, got %v", diags)
	}
}

func TestValidateInvalidRefUnresolvable(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
`)
	root, err := LoadFile("unresolvable.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Unresolvable $ref", "") {
		t.Fatalf("expected unresolvable ref diagnostic, got %v", diags)
	}
}

func TestValidateUnsupportedKeyword(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
  unknownField: value
paths: {}
`)
	root, err := LoadFile("unsupported.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityWarning, "Unsupported keyword", "unknownField") {
		t.Fatalf("expected unsupported keyword diagnostic, got %v", diags)
	}
}

func TestValidateExtensionKeyNotWarned(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
  x-custom: value
paths: {}
`)
	root, err := LoadFile("extension.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range diags {
		if d.Summary == "Unsupported keyword" {
			t.Fatalf("vendor extension should not produce unsupported keyword warning, got %v", d)
		}
	}
}

func TestValidateTypeMismatchInfo(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info: not-an-object
paths: {}
`)
	root, err := LoadFile("info-mismatch.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Invalid OpenAPI structure", "info must be an object") {
		t.Fatalf("expected info type mismatch diagnostic, got %v", diags)
	}
}

func TestValidateTypeMismatchPaths(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: not-an-object
`)
	root, err := LoadFile("paths-mismatch.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Invalid OpenAPI structure", "paths must be an object") {
		t.Fatalf("expected paths type mismatch diagnostic, got %v", diags)
	}
}

func TestValidateTypeMismatchServers(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
servers: not-an-array
paths: {}
`)
	root, err := LoadFile("servers-mismatch.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Invalid OpenAPI structure", "servers must be an array") {
		t.Fatalf("expected servers type mismatch diagnostic, got %v", diags)
	}
}

func TestValidateSchemaTypeArrayInV30(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type:
        - string
        - null
`)
	root, err := LoadFile("type-array.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityError, "Type mismatch", "schema type as an array is only supported in OpenAPI 3.1") {
		t.Fatalf("expected schema array type mismatch diagnostic, got %v", diags)
	}
}

func TestValidateSchemaTypeArrayInV31(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type:
        - string
        - null
`)
	root, err := LoadFile("type-array31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range diags {
		if d.Summary == "Type mismatch" {
			t.Fatalf("OpenAPI 3.1 should allow schema type arrays, got %v", d)
		}
	}
}

// TestValidateV31ConditionalAndDependencyKeywords is a regression test for
// H-17: OpenAPI 3.1 (JSON Schema 2020-12) conditional, dependency, and content
// keywords — if/then/else, dependentSchemas, dependentRequired,
// unevaluatedItems, and contentSchema — must be recognized as schema keywords
// (no "Unsupported keyword" warning) and validation must descend into their
// schema subtrees so a bad keyword inside one is still flagged.
func TestValidateV31ConditionalAndDependencyKeywords(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Thing:
      type: object
      properties:
        kind:
          type: string
      if:
        properties:
          kind:
            const: active
      then:
        required: [kind]
      else:
        properties:
          note:
            type: string
      dependentSchemas:
        kind:
          required: [note]
      dependentRequired:
        kind: [note]
      unevaluatedItems:
        type: string
      contentMediaType: text/plain
      contentSchema:
        type: string
`)
	root, err := LoadFile("v31-keywords.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range diags {
		if d.Summary == "Unsupported keyword" {
			t.Fatalf("3.1 conditional/dependency/content keyword flagged as unsupported: %s — %s", d.Detail, d.SourceLocation)
		}
	}

	// Validation must descend into the new schema subtrees: a bad keyword
	// inside `then` (a schema) is still flagged, proving the descent table
	// reaches it (H-17).
	badData := []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Thing:
      type: object
      then:
        properties:
          kind:
            type: string
            bogusKeywordInThen: yes
`)
	badRoot, err := LoadFile("v31-keywords-bad.yaml", badData)
	if err != nil {
		t.Fatalf("LoadFile bad: %v", err)
	}
	badVer, _ := DetectVersion(badRoot)
	badSpec, _, err := ConvertV31(badRoot)
	if err != nil {
		t.Fatalf("ConvertV31 bad: %v", err)
	}
	badDiags := Validate(badRoot, badSpec, badVer)
	if !hasDiag(badDiags, SeverityWarning, "Unsupported keyword", "bogusKeywordInThen") {
		t.Fatalf("expected descent into 'then' to flag bogusKeywordInThen, got %v", badDiags)
	}
}

func TestValidateUnsupportedSchemaType(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type: unknown
`)
	root, err := LoadFile("unsupported-type.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityWarning, "Unsupported schema type", "unknown") {
		t.Fatalf("expected unsupported schema type diagnostic, got %v", diags)
	}
}

// TestValidateSchemaTypeNullByVersion asserts that `type: null` is a recognized
// JSON Schema primitive only in OpenAPI 3.1. In 3.0 the mechanism is
// `nullable: true`, so a standalone `type: null` is flagged (L-89).
func TestValidateSchemaTypeNullByVersion(t *testing.T) {
	mk := func(ver string) []byte {
		return []byte(fmt.Sprintf(`openapi: %s
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type: null
`, ver))
	}

	t.Run("3.0 warns", func(t *testing.T) {
		root, err := LoadFile("null30.yaml", mk("3.0.3"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		version, _ := DetectVersion(root)
		spec, _, err := ConvertV30(root)
		if err != nil {
			t.Fatalf("ConvertV30: %v", err)
		}
		diags := Validate(root, spec, version)
		if !hasDiag(diags, SeverityWarning, "Unsupported schema type", "null") {
			t.Fatalf("expected unsupported schema type 'null' diagnostic in 3.0, got %v", diags)
		}
	})

	t.Run("3.1 accepts", func(t *testing.T) {
		root, err := LoadFile("null31.yaml", mk("3.1.0"))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		version, _ := DetectVersion(root)
		spec, _, err := ConvertV31(root)
		if err != nil {
			t.Fatalf("ConvertV31: %v", err)
		}
		diags := Validate(root, spec, version)
		if hasDiag(diags, SeverityWarning, "Unsupported schema type", "null") {
			t.Fatalf("expected no unsupported schema type 'null' diagnostic in 3.1, got %v", diags)
		}
	})
}

// TestValidateWebhooks30 warns that `webhooks` is non-standard at the OpenAPI
// 3.0 root, matching the ConvertV30 warning (L-89: previously Validate silently
// accepted it while ConvertV30 warned).
func TestValidateWebhooks30(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
webhooks:
  newItem:
    post:
      operationId: newItem
      responses:
        "200":
          description: ok
`)
	root, err := LoadFile("webhooks30validate.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	diags := Validate(root, spec, version)
	if !hasDiag(diags, SeverityWarning, "Unsupported keyword", "webhooks") {
		t.Fatalf("expected 'Unsupported keyword' warning for webhooks in 3.0, got %v", diags)
	}
}

func TestValidateSwaggerV2(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
definitions:
  Pet:
    type: object
    properties:
      name:
        type: string
`)
	root, err := LoadFile("swagger.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, convertDiags, err := ConvertV2(root)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range append(convertDiags, diags...) {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error diagnostic: %s", d.String())
		}
	}
}

func TestValidateDiagnosticsHaveSourceLocation(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Missing'
`)
	root, err := LoadFile("loc.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range diags {
		if d.Severity != SeverityError {
			continue
		}
		if d.SourceLocation.File == "" || d.SourceLocation.Line == 0 {
			t.Fatalf("expected error diagnostic to have file:line, got %+v", d)
		}
	}
}

func TestValidateNoFalsePositiveNamedMaps(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
servers:
  - url: https://example.com
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
components:
  schemas:
    Animal:
      type: object
    Pet:
      type: object
      properties:
        name:
          type: string
        age:
          type: integer
      allOf:
        - $ref: '#/components/schemas/Animal'
  responses:
    NotFound:
      description: not found
  parameters:
    LimitParam:
      name: limit
      in: query
      schema:
        type: integer
  examples:
    PetExample:
      value:
        name: fluffy
  requestBodies:
    PetBody:
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Pet'
  headers:
    X-Rate-Limit:
      schema:
        type: integer
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/oauth/authorize
          tokenUrl: https://example.com/oauth/token
          scopes:
            'read:pets': read your pets
  links:
    PetLink:
      operationId: getPet
  callbacks:
    myEvent:
      '{$request.body#/url}':
        post:
          summary: webhook
          requestBody:
            content:
              application/json:
                schema:
                  $ref: '#/components/schemas/Pet'
          responses:
            '200':
              description: ok
`)
	root, err := LoadFile("namedmaps.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, convertDiags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range append(convertDiags, diags...) {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error: %s", d.String())
		}
		if d.Summary == "Unsupported keyword" {
			t.Fatalf("unexpected unsupported-keyword warning for named-map key: %s", d.String())
		}
	}
}

func TestValidateNoFalsePositiveSecuritySchemeTypeAndHashRef(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
  description: '# See documentation'
paths:
  /pets:
    get:
      summary: '#pets'
      description: '#not-a-ref'
      responses:
        '200':
          description: '#ok'
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: X-API-Key
    http:
      type: http
      scheme: bearer
`)
	root, err := LoadFile("hash.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, version)
	for _, d := range diags {
		if d.Summary == "Unsupported schema type" {
			t.Fatalf("security scheme 'type' should not be validated as schema type: %s", d.String())
		}
		if d.Summary == "Non-local $ref" || d.Summary == "Unresolvable $ref" {
			t.Fatalf("description/summary scalar starting with # should not be treated as $ref: %s", d.String())
		}
	}
}

func TestValidateNilSpec(t *testing.T) {
	if diags := Validate(nil, nil, Version3_0); diags != nil {
		t.Fatalf("expected nil, got %v", diags)
	}
}

func TestValidateSchemaTypeFile(t *testing.T) {
	tests := []struct {
		name        string
		version     Version
		convert     func(Node, ...ConvertOption) (*Spec, []Diagnostic, error)
		wantWarning bool
	}{
		{"v2.0", Version2_0, ConvertV2, false},
		{"v3.0", Version3_0, ConvertV30, true},
		{"v3.1", Version3_1, ConvertV31, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data []byte
			switch tt.version {
			case Version2_0:
				data = []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
definitions:
  Pet:
    type: file
`)
			case Version3_0:
				data = []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type: file
`)
			case Version3_1:
				data = []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type: file
`)
			}

			root, err := LoadFile("file-"+tt.name+".yaml", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}

			spec, _, err := tt.convert(root)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			diags := Validate(root, spec, tt.version)
			got := hasDiag(diags, SeverityWarning, "Unsupported schema type", "file")
			if got != tt.wantWarning {
				t.Fatalf("type: file in %s: want warning=%v, got diags=%v", tt.name, tt.wantWarning, diags)
			}
		})
	}
}

func TestValidateVersionSpecificKeywords(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		version     Version
		convert     func(Node, ...ConvertOption) (*Spec, []Diagnostic, error)
		key         string
		wantWarning bool
	}{
		{
			name: "openapi rejected in v2.0",
			data: []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
openapi: 3.0.3
`),
			version:     Version2_0,
			convert:     ConvertV2,
			key:         "openapi",
			wantWarning: true,
		},
		{
			name: "host rejected in v3.0",
			data: []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
host: example.com
`),
			version:     Version3_0,
			convert:     ConvertV30,
			key:         "host",
			wantWarning: true,
		},
		{
			name: "host accepted in v2.0",
			data: []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
host: example.com
`),
			version:     Version2_0,
			convert:     ConvertV2,
			key:         "host",
			wantWarning: false,
		},
		{
			name: "basePath rejected in v3.0",
			data: []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
paths: {}
basePath: /v1
`),
			version:     Version3_0,
			convert:     ConvertV30,
			key:         "basePath",
			wantWarning: true,
		},
		{
			name: "basePath rejected in v3.1",
			data: []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
paths: {}
basePath: /v1
`),
			version:     Version3_1,
			convert:     ConvertV31,
			key:         "basePath",
			wantWarning: true,
		},
		{
			name: "basePath accepted in v2.0",
			data: []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
basePath: /v1
`),
			version:     Version2_0,
			convert:     ConvertV2,
			key:         "basePath",
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := LoadFile("version-keyword.yaml", tt.data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}

			spec, _, err := tt.convert(root)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			diags := Validate(root, spec, tt.version)
			got := hasDiag(diags, SeverityWarning, "Unsupported keyword", tt.key)
			if got != tt.wantWarning {
				t.Fatalf("%s: want warning=%v for %q, got diags=%v", tt.name, tt.wantWarning, tt.key, diags)
			}
		})
	}
}

func TestValidateTypeMismatchInfoTitleObject(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title:
    en: My API
  version: "1.0"
paths: {}
`)
	root, err := LoadFile("info-title-object.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, Version3_0)
	if !hasDiag(diags, SeverityError, "Type mismatch", "info.title must be a string, got an object") {
		t.Fatalf("expected info.title object mismatch diagnostic, got %v", diags)
	}
}

func TestValidateTypeMismatchInfoTitleArray(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title:
    - My API
  version: "1.0"
paths: {}
`)
	root, err := LoadFile("info-title-array.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, Version3_0)
	if !hasDiag(diags, SeverityError, "Type mismatch", "info.title must be a string, got an array") {
		t.Fatalf("expected info.title array mismatch diagnostic, got %v", diags)
	}
}

func TestValidateMissingPathsV30(t *testing.T) {
	data := []byte(`openapi: 3.0.3
info:
  title: Test API
  version: "1.0"
`)
	root, err := LoadFile("missing-paths-v30.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, Version3_0)
	if !hasDiag(diags, SeverityError, "Missing required field", "paths is required") {
		t.Fatalf("expected missing paths diagnostic, got %v", diags)
	}
}

func TestValidateMissingPathsV20(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
`)
	root, err := LoadFile("missing-paths-v20.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV2(root)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	diags := Validate(root, spec, Version2_0)
	if !hasDiag(diags, SeverityError, "Missing required field", "paths is required") {
		t.Fatalf("expected missing paths diagnostic, got %v", diags)
	}
}

func TestValidateMissingPathsV31Optional(t *testing.T) {
	data := []byte(`openapi: 3.1.0
info:
  title: Test API
  version: "1.0"
`)
	root, err := LoadFile("missing-paths-v31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	version, _ := DetectVersion(root)
	spec, _, err := ConvertV31(root)
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}

	diags := Validate(root, spec, version)
	if hasDiag(diags, SeverityError, "Missing required field", "paths is required") {
		t.Fatalf("paths should not be required in 3.1, got %v", diags)
	}
}

func TestValidateMissingOpenAPIVersion(t *testing.T) {
	data := []byte(`info:
  title: Test API
  version: "1.0"
paths: {}
`)
	root, err := LoadFile("missing-openapi.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}

	diags := Validate(root, spec, Version3_0)
	if !hasDiag(diags, SeverityError, "Missing required field", "openapi version string is required") {
		t.Fatalf("expected missing openapi version diagnostic, got %v", diags)
	}
}

func TestValidateMissingSwaggerVersion(t *testing.T) {
	data := []byte(`info:
  title: Test API
  version: "1.0"
paths: {}
definitions:
  Pet:
    type: object
`)
	root, err := LoadFile("missing-swagger.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, _, err := ConvertV2(root)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	diags := Validate(root, spec, Version2_0)
	if !hasDiag(diags, SeverityError, "Missing required field", "swagger version string is required") {
		t.Fatalf("expected missing swagger version diagnostic, got %v", diags)
	}
}

func hasDiag(diags []Diagnostic, severity Severity, summary, detailSubstr string) bool {
	for _, d := range diags {
		if d.Severity != severity {
			continue
		}
		if summary != "" && d.Summary != summary {
			continue
		}
		if detailSubstr != "" && !strings.Contains(d.Detail, detailSubstr) {
			continue
		}
		return true
	}
	return false
}
