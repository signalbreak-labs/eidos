package parser

import (
	"fmt"
	"testing"
)

func TestDetectVersionSwagger20(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test API
  version: 1.0.0
`)
	node, err := LoadFile("swagger.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != Version2_0 {
		t.Fatalf("expected version %q, got %q", Version2_0, ver)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}

func TestDetectVersionSwagger20Unquoted(t *testing.T) {
	// Unquoted `swagger: 2.0` is inferred as float64 by the YAML lexer. Version
	// detection must fall back to ScalarNode.Raw so that Swagger 2.0 is still
	// recognized. This documents the workaround noted in T-1eb20c99.
	data := []byte(`swagger: 2.0
info:
  title: Test API
`)
	node, err := LoadFile("swagger.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != Version2_0 {
		t.Fatalf("expected version %q, got %q", Version2_0, ver)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}

func TestDetectVersionOpenAPI30(t *testing.T) {
	cases := []string{"3.0", "3.0.0", "3.0.1", "3.0.2", "3.0.3"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`openapi: %s
info:
  title: Test API
  version: 1.0.0
`, v))
			node, err := LoadFile("openapi.yaml", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			ver, diags := DetectVersion(node)
			if ver != Version3_0 {
				t.Fatalf("expected version %q, got %q", Version3_0, ver)
			}
			if len(diags) != 0 {
				t.Fatalf("expected no diagnostics, got %v", diags)
			}
		})
	}
}

func TestDetectVersionOpenAPI31(t *testing.T) {
	cases := []string{"3.1.0", "3.1.1"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{
  "openapi": %q,
  "info": { "title": "Test API", "version": "1.0.0" }
}`, v))
			node, err := LoadFile("openapi.json", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			ver, diags := DetectVersion(node)
			if ver != Version3_1 {
				t.Fatalf("expected version %q, got %q", Version3_1, ver)
			}
			if len(diags) != 0 {
				t.Fatalf("expected no diagnostics, got %v", diags)
			}
		})
	}
}

func TestDetectVersionMissing(t *testing.T) {
	data := []byte(`info:
  title: Test API
  version: 1.0.0
`)
	node, err := LoadFile("missing.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != VersionUnknown {
		t.Fatalf("expected version unknown, got %q", ver)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError {
		t.Fatalf("expected error severity, got %q", diags[0].Severity)
	}
	if diags[0].Summary != "Missing OpenAPI version" {
		t.Fatalf("unexpected summary: %q", diags[0].Summary)
	}
	if diags[0].SourceLocation.File != "missing.yaml" || diags[0].SourceLocation.Line != 1 {
		t.Fatalf("unexpected source location: %+v", diags[0].SourceLocation)
	}
}

func TestDetectVersionBothFields(t *testing.T) {
	data := []byte(`openapi: 3.0.3
swagger: "2.0"
info:
  title: Test API
`)
	node, err := LoadFile("both.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != VersionUnknown {
		t.Fatalf("expected version unknown, got %q", ver)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Ambiguous OpenAPI version fields" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestDetectVersionUnsupportedSwagger(t *testing.T) {
	data := []byte(`swagger: "2.1"
info:
  title: Test API
`)
	node, err := LoadFile("swagger21.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != VersionUnknown {
		t.Fatalf("expected version unknown, got %q", ver)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Unsupported Swagger version" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestDetectVersionUnsupportedOpenAPI(t *testing.T) {
	// "3.0.0.1" has more than three segments and must be rejected; the prior
	// check validated only the first three and silently ignored the trailing
	// segment (L-91).
	cases := []string{"2.0", "3.2.0", "4.0.0", "3.0.0.1"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			data := []byte(fmt.Sprintf("openapi: %q\ninfo:\n  title: Test API\n", v))
			node, err := LoadFile("unsupported.yaml", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			ver, diags := DetectVersion(node)
			if ver != VersionUnknown {
				t.Fatalf("expected version unknown, got %q", ver)
			}
			if len(diags) != 1 {
				t.Fatalf("expected one diagnostic, got %v", diags)
			}
			if diags[0].Severity != SeverityError || diags[0].Summary != "Unsupported OpenAPI version" {
				t.Fatalf("unexpected diagnostic: %+v", diags[0])
			}
		})
	}
}

func TestDetectVersionOpenAPI30QuotedNoPatch(t *testing.T) {
	// Explicitly quoted two-part strings are accepted as Version3_0.
	data := []byte(`openapi: "3.0"
info:
  title: Test API
`)
	node, err := LoadFile("openapi30.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != Version3_0 {
		t.Fatalf("expected version %q, got %q", Version3_0, ver)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}

func TestDetectVersionOpenAPI31QuotedNoPatch(t *testing.T) {
	// Explicitly quoted two-part strings are accepted as Version3_1.
	data := []byte(`openapi: "3.1"
info:
  title: Test API
`)
	node, err := LoadFile("openapi31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	ver, diags := DetectVersion(node)
	if ver != Version3_1 {
		t.Fatalf("expected version %q, got %q", Version3_1, ver)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
}

func TestDetectVersionNonMapRoot(t *testing.T) {
	node := &SequenceNode{Items: []Node{&ScalarNode{Value: "openapi"}}}
	ver, diags := DetectVersion(node)
	if ver != VersionUnknown {
		t.Fatalf("expected version unknown, got %q", ver)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Invalid OpenAPI document" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestDetectVersionNilRoot(t *testing.T) {
	ver, diags := DetectVersion(nil)
	if ver != VersionUnknown {
		t.Fatalf("expected version unknown, got %q", ver)
	}
	if len(diags) != 1 {
		t.Fatalf("expected one diagnostic, got %v", diags)
	}
	if diags[0].Severity != SeverityError || diags[0].Summary != "Missing OpenAPI version" {
		t.Fatalf("unexpected diagnostic: %+v", diags[0])
	}
}

func TestDetectVersionNonStringValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"boolean", "true"},
		{"null", "null"},
		{"out_of_range_integer", "4.0"},
		{"out_of_range_float", "4.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`openapi: %s
info:
  title: Test API
`, tc.value))
			node, err := LoadFile("nonstring.yaml", data)
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			ver, diags := DetectVersion(node)
			if ver != VersionUnknown {
				t.Fatalf("expected version unknown, got %q", ver)
			}
			if len(diags) != 1 {
				t.Fatalf("expected one diagnostic, got %v", diags)
			}
			if diags[0].Severity != SeverityError || diags[0].Summary != "Unsupported OpenAPI version" {
				t.Fatalf("unexpected diagnostic: %+v", diags[0])
			}
		})
	}
}
