package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) Node {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	node, err := LoadFile(path, data)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return node
}

func TestConvertV2Mycloud(t *testing.T) {
	node := loadFixture(t, "mycloud-v2.yaml")

	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error diagnostic: %s", d)
		}
	}

	if spec.Swagger != "2.0" {
		t.Errorf("swagger = %q, want %q", spec.Swagger, "2.0")
	}
	if spec.Info == nil {
		t.Fatal("info is nil")
	}
	if spec.Info.Title != "Mycloud" {
		t.Errorf("info.title = %q, want %q", spec.Info.Title, "Mycloud")
	}
	if spec.Info.Version != "1.0.0" {
		t.Errorf("info.version = %q, want %q", spec.Info.Version, "1.0.0")
	}

	wantServers := []string{
		"http://api.mycloud.example/v2",
		"https://api.mycloud.example/v2",
	}
	if len(spec.Servers) != len(wantServers) {
		t.Fatalf("servers = %v, want %v", spec.Servers, wantServers)
	}
	for i, want := range wantServers {
		if spec.Servers[i].URL != want {
			t.Errorf("server[%d].url = %q, want %q", i, spec.Servers[i].URL, want)
		}
	}

	if spec.Components == nil {
		t.Fatal("components is nil")
	}
	if spec.Components.Schemas == nil {
		t.Fatal("components.schemas is nil")
	}
	for _, name := range []string{"Pet", "Error"} {
		if _, ok := spec.Components.Schemas[name]; !ok {
			t.Errorf("missing schema %q", name)
		}
	}
	pet := spec.Components.Schemas["Pet"]
	if pet == nil || pet.Type != "object" {
		t.Fatalf("Pet schema missing or wrong type: %v", pet)
	}
	if !reflect.DeepEqual(pet.Required, []string{"id", "name"}) {
		t.Errorf("Pet.required = %v, want [id name]", pet.Required)
	}
	if pet.Properties["id"] == nil || pet.Properties["id"].Type != "integer" {
		t.Errorf("Pet.properties.id missing or wrong type")
	}

	if spec.Components.SecuritySchemes == nil {
		t.Fatal("components.securitySchemes is nil")
	}
	apiKey, ok := spec.Components.SecuritySchemes["api_key"]
	if !ok {
		t.Fatal("missing securityScheme api_key")
	}
	if apiKey.Type != "apiKey" || apiKey.Name != "api_key" || apiKey.In != "header" {
		t.Errorf("api_key scheme = %+v, want apiKey/header", apiKey)
	}

	oauth, ok := spec.Components.SecuritySchemes["mycloud_auth"]
	if !ok {
		t.Fatal("missing securityScheme mycloud_auth")
	}
	if oauth.Type != "oauth2" {
		t.Errorf("mycloud_auth.type = %q, want oauth2", oauth.Type)
	}
	if oauth.Flows == nil || oauth.Flows.Implicit == nil {
		t.Fatal("mycloud_auth oauth2 implicit flow missing")
	}
	if oauth.Flows.Implicit.AuthorizationURL != "http://api.mycloud.example/oauth/dialog" {
		t.Errorf("implicit authorizationUrl = %q, want %q", oauth.Flows.Implicit.AuthorizationURL, "http://api.mycloud.example/oauth/dialog")
	}
	if !reflect.DeepEqual(oauth.Flows.Implicit.Scopes, map[string]string{
		"write:pets": "modify pets in your account",
		"read:pets":  "read your pets",
	}) {
		t.Errorf("implicit scopes = %v", oauth.Flows.Implicit.Scopes)
	}

	if len(spec.Paths) == 0 {
		t.Fatal("paths missing or empty")
	}
	if spec.Paths["/pets/{petId}"].Get == nil {
		t.Error("missing GET /pets/{petId}")
	}
	if spec.Paths["/pets"].Post == nil {
		t.Error("missing POST /pets")
	}
	if spec.Paths["/pets"].Get == nil || spec.Paths["/pets"].Get.OperationID != "listPets" {
		t.Errorf("GET /pets operation wrong: %v", spec.Paths["/pets"].Get)
	}
}

func TestConvertV2Servers(t *testing.T) {
	cases := []struct {
		name             string
		spec             string
		want             []string
		wantDiag         string
		wantDiagSeverity Severity
	}{
		{
			name: "host basePath and schemes",
			spec: `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
host: api.example.com
basePath: /v1
schemes:
  - https
  - http
`,
			want: []string{"https://api.example.com/v1", "http://api.example.com/v1"},
		},
		{
			name: "no schemes",
			spec: `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
host: api.example.com
basePath: /v1
`,
			want: []string{"//api.example.com/v1"},
		},
		{
			name: "host only",
			spec: `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
host: api.example.com
`,
			want: []string{"//api.example.com/"},
		},
		{
			name: "basePath only",
			spec: `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
basePath: /v1
`,
			want: []string{"/v1"},
		},
		{
			name: "schemes without host degrades to relative basePath",
			spec: `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
basePath: /v1
schemes:
  - https
`,
			want:             []string{"/v1"},
			wantDiag:         "host",
			wantDiagSeverity: SeverityWarning,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := LoadFile("test.yaml", []byte(tc.spec))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			spec, diags, err := ConvertV2(node)
			if err != nil {
				t.Fatalf("ConvertV2: %v", err)
			}
			got := make([]string, len(spec.Servers))
			for i, s := range spec.Servers {
				got[i] = s.URL
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("servers = %v, want %v", got, tc.want)
			}
			if tc.wantDiag != "" {
				sev := SeverityError
				if tc.wantDiagSeverity != 0 {
					sev = tc.wantDiagSeverity
				}
				var found bool
				for _, d := range diags {
					if d.Severity == sev && strings.Contains(d.Detail, tc.wantDiag) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected %v diagnostic containing %q, got %v", sev, tc.wantDiag, diags)
				}
			}
		})
	}
}

func TestConvertV2Definitions(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
definitions:
  Pet:
    type: object
    required:
      - id
    properties:
      id:
        type: integer
        format: int64
      category:
        $ref: '#/definitions/Category'
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if got.Components == nil || got.Components.Schemas == nil {
		t.Fatal("missing components/schemas")
	}
	pet := got.Components.Schemas["Pet"]
	if pet == nil {
		t.Fatal("missing Pet schema")
	}
	if pet.Properties["category"].Ref != "#/definitions/Category" {
		t.Errorf("category ref = %q, want %q", pet.Properties["category"].Ref, "#/definitions/Category")
	}
}

func TestConvertV2SecurityDefinitions(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
securityDefinitions:
  basicAuth:
    type: basic
  apiKey:
    type: apiKey
    name: X-API-Key
    in: header
  oauth2:
    type: oauth2
    flow: accessCode
    authorizationUrl: https://example.com/oauth/authorize
    tokenUrl: https://example.com/oauth/token
    scopes:
      read: read access
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if got.Components == nil || got.Components.SecuritySchemes == nil {
		t.Fatal("missing components/securitySchemes")
	}

	basic, ok := got.Components.SecuritySchemes["basicAuth"]
	if !ok || basic.Type != "http" || basic.Scheme != "basic" {
		t.Errorf("basicAuth = %+v, want http/basic", basic)
	}

	apiKey, ok := got.Components.SecuritySchemes["apiKey"]
	if !ok || apiKey.Type != "apiKey" || apiKey.Name != "X-API-Key" || apiKey.In != "header" {
		t.Errorf("apiKey = %+v", apiKey)
	}

	oauth, ok := got.Components.SecuritySchemes["oauth2"]
	if !ok || oauth.Type != "oauth2" || oauth.Flows == nil || oauth.Flows.AuthorizationCode == nil {
		t.Fatalf("oauth2 = %+v", oauth)
	}
	if oauth.Flows.AuthorizationCode.AuthorizationURL != "https://example.com/oauth/authorize" {
		t.Errorf("oauth2 authorizationUrl = %q", oauth.Flows.AuthorizationCode.AuthorizationURL)
	}
	if oauth.Flows.AuthorizationCode.TokenURL != "https://example.com/oauth/token" {
		t.Errorf("oauth2 tokenUrl = %q", oauth.Flows.AuthorizationCode.TokenURL)
	}
	if !reflect.DeepEqual(oauth.Flows.AuthorizationCode.Scopes, map[string]string{"read": "read access"}) {
		t.Errorf("oauth2 scopes = %v", oauth.Flows.AuthorizationCode.Scopes)
	}
}

func TestConvertV2BodyParameterToRequestBody(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    post:
      consumes:
        - application/json
      parameters:
        - name: pet
          in: body
          required: true
          schema:
            $ref: '#/definitions/Pet'
      responses:
        "200":
          description: OK
definitions:
  Pet:
    type: object
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	op := got.Paths["/pets"].Post
	if op == nil || op.RequestBody == nil {
		t.Fatal("missing requestBody")
	}
	if !op.RequestBody.Required {
		t.Error("requestBody should be required")
	}
	mt, ok := op.RequestBody.Content["application/json"]
	if !ok {
		t.Fatalf("missing application/json content: %v", op.RequestBody.Content)
	}
	if mt.Schema == nil || mt.Schema.Ref != "#/definitions/Pet" {
		t.Errorf("requestBody schema ref = %v", mt.Schema)
	}
}

func TestConvertV2ResponseSchema(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    get:
      produces:
        - application/json
      responses:
        "200":
          description: A list of pets
          schema:
            type: array
            items:
              $ref: '#/definitions/Pet'
definitions:
  Pet:
    type: object
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	resp := got.Paths["/pets"].Get.Responses["200"]
	if resp == nil {
		t.Fatal("missing 200 response")
	}
	mt, ok := resp.Content["application/json"]
	if !ok {
		t.Fatalf("missing application/json content")
	}
	if mt.Schema == nil || mt.Schema.Type != "array" || mt.Schema.Items == nil {
		t.Errorf("response schema = %+v", mt.Schema)
	}
	itemsSchema, ok := mt.Schema.Items.(*Schema)
	if !ok {
		t.Fatalf("expected Items to be *Schema, got %T", mt.Schema.Items)
	}
	if itemsSchema.Ref != "#/definitions/Pet" {
		t.Errorf("items ref = %q", itemsSchema.Ref)
	}
}

func TestConvertV2BodyParameterNoSchemaPreservesLocation(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    post:
      consumes:
        - application/json
      parameters:
        - name: pet
          in: body
          required: true
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	op := got.Paths["/pets"].Post
	if op == nil || op.RequestBody == nil {
		t.Fatal("missing requestBody")
	}
	if op.RequestBody.SourceLocation.IsEmpty() {
		t.Error("requestBody sourceLocation should be populated from the body parameter even when it has no schema")
	}
}

func TestConvertV2ResponseExamples(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    get:
      produces:
        - application/json
        - application/xml
      responses:
        "200":
          description: A list of pets
          schema:
            type: array
            items:
              type: string
          examples:
            application/json:
              - rex
              - fido
            application/octet-stream:
              raw: bytes
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	resp := got.Paths["/pets"].Get.Responses["200"]
	if resp == nil {
		t.Fatal("missing 200 response")
	}
	jsonMT, ok := resp.Content["application/json"]
	if !ok {
		t.Fatalf("missing application/json content: %v", resp.Content)
	}
	ex, ok := jsonMT.Example.([]any)
	if !ok || len(ex) != 2 {
		t.Fatalf("application/json example = %#v, want [rex, fido]", jsonMT.Example)
	}

	// application/octet-stream has no schema/produces entry, so the example
	// should create its own MediaType entry rather than be dropped.
	octetMT, ok := resp.Content["application/octet-stream"]
	if !ok {
		t.Fatalf("missing application/octet-stream content (example should create entry): %v", resp.Content)
	}
	want := map[string]any{"raw": "bytes"}
	if !reflect.DeepEqual(octetMT.Example, want) {
		t.Errorf("application/octet-stream example = %#v, want %#v", octetMT.Example, want)
	}
}

func TestConvertV2BodyParameterDescription(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    post:
      consumes:
        - application/json
      parameters:
        - name: pet
          in: body
          required: true
          description: The pet to create
          schema:
            $ref: '#/definitions/Pet'
      responses:
        "200":
          description: OK
definitions:
  Pet:
    type: object
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	rb := got.Paths["/pets"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	if rb.Description != "The pet to create" {
		t.Errorf("requestBody.description = %q, want %q", rb.Description, "The pet to create")
	}
}

func TestConvertV2FormDataParameterToRequestBody(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    post:
      parameters:
        - name: name
          in: formData
          required: true
          type: string
        - name: age
          in: formData
          type: integer
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	rb := got.Paths["/pets"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	mt, ok := rb.Content["application/x-www-form-urlencoded"]
	if !ok {
		t.Fatalf("missing application/x-www-form-urlencoded content: %v", rb.Content)
	}
	if mt.Schema == nil {
		t.Fatal("missing formData schema")
	}
	if !reflect.DeepEqual(mt.Schema.Required, []string{"name"}) {
		t.Errorf("schema.required = %v, want [name]", mt.Schema.Required)
	}
	if mt.Schema.Properties["name"] == nil || mt.Schema.Properties["name"].Type != "string" {
		t.Errorf("name property missing or wrong type")
	}
	if mt.Schema.Properties["age"] == nil || mt.Schema.Properties["age"].Type != "integer" {
		t.Errorf("age property missing or wrong type")
	}
}

func TestConvertV2FormDataMultipartParameterToRequestBody(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /upload:
    post:
      consumes:
        - multipart/form-data
      parameters:
        - name: file
          in: formData
          type: file
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	rb := got.Paths["/upload"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	mt, ok := rb.Content["multipart/form-data"]
	if !ok {
		t.Fatalf("missing multipart/form-data content: %v", rb.Content)
	}
	if mt.Schema == nil {
		t.Fatal("missing formData schema")
	}
	fileSchema := mt.Schema.Properties["file"]
	if fileSchema == nil || fileSchema.Type != "string" || fileSchema.Format != "binary" {
		t.Errorf("file schema = %+v, want string/binary", fileSchema)
	}
	enc, ok := mt.Encoding["file"]
	if !ok {
		t.Fatal("missing encoding for file property")
	}
	if enc.ContentType != "application/octet-stream" {
		t.Errorf("encoding contentType = %q, want application/octet-stream", enc.ContentType)
	}
}

func TestConvertV2FormDataFileParameterToRequestBody(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /upload:
    post:
      consumes:
        - application/x-www-form-urlencoded
      parameters:
        - name: file
          in: formData
          type: file
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	rb := got.Paths["/upload"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	// type:file forces multipart/form-data regardless of consumes.
	if _, ok := rb.Content["multipart/form-data"]; !ok {
		t.Fatalf("expected multipart/form-data content, got %v", rb.Content)
	}
}

func TestConvertV2MixedBodyAndFormDataParameters(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    post:
      consumes:
        - application/json
      parameters:
        - name: pet
          in: body
          schema:
            $ref: '#/definitions/Pet'
        - name: name
          in: formData
          type: string
      responses:
        "200":
          description: OK
definitions:
  Pet:
    type: object
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Summary, "Invalid parameter combination") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected diagnostic for mixed body+formData, got %v", diags)
	}
	rb := got.Paths["/pets"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	if _, ok := rb.Content["application/json"]; !ok {
		t.Errorf("missing application/json content: %v", rb.Content)
	}
}

func TestConvertV2PathItemParameters(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        type: string
    get:
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	item := got.Paths["/pets/{petId}"]
	if item == nil {
		t.Fatal("missing path item")
	}
	if len(item.Parameters) != 1 {
		t.Fatalf("path item parameters = %v", item.Parameters)
	}
	if item.Parameters[0].Name != "petId" || item.Parameters[0].In != "path" {
		t.Errorf("path item parameter = %+v", item.Parameters[0])
	}
}

func TestConvertV2OperationParameters(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets/{petId}:
    get:
      parameters:
        - name: petId
          in: path
          required: true
          type: string
        - name: limit
          in: query
          type: integer
        - name: X-Trace
          in: header
          type: string
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	params := got.Paths["/pets/{petId}"].Get.Parameters
	if len(params) != 3 {
		t.Fatalf("operation parameters = %v", params)
	}
	want := map[string]string{
		"petId":   "path",
		"limit":   "query",
		"X-Trace": "header",
	}
	for _, p := range params {
		if want[p.Name] != p.In {
			t.Errorf("parameter %s in = %q, want %q", p.Name, p.In, want[p.Name])
		}
	}
}

func TestConvertV2ProducesConsumesTypeErrorsEmitDiagnostics(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
produces: not-a-sequence
consumes:
  - application/json
  - 123
paths:
  /pets:
    get:
      produces:
        - 456
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	// Three type-error diagnostics are expected: global produces (not a
	// sequence), global consumes item (non-string), and operation produces
	// item (non-string). v2StringSlice emits warnings for these rather than
	// silently dropping the malformed values.
	var count int
	for _, d := range diags {
		if d.Severity != SeverityWarning {
			continue
		}
		if strings.Contains(d.Detail, "produces") || strings.Contains(d.Detail, "consumes") ||
			strings.Contains(d.Summary, "produces") || strings.Contains(d.Summary, "consumes") {
			count++
		}
	}
	if count < 3 {
		t.Errorf("expected at least 3 produces/consumes type-warning diagnostics, got %d: %v", count, diags)
	}
}

func TestConvertV2MalformedDiagnostics(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths: not-an-object
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Detail, "paths") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error diagnostic for malformed paths, got %v", diags)
	}
}

func TestConvertV2JSONInput(t *testing.T) {
	spec := `{
  "swagger": "2.0",
  "info": {
    "title": "Test",
    "version": "1.0.0"
  },
  "paths": {
    "/pets": {
      "get": {
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    }
  }
}`
	node, err := LoadFile("test.json", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if got.Info == nil || got.Info.Title != "Test" {
		t.Errorf("info = %+v", got.Info)
	}
	if got.Paths["/pets"].Get == nil {
		t.Error("missing GET /pets")
	}
}

func TestConvertV2TopLevelParametersResponses(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
produces:
  - application/json
parameters:
  petId:
    name: petId
    in: path
    required: true
    type: string
responses:
  NotFound:
    description: Not found
    schema:
      type: object
      properties:
        message:
          type: string
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if got.Components == nil {
		t.Fatal("missing components")
	}
	if got.Components.Parameters == nil || got.Components.Parameters["petId"] == nil {
		t.Errorf("missing components.parameters petId")
	}
	if got.Components.Responses == nil || got.Components.Responses["NotFound"] == nil {
		t.Errorf("missing components.responses NotFound")
	}
	notFound := got.Components.Responses["NotFound"]
	if notFound.Content == nil {
		t.Fatalf("NotFound response missing Content map")
	}
	mt := notFound.Content["application/json"]
	if mt == nil {
		t.Fatalf("NotFound response missing application/json media type")
	}
	if mt.Schema == nil {
		t.Fatalf("NotFound response application/json media type missing schema")
	}
	if mt.Schema.Type != "object" {
		t.Errorf("NotFound schema type = %q, want object", mt.Schema.Type)
	}
	if mt.Schema.Properties == nil || mt.Schema.Properties["message"] == nil {
		t.Errorf("NotFound schema missing message property")
	}
}

func TestConvertV2Extensions(t *testing.T) {
	spec := `swagger: "2.0"
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
        "200":
          description: ok
          x-response-ext: response-value
definitions:
  Item:
    type: object
    x-schema-ext: schema-value
`
	node, err := LoadFile("extensions.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	if got.Info == nil || got.Info.Extensions["x-info-ext"] != "info-value" {
		t.Fatalf("info extensions missing: %+v", got.Info)
	}
	pi := got.Paths["/items"]
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
	if got.Components == nil || got.Components.Schemas["Item"] == nil || got.Components.Schemas["Item"].Extensions["x-schema-ext"] != "schema-value" {
		t.Fatalf("schema extensions missing: %+v", got.Components)
	}
}

func TestConvertV2GlobalProducesConsumesFallback(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
produces:
  - application/xml
consumes:
  - application/xml
paths:
  /pets:
    get:
      responses:
        "200":
          description: OK
          schema:
            type: string
    post:
      parameters:
        - name: pet
          in: body
          schema:
            type: string
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	get := got.Paths["/pets"].Get
	if get == nil || get.Responses["200"] == nil || get.Responses["200"].Content["application/xml"] == nil {
		t.Errorf("GET response content should fall back to global produces application/xml: %+v", get.Responses["200"].Content)
	}

	post := got.Paths["/pets"].Post
	if post == nil || post.RequestBody == nil || post.RequestBody.Content["application/xml"] == nil {
		t.Errorf("POST requestBody content should fall back to global consumes application/xml: %+v", post.RequestBody)
	}
}

func TestConvertV2UnrecognizedOAuth2Flow(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
securityDefinitions:
  oauth2:
    type: oauth2
    flow: unknownFlow
    authorizationUrl: https://example.com/oauth/authorize
    scopes:
      read: read access
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	oauth, ok := got.Components.SecuritySchemes["oauth2"]
	if !ok || oauth.Type != "oauth2" || oauth.Flows != nil {
		t.Fatalf("oauth2 = %+v, want type=oauth2 with nil Flows", oauth)
	}

	var found bool
	for _, d := range diags {
		if d.Severity == SeverityWarning && strings.Contains(d.Summary, "Unrecognized OAuth2 flow") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning diagnostic for unrecognized OAuth2 flow, got %v", diags)
	}
}

func TestConvertV2DiscriminatorEmptyPropertyNameObject(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
definitions:
  Pet:
    type: object
    discriminator:
      propertyName: ""
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Detail, "discriminator propertyName must not be empty") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error diagnostic for empty object-form discriminator, got %v", diags)
	}
}

func TestConvertV2DiscriminatorMissingPropertyName(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
definitions:
  Pet:
    type: object
    discriminator:
      mapping:
        Dog: '#/definitions/Dog'
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Detail, "discriminator propertyName must not be empty") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error diagnostic for missing discriminator propertyName, got %v", diags)
	}
}

func TestConvertV2DiscriminatorInvalidNodeType(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
definitions:
  Pet:
    type: object
    discriminator:
      - not-a-string
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == SeverityWarning && strings.Contains(d.Detail, "discriminator must be a string or object") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning diagnostic for invalid discriminator type, got %v", diags)
	}
}

func TestConvertV2CollectionFormat(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets/{petId}:
    get:
      parameters:
        - name: petId
          in: path
          required: true
          type: array
          items:
            type: string
          collectionFormat: csv
        - name: tags
          in: query
          type: array
          items:
            type: string
          collectionFormat: multi
        - name: values
          in: query
          type: array
          items:
            type: string
          collectionFormat: tsv
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	params := got.Paths["/pets/{petId}"].Get.Parameters
	want := map[string]struct {
		style   string
		explode bool
	}{
		"petId":  {style: "simple", explode: false},
		"tags":   {style: "form", explode: true},
		"values": {style: "", explode: false},
	}
	for _, p := range params {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected parameter %q", p.Name)
			continue
		}
		if p.Style != w.style {
			t.Errorf("%s style = %q, want %q", p.Name, p.Style, w.style)
		}
		if p.Explode != w.explode {
			t.Errorf("%s explode = %v, want %v", p.Name, p.Explode, w.explode)
		}
		if p.Name == "values" {
			if p.Extensions == nil || p.Extensions["x-collectionFormat"] != "tsv" {
				t.Errorf("values x-collectionFormat extension missing: %+v", p.Extensions)
			}
		}
	}
}

func TestConvertV2FormDataCollectionFormatEncoding(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /upload:
    post:
      consumes:
        - multipart/form-data
      parameters:
        - name: files
          in: formData
          type: array
          items:
            type: file
          collectionFormat: multi
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	rb := got.Paths["/upload"].Post.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	mt, ok := rb.Content["multipart/form-data"]
	if !ok {
		t.Fatalf("missing multipart/form-data content: %v", rb.Content)
	}
	enc, ok := mt.Encoding["files"]
	if !ok {
		t.Fatal("missing encoding for files property")
	}
	if enc.Style != "form" || enc.Explode != true {
		t.Errorf("files encoding = style:%q explode:%v, want form/true", enc.Style, enc.Explode)
	}
}

func TestConvertV2ExplicitEmptyProducesConsumes(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
produces:
  - application/xml
consumes:
  - application/xml
paths:
  /pets:
    get:
      produces: []
      responses:
        "200":
          description: OK
          schema:
            type: string
    post:
      consumes: []
      parameters:
        - name: pet
          in: body
          schema:
            type: string
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	get := got.Paths["/pets"].Get
	if get == nil || get.Responses["200"] == nil {
		t.Fatal("missing GET /pets response")
	}
	// The key assertion is the absence of application/xml, which proves the
	// explicit `produces: []` did not inherit the global produces. The presence
	// of application/json is the response builder's default when no content
	// types are available, not an inherited value.
	if _, ok := get.Responses["200"].Content["application/xml"]; ok {
		t.Errorf("GET response should not inherit global produces application/xml")
	}
	if _, ok := get.Responses["200"].Content["application/json"]; !ok {
		t.Errorf("GET response should default to application/json, got %v", get.Responses["200"].Content)
	}

	post := got.Paths["/pets"].Post
	if post == nil || post.RequestBody == nil {
		t.Fatal("missing POST /pets requestBody")
	}
	// Same for the request body: the absence of application/xml is what
	// matters; application/json is the fallback default from the request body
	// builder when the explicit `consumes: []` leaves no content types.
	if _, ok := post.RequestBody.Content["application/xml"]; ok {
		t.Errorf("POST requestBody should not inherit global consumes application/xml")
	}
	if _, ok := post.RequestBody.Content["application/json"]; !ok {
		t.Errorf("POST requestBody should default to application/json, got %v", post.RequestBody.Content)
	}
}

func TestConvertV2MissingOAuth2Flow(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
securityDefinitions:
  oauth2:
    type: oauth2
    authorizationUrl: https://example.com/oauth/authorize
    scopes:
      read: read access
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	oauth, ok := got.Components.SecuritySchemes["oauth2"]
	if !ok || oauth.Type != "oauth2" || oauth.Flows != nil {
		t.Fatalf("oauth2 = %+v, want type=oauth2 with nil Flows", oauth)
	}

	var found bool
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Detail, "missing required flow field") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error diagnostic for missing oauth2 flow field, got %v", diags)
	}
}

func TestConvertV2CollectionFormatPreservesUserExtension(t *testing.T) {
	spec := `swagger: "2.0"
info:
  title: Test
  version: "1.0.0"
paths:
  /pets:
    get:
      parameters:
        - name: tags
          in: query
          type: array
          items:
            type: string
          collectionFormat: tsv
          x-collectionFormat: user-tsv
        - name: values
          in: query
          type: array
          items:
            type: string
          collectionFormat: custom
          x-collectionFormat: user-custom
      responses:
        "200":
          description: OK
`
	node, err := LoadFile("test.yaml", []byte(spec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	got, _, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	params := got.Paths["/pets"].Get.Parameters
	for _, p := range params {
		want := ""
		switch p.Name {
		case "tags":
			want = "user-tsv"
		case "values":
			want = "user-custom"
		default:
			t.Errorf("unexpected parameter %q", p.Name)
			continue
		}
		if p.Extensions == nil || p.Extensions["x-collectionFormat"] != want {
			t.Errorf("%s x-collectionFormat = %v, want %q", p.Name, p.Extensions["x-collectionFormat"], want)
		}
	}
}

func TestConvertV2ScalarTypeMismatches(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Mismatch API
  version: "1.0.0"
  description: 42
host: 123
basePath: true
schemes:
  - http
  - 456
paths:
  /pets:
    get:
      summary:
        - not a string
      parameters:
        - name: limit
          in: query
          required: "yes"
          type: integer
      responses:
        200:
          description: OK
definitions:
  Pet:
    type: object
    maxLength: "not a number"
    uniqueItems: 1
    required:
      - name
      - 123
securityDefinitions:
  apiKey:
    type: 789
    in: header
    name: X-API-Key
`)
	node, err := LoadFile("mismatches.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if spec.Info.Description != "" {
		t.Errorf("expected empty description for type mismatch, got %q", spec.Info.Description)
	}

	wantSummaries := map[string]bool{
		"Invalid info.description value":     false,
		"Invalid host value":                 false,
		"Invalid basePath value":             false,
		"Invalid schemes item value":         false,
		"Invalid operation.summary value":    false,
		"Invalid parameter.required value":   false,
		"Invalid schema.maxLength value":     false,
		"Invalid schema.uniqueItems value":   false,
		"Invalid schema.required item value": false,
		"Invalid security.type value":        false,
	}
	for _, d := range diags {
		if _, ok := wantSummaries[d.Summary]; ok {
			wantSummaries[d.Summary] = true
		}
	}
	for summary, found := range wantSummaries {
		if !found {
			t.Errorf("expected diagnostic %q", summary)
		}
	}
}

// TestConvertV2DeepScalarTypeMismatches covers the Swagger 2.0 scalar fields that
// were previously silently coerced via the un-validated scalarValue/stringValue
// helpers. Genuinely scalar-typed fields (strings, the additionalProperties
// boolean) now emit a warning diagnostic; any-value fields (default/example/const
// whose value follows the schema type) are preserved via nodeToNative without
// warning, matching the OpenAPI 3.x converter depth and avoiding false positives
// on legitimate array/object values.
func TestConvertV2DeepScalarTypeMismatches(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Deep Mismatch API
  version: "1.0.0"
externalDocs:
  description: 5
  url: 7
paths:
  /pets:
    get:
      summary: list
      externalDocs:
        description: 11
        url: 13
      parameters:
        - name: tags
          in: query
          type: array
          items:
            type: string
          collectionFormat: [1]
      responses:
        200:
          description: OK
        201:
          $ref: 123
          description: 9
        202:
          description: also ok
          schema:
            $ref: "#/definitions/Pet"
definitions:
  Pet:
    type: object
    default: [1, 2]
    example:
      a: b
    const: [3, 4]
    examples:
      foo:
        value: [1, 2]
    exclusiveMaximum: true
    exclusiveMinimum: false
    additionalProperties: "notabool"
    properties:
      name:
        type: string
`)
	node, err := LoadFile("deep-mismatches.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}

	wantSummaries := map[string]bool{
		"Invalid response.$ref value":               false,
		"Invalid response.description value":        false,
		"Invalid parameter.collectionFormat value":  false,
		"Invalid externalDocs.description value":    false,
		"Invalid externalDocs.url value":            false,
		"Invalid schema.additionalProperties value": false,
	}
	// any-value fields that must NOT warn (over-warning regression guard).
	forbiddenSummaries := map[string]bool{
		"Invalid schema.default value":          false,
		"Invalid schema.example value":          false,
		"Invalid schema.const value":            false,
		"Invalid schema.exclusiveMaximum value": false,
		"Invalid schema.exclusiveMinimum value": false,
		"Invalid example.value value":           false,
	}
	for _, d := range diags {
		if _, ok := wantSummaries[d.Summary]; ok {
			wantSummaries[d.Summary] = true
		}
		if _, ok := forbiddenSummaries[d.Summary]; ok {
			forbiddenSummaries[d.Summary] = true
		}
	}
	for summary, found := range wantSummaries {
		if !found {
			t.Errorf("expected diagnostic %q", summary)
		}
	}
	for summary, found := range forbiddenSummaries {
		if found {
			t.Errorf("any-value field should not warn, got diagnostic %q", summary)
		}
	}

	// Array/object any-value fields must be preserved (not silently dropped).
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatalf("missing components.schemas")
	}
	pet := spec.Components.Schemas["Pet"]
	if pet == nil {
		t.Fatalf("missing Pet definition")
	}
	if def, ok := pet.Default.([]any); !ok || len(def) != 2 {
		t.Errorf("Pet.default not preserved as 2-element slice, got %#v", pet.Default)
	}
	if ex, ok := pet.Example.(map[string]any); !ok || ex["a"] != "b" {
		t.Errorf("Pet.example not preserved as map, got %#v", pet.Example)
	}
	if c, ok := pet.Const.([]any); !ok || len(c) != 2 {
		t.Errorf("Pet.const not preserved as 2-element slice, got %#v", pet.Const)
	}
	if pet.ExclusiveMaximum != true {
		t.Errorf("Pet.exclusiveMaximum not preserved as bool true, got %#v", pet.ExclusiveMaximum)
	}
	if pet.ExclusiveMinimum != false {
		t.Errorf("Pet.exclusiveMinimum not preserved as bool false, got %#v", pet.ExclusiveMinimum)
	}

	// The named example's value (any-value, via parseExample) must be preserved.
	exFoo := pet.Examples["foo"]
	if exFoo == nil {
		t.Fatalf("missing foo example on Pet schema")
	}
	if v, ok := exFoo.Value.([]any); !ok || len(v) != 2 {
		t.Errorf("example.value not preserved as 2-element slice, got %#v", exFoo.Value)
	}

	// Non-string collectionFormat must coerce to empty (no panic) and warn; it maps
	// to Parameter.Style, which must remain unset for the bad value.
	if len(spec.Paths["/pets"].Get.Parameters) == 0 {
		t.Fatalf("missing operation parameters")
	}
	if style := spec.Paths["/pets"].Get.Parameters[0].Style; style != "" {
		t.Errorf("non-string collectionFormat should leave Style empty, got %q", style)
	}
}

// TestConvertV2SwaggerVersionPreservation asserts the top-level swagger version
// field preserves unquoted scalar values (e.g. YAML `swagger: 2.0`, which the
// lexer represents as a numeric scalar) without emitting a spurious diagnostic,
// while a genuinely non-scalar value warns.
func TestConvertV2SwaggerVersionPreservation(t *testing.T) {
	t.Run("quoted string", func(t *testing.T) {
		node, err := LoadFile("s.yaml", []byte(`swagger: "2.0"
info:
  title: T
  version: "1"
`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV2(node)
		if err != nil {
			t.Fatalf("ConvertV2: %v", err)
		}
		if spec.Swagger != "2.0" {
			t.Errorf("swagger = %q, want %q", spec.Swagger, "2.0")
		}
		for _, d := range diags {
			if strings.Contains(d.Summary, "swagger") {
				t.Errorf("quoted swagger should not warn, got %q: %s", d.Summary, d.Detail)
			}
		}
	})

	t.Run("unquoted numeric", func(t *testing.T) {
		node, err := LoadFile("s.yaml", []byte(`swagger: 2.0
info:
  title: T
  version: "1"
`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV2(node)
		if err != nil {
			t.Fatalf("ConvertV2: %v", err)
		}
		if spec.Swagger != "2.0" {
			t.Errorf("unquoted swagger = %q, want %q", spec.Swagger, "2.0")
		}
		for _, d := range diags {
			if strings.Contains(d.Summary, "swagger") {
				t.Errorf("unquoted numeric swagger should not warn, got %q: %s", d.Summary, d.Detail)
			}
		}
	})

	t.Run("non-scalar warns", func(t *testing.T) {
		// A block sequence (not a flow sequence, which the lexer reads as a scalar
		// string at the top level) exercises the genuinely-non-scalar branch.
		node, err := LoadFile("s.yaml", []byte(`swagger:
  - 1
  - 2
info:
  title: T
  version: "1"
`))
		if err != nil {
			t.Fatalf("LoadFile: %v", err)
		}
		spec, diags, err := ConvertV2(node)
		if err != nil {
			t.Fatalf("ConvertV2: %v", err)
		}
		if spec.Swagger != "" {
			t.Errorf("non-scalar swagger = %q, want empty", spec.Swagger)
		}
		found := false
		for _, d := range diags {
			if d.Summary == "Invalid swagger value" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q diagnostic for non-scalar swagger", "Invalid swagger value")
		}
	})
}

// TestConvertV2AdditionalPropertiesBoolPreserved asserts a boolean
// additionalProperties value is stored directly and does not warn, complementing
// the non-bool-scalar warning exercised in TestConvertV2DeepScalarTypeMismatches.
func TestConvertV2AdditionalPropertiesBoolPreserved(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: T
  version: "1"
definitions:
  Closed:
    type: object
    additionalProperties: false
  Open:
    type: object
    additionalProperties: true
`)
	node, err := LoadFile("addp.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	for _, d := range diags {
		if strings.Contains(d.Summary, "additionalProperties") {
			t.Errorf("boolean additionalProperties should not warn, got %q", d.Summary)
		}
	}
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatalf("missing components.schemas")
	}
	if spec.Components.Schemas["Closed"].AdditionalProperties != false {
		t.Errorf("Closed.additionalProperties = %#v, want false", spec.Components.Schemas["Closed"].AdditionalProperties)
	}
	if spec.Components.Schemas["Open"].AdditionalProperties != true {
		t.Errorf("Open.additionalProperties = %#v, want true", spec.Components.Schemas["Open"].AdditionalProperties)
	}
}
