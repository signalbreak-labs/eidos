package parser

import (
	"strings"
	"testing"
)

// v2EdgeSpec exercises the Swagger 2.0 helper paths that the mycloud fixture
// does not reach: info contact/license, per-parameter collectionFormat styles,
// response headers (object and $ref forms), oauth2 security definitions with
// every flow type (plus the missing-flow and unrecognized-flow error paths),
// top-level and operation-level security requirements, and the schema metadata
// edges (xml, object/string/boolean additionalProperties, discriminator
// variants, allOf composites, examples, numeric constraints, enum).
//
// The fixture is written entirely in block YAML: the in-house lexer does not
// support flow-style `[...]`/`{...}` collections.
const v2EdgeSpec = `
swagger: "2.0"
info:
  title: Edge
  version: "1.0.0"
  contact:
    name: API Team
    url: https://example.com/contact
    email: api@example.com
  license:
    name: MIT
    url: https://example.com/license
security:
  - keyAuth:
      - read
securityDefinitions:
  basicAuth:
    type: basic
    description: basic auth
  keyAuth:
    type: apiKey
    name: X-Key
    in: header
  oauthImplicit:
    type: oauth2
    flow: implicit
    authorizationUrl: https://example.com/authorize
    scopes:
      read: Read access
      write: Write access
  oauthCode:
    type: oauth2
    flow: accessCode
    authorizationUrl: https://example.com/authorize
    tokenUrl: https://example.com/token
    refreshUrl: https://example.com/refresh
    scopes:
      read: Read access
  oauthPassword:
    type: oauth2
    flow: password
    tokenUrl: https://example.com/token
  oauthApp:
    type: oauth2
    flow: application
    tokenUrl: https://example.com/token
    scopes:
      read: Read
  brokenOAuth:
    type: oauth2
    description: missing flow
  weirdOAuth:
    type: oauth2
    flow: exotic
paths:
  /pets:
    get:
      tags:
        - pets
      operationId: listPets
      security:
        - keyAuth:
            - read
        - oauthImplicit:
            - read
            - write
      parameters:
        - name: limit
          in: query
          type: integer
          collectionFormat: csv
        - name: tags
          in: query
          type: array
          items:
            type: string
          collectionFormat: ssv
        - name: ids
          in: query
          type: array
          items:
            type: string
          collectionFormat: tsv
        - name: multi
          in: query
          type: array
          items:
            type: string
          collectionFormat: multi
        - name: pipes
          in: query
          type: array
          items:
            type: string
          collectionFormat: pipes
        - name: unknown
          in: query
          type: array
          items:
            type: string
          collectionFormat: exotic
      produces:
        - application/json
      responses:
        "200":
          description: ok
          headers:
            X-Rate-Limit:
              type: integer
              description: rate limit
            X-Rate-Ref:
              $ref: "#/headers/RateHeader"
          schema:
            type: array
            items:
              $ref: "#/definitions/Pet"
          examples:
            application/json:
              - id: 1
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - name: id
          in: path
          required: true
          type: string
          collectionFormat: csv
      responses:
        "200":
          description: ok
          schema:
            $ref: "#/definitions/Pet"
        "404":
          description: not found
definitions:
  Pet:
    type: object
    required:
      - id
      - name
    properties:
      id:
        type: integer
        format: int64
      name:
        type: string
      owner:
        $ref: "#/definitions/Owner"
    patternProperties:
      "^x-":
        type: string
    additionalProperties:
      type: string
    xml:
      name: pet
      namespace: https://example.com/xml
      prefix: p
      attribute: true
      wrapped: false
    discriminator:
      propertyName: petType
      mapping:
        cat: "#/definitions/Cat"
        dog: "#/definitions/Dog"
    externalDocs:
      description: more
      url: https://example.com/docs
    example:
      id: 1
      name: Rex
    examples:
      simple:
        summary: simple
        value:
          id: 1
    enum:
      - a
      - b
      - c
    multipleOf: 0.5
    maximum: 10
    minimum: 0
    exclusiveMaximum: true
    exclusiveMinimum: false
  Owner:
    type: object
    properties:
      name:
        type: string
  Cat:
    type: object
    allOf:
      - $ref: "#/definitions/Base"
      - type: object
        properties:
          meowVolume:
            type: integer
  Base:
    type: object
    properties:
      legs:
        type: integer
  Thing:
    type: object
    oneOf:
      - type: string
      - type: integer
    anyOf:
      - $ref: "#/definitions/Base"
    not:
      type: string
    contains:
      type: string
    propertyNames:
      maxLength: 10
    prefixItems:
      - type: string
    unevaluatedProperties:
      type: string
  ThingRef:
    type: object
    unevaluatedProperties: "#/definitions/Owner"
  Extra:
    type: object
    additionalProperties: "#/definitions/Owner"
  BoolProps:
    type: object
    additionalProperties: true
  BadProps:
    type: object
    additionalProperties: 5
  StrDisc:
    type: object
    discriminator: petType
  EmptyDisc:
    type: object
    discriminator: ""
  EmptyPropDisc:
    type: object
    discriminator:
      propertyName: ""
  SeqDisc:
    type: object
    discriminator:
      - a
      - b
  BadXML:
    type: object
    xml: not-a-map
`

func loadV2EdgeSpec(t *testing.T) *Spec {
	t.Helper()
	node, err := LoadFile("v2-edge.yaml", []byte(v2EdgeSpec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	t.Logf("ConvertV2 diags: %v", diags)
	return spec
}

// TestConvertV2Edge_InfoContactLicense asserts info.contact and info.license
// parse (parseContact/parseLicense).
func TestConvertV2Edge_InfoContactLicense(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	if spec.Info == nil || spec.Info.Contact == nil {
		t.Fatal("expected contact, got nil")
	}
	if spec.Info.Contact.Name != "API Team" || spec.Info.Contact.Email != "api@example.com" {
		t.Errorf("contact = %+v", spec.Info.Contact)
	}
	if spec.Info.License == nil || spec.Info.License.Name != "MIT" {
		t.Fatalf("license = %+v", spec.Info.License)
	}
}

// TestConvertV2Edge_SecuritySchemes asserts every oauth2 flow type and the
// basic/apiKey schemes parse into the expected SecurityScheme shapes.
func TestConvertV2Edge_SecuritySchemes(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	schemes := spec.Components.SecuritySchemes

	basic := schemes["basicAuth"]
	if basic.Type != "http" || basic.Scheme != "basic" {
		t.Errorf("basicAuth = %+v, want http/basic", basic)
	}
	if apiKey := schemes["keyAuth"]; apiKey.Type != "apiKey" || apiKey.In != "header" || apiKey.Name != "X-Key" {
		t.Errorf("keyAuth = %+v", apiKey)
	}
	implicit := schemes["oauthImplicit"].Flows
	if implicit == nil || implicit.Implicit == nil {
		t.Fatal("expected implicit flow")
	}
	if implicit.Implicit.AuthorizationURL != "https://example.com/authorize" {
		t.Errorf("implicit authz url = %q", implicit.Implicit.AuthorizationURL)
	}
	if implicit.Implicit.Scopes["read"] != "Read access" || implicit.Implicit.Scopes["write"] != "Write access" {
		t.Errorf("implicit scopes = %v", implicit.Implicit.Scopes)
	}
	code := schemes["oauthCode"].Flows
	if code == nil || code.AuthorizationCode == nil {
		t.Fatal("expected authorizationCode flow")
	}
	if code.AuthorizationCode.TokenURL != "https://example.com/token" || code.AuthorizationCode.RefreshURL != "https://example.com/refresh" {
		t.Errorf("authCode flow = %+v", code.AuthorizationCode)
	}
	if pw := schemes["oauthPassword"].Flows; pw == nil || pw.Password == nil || pw.Password.TokenURL == "" {
		t.Errorf("password flow = %+v", pw)
	}
	if app := schemes["oauthApp"].Flows; app == nil || app.ClientCredentials == nil {
		t.Errorf("application flow = %+v", app)
	}
	// Broken schemes parse but yield nil flows (error path returns before flows).
	if schemes["brokenOAuth"].Flows != nil {
		t.Errorf("brokenOAuth should have nil flows, got %+v", schemes["brokenOAuth"].Flows)
	}
	if schemes["weirdOAuth"].Flows != nil {
		t.Errorf("weirdOAuth should have nil flows, got %+v", schemes["weirdOAuth"].Flows)
	}
}

// TestConvertV2Edge_OAuthDiags asserts the missing-flow error and
// unrecognized-flow warning diagnostics are emitted.
func TestConvertV2Edge_OAuthDiags(t *testing.T) {
	node, err := LoadFile("v2-edge.yaml", []byte(v2EdgeSpec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	hasError := false
	hasWarning := false
	for _, d := range diags {
		switch {
		case d.Severity == SeverityError && strings.Contains(d.Detail, "missing required flow field"):
			hasError = true
		case d.Severity == SeverityWarning && strings.Contains(d.Detail, "is not recognized"):
			hasWarning = true
		}
	}
	if !hasError {
		t.Error("expected missing-flow error diagnostic")
	}
	if !hasWarning {
		t.Error("expected unrecognized-flow warning diagnostic")
	}
}

// TestConvertV2Edge_CollectionFormat asserts each collectionFormat maps to the
// correct OpenAPI 3.0 style/explode (or an x-collectionFormat extension).
func TestConvertV2Edge_CollectionFormat(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	op := spec.Paths["/pets"].Get
	if op == nil {
		t.Fatal("missing /pets get operation")
	}
	styleFor := map[string]struct {
		style   string
		explode bool
		ext     string
	}{
		"limit":   {style: "form", explode: false}, // csv in query → form
		"tags":    {style: "space", explode: false},
		"ids":     {ext: "tsv"}, // tsv → x-collectionFormat
		"multi":   {style: "form", explode: true},
		"pipes":   {style: "pipe", explode: false},
		"unknown": {ext: "exotic"},
	}
	for _, p := range op.Parameters {
		want, ok := styleFor[p.Name]
		if !ok {
			continue
		}
		if want.style != "" && p.Style != want.style {
			t.Errorf("%s style = %q, want %q", p.Name, p.Style, want.style)
		}
		if want.style != "" && p.Explode != want.explode {
			t.Errorf("%s explode = %v, want %v", p.Name, p.Explode, want.explode)
		}
		if want.ext != "" {
			if p.Extensions == nil || p.Extensions["x-collectionFormat"] != want.ext {
				t.Errorf("%s x-collectionFormat = %v, want %q", p.Name, p.Extensions, want.ext)
			}
		}
	}
	// csv in path → simple.
	pathOp := spec.Paths["/pets/{id}"].Get
	if len(pathOp.Parameters) != 1 || pathOp.Parameters[0].Style != "simple" || pathOp.Parameters[0].Explode {
		t.Errorf("path csv param = %+v, want simple non-explode", pathOp.Parameters)
	}
}

// TestConvertV2Edge_ResponseHeaders asserts response header maps parse both the
// inline object form and the $ref string form.
func TestConvertV2Edge_ResponseHeaders(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	resp := spec.Paths["/pets"].Get.Responses["200"]
	if resp == nil {
		t.Fatal("missing 200 response")
	}
	inline, ok := resp.Headers["X-Rate-Limit"]
	if !ok || inline == nil {
		t.Fatal("missing inline header X-Rate-Limit")
	}
	if inline.Schema == nil || inline.Schema.Type != "integer" {
		t.Errorf("inline header schema = %+v", inline.Schema)
	}
	refHdr, ok := resp.Headers["X-Rate-Ref"]
	if !ok || refHdr == nil || refHdr.Ref != "#/headers/RateHeader" {
		t.Errorf("ref header = %+v", refHdr)
	}
}

// TestConvertV2Edge_ResponseExamples asserts response-level per-mimetype
// examples attach to the matching MediaType.
func TestConvertV2Edge_ResponseExamples(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	mt, ok := spec.Paths["/pets"].Get.Responses["200"].Content["application/json"]
	if !ok {
		t.Fatal("missing application/json media type")
	}
	if mt.Example == nil {
		t.Error("expected response example to attach to media type")
	}
}

// TestConvertV2Edge_SecurityRequirements asserts operation-level security with
// scopes and top-level security parse.
func TestConvertV2Edge_SecurityRequirements(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	if len(spec.Security) != 1 || len(spec.Security[0].Requirements["keyAuth"]) != 1 {
		t.Errorf("top-level security = %+v", spec.Security)
	}
	op := spec.Paths["/pets"].Get
	if len(op.Security) != 2 {
		t.Fatalf("operation security = %+v", op.Security)
	}
	if scopes := op.Security[1].Requirements["oauthImplicit"]; len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("operation oauthImplicit scopes = %v", scopes)
	}
}

// TestConvertV2Edge_SchemaMetadata asserts the Pet schema's xml, discriminator,
// externalDocs, examples, enum, and numeric constraints all parse.
func TestConvertV2Edge_SchemaMetadata(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	pet := spec.Components.Schemas["Pet"]
	if pet == nil {
		t.Fatal("missing Pet definition")
	}
	if pet.XML == nil || pet.XML.Name != "pet" || pet.XML.Namespace != "https://example.com/xml" || pet.XML.Prefix != "p" || !pet.XML.Attribute || pet.XML.Wrapped {
		t.Errorf("pet xml = %+v", pet.XML)
	}
	if pet.Discriminator == nil || pet.Discriminator.PropertyName != "petType" || pet.Discriminator.Mapping["cat"] != "#/definitions/Cat" {
		t.Errorf("pet discriminator = %+v", pet.Discriminator)
	}
	if pet.ExternalDocs == nil || pet.ExternalDocs.URL != "https://example.com/docs" {
		t.Errorf("pet externalDocs = %+v", pet.ExternalDocs)
	}
	if len(pet.Examples) != 1 || pet.Examples["simple"].Summary != "simple" {
		t.Errorf("pet examples = %+v", pet.Examples)
	}
	if len(pet.Enum) != 3 {
		t.Errorf("pet enum = %v", pet.Enum)
	}
	if pet.MultipleOf != 0.5 || pet.Maximum != 10 || pet.Minimum != 0 {
		t.Errorf("pet numeric bounds = %v/%v/%v", pet.MultipleOf, pet.Maximum, pet.Minimum)
	}
	if pet.Properties["owner"] == nil || pet.Properties["owner"].Ref != "#/definitions/Owner" {
		t.Errorf("pet owner property = %+v", pet.Properties["owner"])
	}
	if pet.PatternProperties["^x-"] == nil {
		t.Error("expected patternProperty ^x-")
	}
	if ap, ok := pet.AdditionalProperties.(*Schema); !ok || ap.Type != "string" {
		t.Errorf("pet additionalProperties = %#v", pet.AdditionalProperties)
	}
}

// TestConvertV2Edge_AdditionalPropertiesShapes asserts the string-ref, boolean,
// and wrong-scalar additionalProperties forms.
func TestConvertV2Edge_AdditionalPropertiesShapes(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	if ap, ok := spec.Components.Schemas["Extra"].AdditionalProperties.(string); !ok || ap != "#/definitions/Owner" {
		t.Errorf("Extra additionalProperties = %#v, want string ref", spec.Components.Schemas["Extra"].AdditionalProperties)
	}
	if b, ok := spec.Components.Schemas["BoolProps"].AdditionalProperties.(bool); !ok || !b {
		t.Errorf("BoolProps additionalProperties = %#v, want true", spec.Components.Schemas["BoolProps"].AdditionalProperties)
	}
	if v := spec.Components.Schemas["BadProps"].AdditionalProperties; v != float64(5) {
		t.Errorf("BadProps should warn and store raw scalar, got %T(%v)", v, v)
	}
}

// TestConvertV2Edge_CompositeEdges asserts the oneOf/anyOf/not/contains/
// propertyNames/prefixItems/unevaluatedProperties branches of
// parseSchemaComposite.
func TestConvertV2Edge_CompositeEdges(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	thing := spec.Components.Schemas["Thing"]
	if thing == nil {
		t.Fatal("missing Thing definition")
	}
	if len(thing.OneOf) != 2 || thing.OneOf[0].Type != "string" || thing.OneOf[1].Type != "integer" {
		t.Errorf("thing oneOf = %+v", thing.OneOf)
	}
	if len(thing.AnyOf) != 1 || thing.AnyOf[0].Ref != "#/definitions/Base" {
		t.Errorf("thing anyOf = %+v", thing.AnyOf)
	}
	if thing.Not == nil || thing.Not.Type != "string" {
		t.Errorf("thing not = %+v", thing.Not)
	}
	if thing.Contains == nil || thing.Contains.Type != "string" {
		t.Errorf("thing contains = %+v", thing.Contains)
	}
	if thing.PropertyNames == nil || thing.PropertyNames.MaxLength != 10 {
		t.Errorf("thing propertyNames = %+v", thing.PropertyNames)
	}
	if len(thing.PrefixItems) != 1 || thing.PrefixItems[0].Type != "string" {
		t.Errorf("thing prefixItems = %+v", thing.PrefixItems)
	}
	if up := thing.UnevaluatedProperties; up == nil || up.Type != "string" {
		t.Errorf("thing unevaluatedProperties = %#v", thing.UnevaluatedProperties)
	}
	// String-ref form of unevaluatedProperties.
	if ref := spec.Components.Schemas["ThingRef"].UnevaluatedProperties; ref == nil || ref.Ref != "#/definitions/Owner" {
		t.Errorf("ThingRef unevaluatedProperties = %#v", spec.Components.Schemas["ThingRef"].UnevaluatedProperties)
	}
}

// TestConvertV2Edge_AllOfComposite asserts allOf arrays parse into the AllOf
// slice via parseSchemaSubList.
func TestConvertV2Edge_AllOfComposite(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	cat := spec.Components.Schemas["Cat"]
	if cat == nil || len(cat.AllOf) != 2 {
		t.Fatalf("cat allOf = %+v", cat)
	}
	if cat.AllOf[0].Ref != "#/definitions/Base" {
		t.Errorf("allOf[0] = %+v, want Base ref", cat.AllOf[0])
	}
	if cat.AllOf[1].Properties["meowVolume"] == nil {
		t.Errorf("allOf[1] = %+v", cat.AllOf[1])
	}
}

// TestConvertV2Edge_DiscriminatorVariants asserts the string, empty, and
// wrong-type discriminator branches of parseDiscriminator.
func TestConvertV2Edge_DiscriminatorVariants(t *testing.T) {
	node, err := LoadFile("v2-edge.yaml", []byte(v2EdgeSpec))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	spec, diags, err := ConvertV2(node)
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if d := spec.Components.Schemas["StrDisc"].Discriminator; d == nil || d.PropertyName != "petType" {
		t.Errorf("StrDisc discriminator = %+v", d)
	}
	if d := spec.Components.Schemas["SeqDisc"].Discriminator; d != nil {
		t.Errorf("SeqDisc should yield nil discriminator, got %+v", d)
	}
	errorCount := 0
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Detail, "propertyName must not be empty") {
			errorCount++
		}
	}
	if errorCount != 2 {
		t.Errorf("expected 2 empty-propertyName errors, got %d (%v)", errorCount, diags)
	}
}

// TestConvertV2Edge_BadXML asserts a non-mapping xml value yields nil (no
// crash) rather than a diagnostic.
func TestConvertV2Edge_BadXML(t *testing.T) {
	spec := loadV2EdgeSpec(t)
	if x := spec.Components.Schemas["BadXML"].XML; x != nil {
		t.Errorf("BadXML xml = %+v, want nil", x)
	}
}
