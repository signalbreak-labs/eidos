package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocalReferencesV30(t *testing.T) {
	dir := t.TempDir()
	entry := writeParserRefFile(t, dir, "openapi.yaml", `
openapi: 3.0.3
info: {title: Pets, version: 1.0.0}
paths:
  /pets:
    $ref: ./aliases.yaml#/Path
components:
  schemas:
    Pet: {$ref: ./aliases.yaml#/Schema}
  parameters:
    PetID: {$ref: ./aliases.yaml#/Parameter}
  requestBodies:
    PetBody: {$ref: ./aliases.yaml#/RequestBody}
  responses:
    PetResponse: {$ref: ./aliases.yaml#/Response}
  securitySchemes:
    ApiKey: {$ref: ./aliases.yaml#/Security}
`)
	writeParserRefFile(t, dir, "aliases.yaml", `
Path: {$ref: ./targets.yaml#/Path}
Schema: {$ref: ./targets.yaml#/Schema}
Parameter: {$ref: ./targets.yaml#/Parameter}
RequestBody: {$ref: ./targets.yaml#/RequestBody}
Response: {$ref: ./targets.yaml#/Response}
Security: {$ref: ./targets.yaml#/Security}
`)
	writeParserRefFile(t, dir, "targets.yaml", `
Path:
  get:
    operationId: listPets
    responses:
      '200': {description: pets}
Schema: {type: object}
Parameter: {name: id, in: path, required: true, schema: {type: string}}
RequestBody: {required: true, content: {application/json: {schema: {type: object}}}}
Response: {description: a pet, content: {application/json: {schema: {type: object}}}}
Security: {type: apiKey, in: header, name: X-API-Key}
`)

	_, spec := parseParserRefEntry(t, entry)
	loc := spec.SourceLocation
	check := func(name string, diags []Diagnostic) {
		t.Helper()
		if len(diags) > 0 {
			t.Fatalf("%s diagnostics = %v", name, diags)
		}
	}

	if key := spec.ReferenceKey("./aliases.yaml#/Path", loc); !strings.HasSuffix(key, "aliases.yaml#/Path") {
		t.Fatalf("ReferenceKey() = %q, want document-qualified path", key)
	}
	schema, diags := spec.ResolveSchemaReference(&Schema{Ref: "#/components/schemas/Pet", SourceLocation: loc})
	check("schema component", diags)
	schema, diags = spec.ResolveSchemaReference(schema)
	check("schema alias", diags)
	schema, diags = spec.ResolveSchemaReference(schema)
	check("schema target", diags)
	if schema.Type != "object" {
		t.Fatalf("schema = %+v, want resolved object", schema)
	}

	parameter, diags := spec.ResolveParameterReference(&Parameter{Ref: "#/components/parameters/PetID", SourceLocation: loc})
	check("parameter", diags)
	if parameter.Name != "id" {
		t.Fatalf("parameter = %+v, want resolved id", parameter)
	}
	body, diags := spec.ResolveRequestBodyReference(&RequestBody{Ref: "#/components/requestBodies/PetBody", SourceLocation: loc})
	check("request body", diags)
	if !body.Required {
		t.Fatalf("request body = %+v, want required", body)
	}
	response, diags := spec.ResolveResponseReference(&Response{Ref: "#/components/responses/PetResponse", SourceLocation: loc})
	check("response", diags)
	if response.Description != "a pet" {
		t.Fatalf("response = %+v, want resolved description", response)
	}
	pathItem, diags := spec.ResolvePathItemReference(spec.Paths["/pets"])
	check("path item", diags)
	if pathItem.Get == nil || pathItem.Get.OperationID != "listPets" {
		t.Fatalf("path item = %+v, want resolved GET", pathItem)
	}
	scheme, diags := spec.ResolveSecuritySchemeReference(&SecurityScheme{Ref: "#/components/securitySchemes/ApiKey", SourceLocation: loc})
	check("security scheme", diags)
	if scheme.Type != "apiKey" {
		t.Fatalf("security scheme = %+v, want resolved API key", scheme)
	}

	// A second traversal hits the typed cache without changing the result.
	cached, diags := spec.ResolvePathItemReference(spec.Paths["/pets"])
	check("cached path item", diags)
	if cached.Get == nil || cached.Get.OperationID != "listPets" {
		t.Fatalf("cached path item = %+v", cached)
	}
}

func TestResolveLocalReferencesV2(t *testing.T) {
	dir := t.TempDir()
	entry := writeParserRefFile(t, dir, "swagger.yaml", `
swagger: '2.0'
info: {title: Pets, version: 1.0.0}
host: pets.example.com
produces: [application/json]
consumes: [application/json]
schemes: [https]
paths:
  /pets: {$ref: ./parts.yaml#/Path}
definitions:
  Pet: {$ref: ./parts.yaml#/Schema}
parameters:
  PetID: {$ref: ./parts.yaml#/Parameter}
responses:
  PetResponse: {$ref: ./parts.yaml#/Response}
`)
	writeParserRefFile(t, dir, "parts.yaml", `
Path:
  get:
    operationId: listPets
    responses:
      '200': {description: pets, schema: {$ref: '#/Schema'}}
Schema: {type: object}
Parameter: {name: id, in: path, required: true, type: string}
Response: {description: a pet, schema: {$ref: '#/Schema'}}
Security: {type: apiKey, in: header, name: X-API-Key}
`)

	_, spec := parseParserRefEntryVersion(t, entry, Version2_0)
	if schema, diags := spec.ResolveSchemaReference(spec.Components.Schemas["Pet"]); len(diags) > 0 || schema.Type != "object" {
		t.Fatalf("schema = %+v, diagnostics = %v", schema, diags)
	}
	if parameter, diags := spec.ResolveParameterReference(spec.Components.Parameters["PetID"]); len(diags) > 0 || parameter.Name != "id" {
		t.Fatalf("parameter = %+v, diagnostics = %v", parameter, diags)
	}
	if response, diags := spec.ResolveResponseReference(spec.Components.Responses["PetResponse"]); len(diags) > 0 || response.Description != "a pet" {
		t.Fatalf("response = %+v, diagnostics = %v", response, diags)
	}
	if pathItem, diags := spec.ResolvePathItemReference(spec.Paths["/pets"]); len(diags) > 0 || pathItem.Get == nil {
		t.Fatalf("path item = %+v, diagnostics = %v", pathItem, diags)
	}
	scheme, diags := spec.ResolveSecuritySchemeReference(&SecurityScheme{
		Ref:            "./parts.yaml#/Security",
		SourceLocation: spec.SourceLocation,
	})
	if len(diags) > 0 || scheme.Type != "apiKey" {
		t.Fatalf("security scheme = %+v, diagnostics = %v", scheme, diags)
	}
}

func TestValidateLocalReferences_MissingNestedFileUsesReferrerLocation(t *testing.T) {
	dir := t.TempDir()
	entry := writeParserRefFile(t, dir, "openapi.yaml", `
openapi: 3.0.3
info: {title: Pets, version: 1.0.0}
paths: {}
components:
  schemas:
    Pet:
      $ref: ./schemas/pet.yaml#/Pet
`)
	referrer := writeParserRefFile(t, dir, "schemas/pet.yaml", `
Pet:
  type: object
  properties:
    friend:
      $ref: ./missing.yaml#/Missing
`)

	diags := loadParserSpecWithLocalRefs(t, entry)
	diagnostic := findParserDiagnostic(diags, "Unresolvable $ref")
	if diagnostic == nil {
		t.Fatalf("diagnostics = %v, want missing-file error", diags)
	}
	canonicalReferrer, err := filepath.EvalSymlinks(referrer)
	if err != nil {
		t.Fatalf("canonicalize referrer: %v", err)
	}
	if diagnostic.SourceLocation == nil || diagnostic.SourceLocation.File != canonicalReferrer || diagnostic.SourceLocation.Line == 0 {
		t.Fatalf("source location = %+v, want referenced file position", diagnostic.SourceLocation)
	}
}

func TestValidateLocalReferences_RejectsRemoteRef(t *testing.T) {
	dir := t.TempDir()
	entry := writeParserRefFile(t, dir, "openapi.yaml", `
openapi: 3.0.3
info: {title: Pets, version: 1.0.0}
paths: {}
components:
  schemas:
    Pet:
      $ref: https://example.com/pet.yaml#/Pet
`)

	diags := loadParserSpecWithLocalRefs(t, entry)
	if findParserDiagnostic(diags, "Unsupported remote $ref") == nil {
		t.Fatalf("diagnostics = %v, want unsupported-remote error", diags)
	}
}

func TestValidateLocalReferences_EnforcesLimits(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		dir := t.TempDir()
		entry := writeParserRefFile(t, dir, "openapi.yaml", localRefLimitEntry("./schema.yaml#/Schema"))
		writeParserRefFile(t, dir, "schema.yaml", "Schema:\n  type: string\n")
		root, spec := parseParserRefEntry(t, entry)
		spec.localRefs.totalBytes = maxReferenceBytes
		diags := Validate(root, spec, Version3_0)
		if findParserDiagnostic(diags, "Reference byte limit exceeded") == nil {
			t.Fatalf("diagnostics = %v, want byte-limit error", diags)
		}
	})

	t.Run("documents", func(t *testing.T) {
		dir := t.TempDir()
		entry := writeParserRefFile(t, dir, "openapi.yaml", localRefLimitEntry("./chain/0.yaml"))
		for i := 0; i < maxReferenceDocuments; i++ {
			contents := "value: done\n"
			if i+1 < maxReferenceDocuments {
				contents = fmt.Sprintf("$ref: ./%d.yaml\n", i+1)
			}
			writeParserRefFile(t, dir, fmt.Sprintf("chain/%d.yaml", i), contents)
		}
		diags := loadParserSpecWithLocalRefs(t, entry)
		if findParserDiagnostic(diags, "Reference document limit exceeded") == nil {
			t.Fatalf("diagnostics = %v, want document-limit error", diags)
		}
	})

	t.Run("depth", func(t *testing.T) {
		dir := t.TempDir()
		entry := writeParserRefFile(t, dir, "openapi.yaml", localRefLimitEntry("./chain.yaml#/n0"))
		entries := make([]string, 0, maxReferenceDepth+1)
		for i := 0; i <= maxReferenceDepth; i++ {
			if i == maxReferenceDepth {
				entries = append(entries, fmt.Sprintf("n%d:\n  type: string", i))
				continue
			}
			entries = append(entries, fmt.Sprintf("n%d:\n  $ref: '#/n%d'", i, i+1))
		}
		writeParserRefFile(t, dir, "chain.yaml", strings.Join(entries, "\n")+"\n")
		diags := loadParserSpecWithLocalRefs(t, entry)
		if findParserDiagnostic(diags, "Reference depth limit exceeded") == nil {
			t.Fatalf("diagnostics = %v, want depth-limit error", diags)
		}
	})
}

func localRefLimitEntry(ref string) string {
	return fmt.Sprintf(`
openapi: 3.0.3
info: {title: Limits, version: 1.0.0}
paths: {}
components:
  schemas:
    Value:
      $ref: %s
`, ref)
}

func loadParserSpecWithLocalRefs(t *testing.T, entry string) []Diagnostic {
	t.Helper()
	root, spec := parseParserRefEntry(t, entry)
	return Validate(root, spec, Version3_0)
}

func parseParserRefEntry(t *testing.T, entry string) (Node, *Spec) {
	t.Helper()
	return parseParserRefEntryVersion(t, entry, Version3_0)
}

func parseParserRefEntryVersion(t *testing.T, entry string, version Version) (Node, *Spec) {
	t.Helper()
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry spec: %v", err)
	}
	root, err := LoadFile(entry, data)
	if err != nil {
		t.Fatalf("parse entry spec: %v", err)
	}
	var spec *Spec
	var diags []Diagnostic
	if version == Version2_0 {
		spec, diags, err = ConvertV2(root)
	} else {
		spec, diags, err = ConvertV30(root)
	}
	if err != nil {
		t.Fatalf("convert entry spec: %v", err)
	}
	if findParserDiagnostic(diags, "") != nil {
		t.Fatalf("unexpected conversion diagnostics: %v", diags)
	}
	if err := EnableLocalReferences(spec, root, entry, version); err != nil {
		t.Fatalf("EnableLocalReferences() error = %v", err)
	}
	return root, spec
}

func findParserDiagnostic(diags []Diagnostic, summary string) *Diagnostic {
	for i := range diags {
		if summary == "" || diags[i].Summary == summary {
			return &diags[i]
		}
	}
	return nil
}

func writeParserRefFile(t *testing.T, root, name, contents string) string {
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
