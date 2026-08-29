package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry spec: %v", err)
	}
	root, err := LoadFile(entry, data)
	if err != nil {
		t.Fatalf("parse entry spec: %v", err)
	}
	spec, diags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("convert entry spec: %v", err)
	}
	if findParserDiagnostic(diags, "") != nil {
		t.Fatalf("unexpected conversion diagnostics: %v", diags)
	}
	if err := EnableLocalReferences(spec, root, entry, Version3_0); err != nil {
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
