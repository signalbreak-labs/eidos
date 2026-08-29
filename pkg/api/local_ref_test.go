package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestBuildProviderIRWithName_LocalMultiFileRefs(t *testing.T) {
	dir := t.TempDir()
	entry := writeLocalRefFile(t, dir, "openapi.yaml", `
openapi: 3.0.3
info:
  title: Multi File Pets
  version: 1.0.0
paths:
  /pets:
    $ref: ./paths/aliases.yaml#/Pets
  /pets/{id}:
    $ref: ./paths/pet.yaml
security:
  - ApiKey: []
components:
  securitySchemes:
    ApiKey:
      $ref: ./components/security.yaml#/ApiKey
`)
	writeLocalRefFile(t, dir, "paths/aliases.yaml", `
Pets:
  $ref: ./pets.yaml
`)
	writeLocalRefFile(t, dir, "paths/pets.yaml", `
post:
  operationId: createPet
  requestBody:
    $ref: ../operations/aliases.yaml#/requestBody
  responses:
    '201':
      $ref: ../operations/aliases.yaml#/response
`)
	writeLocalRefFile(t, dir, "paths/pet.yaml", `
parameters:
  - $ref: ../components/aliases.yaml#/PetID
get:
  operationId: getPet
  responses:
    '200':
      $ref: ../operations/pet.yaml#/response
delete:
  operationId: deletePet
  responses:
    '204':
      description: deleted
`)
	writeLocalRefFile(t, dir, "operations/aliases.yaml", `
requestBody:
  $ref: ./pet.yaml#/requestBody
response:
  $ref: ./pet.yaml#/response
`)
	writeLocalRefFile(t, dir, "operations/pet.yaml", `
requestBody:
  required: true
  content:
    application/json:
      schema:
        $ref: ../schemas/pet.yaml#/Pet
response:
  description: a pet
  content:
    application/json:
      schema:
        $ref: ../schemas/pet.yaml#/Pet
`)
	writeLocalRefFile(t, dir, "components/parameters.yaml", `
PetID:
  name: id
  in: path
  required: true
  schema:
    type: string
`)
	writeLocalRefFile(t, dir, "components/aliases.yaml", `
PetID:
  $ref: ./parameters.yaml#/PetID
`)
	writeLocalRefFile(t, dir, "components/security.yaml", `
ApiKey:
  $ref: ./auth.yaml#/ApiKey
`)
	writeLocalRefFile(t, dir, "components/auth.yaml", `
ApiKey:
  type: apiKey
  in: header
  name: X-API-Key
`)
	writeLocalRefFile(t, dir, "schemas/pet.yaml", `
Pet:
  type: object
  required: [id, name]
  properties:
    id:
      type: string
    name:
      type: string
    profile:
      $ref: ./profile.json#/$defs/Profile
`)
	writeLocalRefFile(t, dir, "schemas/profile.json", `{
  "$defs": {
    "Profile": {
      "type": "object",
      "properties": {
        "nickname": {"type": "string"},
        "pet": {"$ref": "./pet.yaml#/Pet"}
      }
    }
  }
}`)

	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry spec: %v", err)
	}
	provider, _, diags, err := BuildProviderIRWithName(data, entry, "", nil)
	if err != nil {
		t.Fatalf("BuildProviderIRWithName() error = %v", err)
	}
	if diags.HasErrors() {
		t.Fatalf("BuildProviderIRWithName() diagnostics = %v", diags)
	}
	if len(provider.Resources) != 1 {
		t.Fatalf("resources = %d, want one wired resource", len(provider.Resources))
	}
	resource := provider.Resources[0]
	if resource.CRUDMapping.Create.Method != http.MethodPost ||
		resource.CRUDMapping.Read.Method != http.MethodGet ||
		resource.CRUDMapping.Delete.Method != http.MethodDelete {
		t.Fatalf("CRUD mapping = %+v, want POST/GET/DELETE", resource.CRUDMapping)
	}
	if len(resource.CRUDMapping.Read.PathParams) != 1 || resource.CRUDMapping.Read.PathParams[0].Name != "id" {
		t.Fatalf("read path params = %+v, want external id parameter", resource.CRUDMapping.Read.PathParams)
	}
	if !hasIRAttribute(resource.Schema.Attributes, "name") {
		t.Fatalf("resource attributes = %+v, want schema from sibling file", resource.Schema.Attributes)
	}
	if len(provider.SecurityIR.Schemes) != 1 || provider.SecurityIR.Schemes[0].Type != ir.SecuritySchemeAPIKey {
		t.Fatalf("security schemes = %+v, want chained external API key", provider.SecurityIR.Schemes)
	}
}

func TestBuildProviderIRWithName_LocalRefsVersionParity(t *testing.T) {
	cases := map[string]string{
		"swagger-2": `
swagger: "2.0"
info: {title: Pets, version: 1.0.0}
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: pets
          schema:
            $ref: ./pet.yaml#/Pet
`,
		"openapi-3.1": `
openapi: 3.1.0
info: {title: Pets, version: 1.0.0}
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: pets
          content:
            application/json:
              schema:
                $ref: ./pet.yaml#/Pet
`,
	}
	for name, entryContents := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			entry := writeLocalRefFile(t, dir, "openapi.yaml", entryContents)
			writeLocalRefFile(t, dir, "pet.yaml", "Pet:\n  type: object\n  properties:\n    name:\n      type: string\n")
			data, err := os.ReadFile(entry)
			if err != nil {
				t.Fatalf("read entry spec: %v", err)
			}
			provider, _, diags, err := BuildProviderIRWithName(data, entry, "", nil)
			if err != nil {
				t.Fatalf("BuildProviderIRWithName() error = %v", err)
			}
			if diags.HasErrors() || len(provider.DataSources) != 1 {
				t.Fatalf("provider = %+v, diagnostics = %v; want one resolved data source", provider, diags)
			}
		})
	}
}

func TestParseSpec_DisplayNameDoesNotAuthorizeLocalRefs(t *testing.T) {
	dir := t.TempDir()
	entry := writeLocalRefFile(t, dir, "openapi.yaml", `
openapi: 3.0.3
info: {title: Pets, version: 1.0.0}
paths: {}
components:
  schemas:
    Pet:
      $ref: ./pet.yaml#/Pet
`)
	writeLocalRefFile(t, dir, "pet.yaml", "Pet:\n  type: string\n")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry spec: %v", err)
	}

	if _, diags, parseErr := ParseSpec(data, entry); parseErr != nil || !diags.HasErrors() {
		t.Fatalf("ParseSpec() error = %v, diagnostics = %v; want display-only path to reject file ref", parseErr, diags)
	}
	if _, diags, parseErr := ParseSpecWithName(data, entry); parseErr != nil || diags.HasErrors() {
		t.Fatalf("ParseSpecWithName() error = %v, diagnostics = %v; want local file ref resolved", parseErr, diags)
	}
}

func writeLocalRefFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func hasIRAttribute(attributes []ir.AttributeIR, name string) bool {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return true
		}
	}
	return false
}
