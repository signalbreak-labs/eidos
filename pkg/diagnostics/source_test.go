package diagnostics_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

func TestSourceHelpers(t *testing.T) {
	loc := diagnostics.SourceLocation{File: "spec.yaml", Line: 42, Column: 5}

	t.Run("WithSourceLocation attaches when empty", func(t *testing.T) {
		d := diagnostics.Diagnostic{Severity: diagnostics.Error, Summary: "oops"}
		got := d.WithSourceLocation(loc)
		if got.SourceLocation == nil || got.SourceLocation.File != "spec.yaml" || got.SourceLocation.Line != 42 {
			t.Fatalf("expected source location attached, got %+v", got.SourceLocation)
		}
	})

	t.Run("WithSourceLocation preserves existing", func(t *testing.T) {
		existing := diagnostics.SourceLocation{File: "other.yaml", Line: 1}
		d := diagnostics.Diagnostic{Severity: diagnostics.Error, Summary: "oops", SourceLocation: &existing}
		got := d.WithSourceLocation(loc)
		if got.SourceLocation == nil || got.SourceLocation.File != "other.yaml" {
			t.Fatalf("expected existing source location preserved, got %+v", got.SourceLocation)
		}
	})

	t.Run("HasSourceLocation", func(t *testing.T) {
		if (diagnostics.Diagnostic{}).HasSourceLocation() {
			t.Error("empty diagnostic should not have source location")
		}
		if (diagnostics.Diagnostic{SourceLocation: &diagnostics.SourceLocation{}}).HasSourceLocation() {
			t.Error("empty source location should not count")
		}
		if (diagnostics.Diagnostic{SourceLocation: &diagnostics.SourceLocation{File: "x.yaml"}}).HasSourceLocation() {
			t.Error("file-only source location should not count")
		}
		if !(diagnostics.Diagnostic{SourceLocation: &diagnostics.SourceLocation{File: "x.yaml", Line: 3}}).HasSourceLocation() {
			t.Error("file and line should count")
		}
	})

	t.Run("EnsureSourceLocation", func(t *testing.T) {
		ds := diagnostics.Diagnostics{
			{Severity: diagnostics.Error, Summary: "a"},
			{Severity: diagnostics.Error, Summary: "b", SourceLocation: &diagnostics.SourceLocation{File: "b.yaml", Line: 2}},
		}
		got := ds.EnsureSourceLocation(loc)
		if !got[0].HasSourceLocation() {
			t.Fatalf("expected first diagnostic to receive location, got %+v", got[0].SourceLocation)
		}
		if got[1].SourceLocation.File != "b.yaml" {
			t.Fatalf("expected second diagnostic location preserved, got %+v", got[1].SourceLocation)
		}
	})
}

func TestAllParserDiagnosticsHaveSourceLocation(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		convert func(parser.Node, ...parser.ConvertOption) (*parser.Spec, []parser.Diagnostic, error)
	}{
		{
			name: "missing info v30",
			spec: `openapi: 3.0.3
paths: {}
`,
			convert: parser.ConvertV30,
		},
		{
			name: "unsupported keyword v30",
			spec: `openapi: 3.0.3
info:
  title: Test
  version: "1.0"
  unknownField: value
paths: {}
`,
			convert: parser.ConvertV30,
		},
		{
			name: "invalid ref v30",
			spec: `openapi: 3.0.3
info:
  title: Test
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
`,
			convert: parser.ConvertV30,
		},
		{
			name: "type mismatch v30",
			spec: `openapi: 3.0.3
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Pet:
      type:
        - string
        - null
`,
			convert: parser.ConvertV30,
		},
		{
			name: "mixed body and formdata v2",
			spec: `swagger: "2.0"
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
`,
			convert: parser.ConvertV2,
		},
		{
			name: "unrecognized oauth2 flow v2",
			spec: `swagger: "2.0"
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
`,
			convert: parser.ConvertV2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join("testdata", tc.name+".yaml")
			node, err := parser.LoadFile(file, []byte(tc.spec))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}

			version, versionDiags := parser.DetectVersion(node)
			spec, convertDiags, err := tc.convert(node)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			diags := versionDiags
			diags = append(diags, convertDiags...)
			if spec != nil {
				diags = append(diags, parser.Validate(node, spec, version)...)
			}

			// Each case is a deliberately invalid spec that must emit at least
			// one diagnostic; without this guard a regression that stops
			// emitting a diagnostic would pass vacuously (L-23).
			if len(diags) == 0 {
				t.Errorf("expected at least one diagnostic for %s, got none", tc.name)
			}
			for _, d := range diags {
				if !d.HasSourceLocation() {
					t.Errorf("diagnostic missing file:line: %s", d.String())
				}
				str := d.String()
				// A located diagnostic starts with "path:line" and therefore contains a colon before the severity label.
				if !strings.Contains(str, ":") || strings.HasPrefix(str, "error:") || strings.HasPrefix(str, "warning:") || strings.HasPrefix(str, "info:") {
					t.Errorf("diagnostic string lacks location prefix: %s", str)
				}
			}
		})
	}
}

func TestGoldenDiagnosticMessages(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		convert func(parser.Node, ...parser.ConvertOption) (*parser.Spec, []parser.Diagnostic, error)
	}{
		{
			name: "missing_info",
			spec: `openapi: 3.0.3
paths: {}
`,
			convert: parser.ConvertV30,
		},
		{
			name: "unsupported_keyword",
			spec: `openapi: 3.0.3
info:
  title: Test
  version: "1.0"
  unknownField: value
paths: {}
`,
			convert: parser.ConvertV30,
		},
		{
			name: "invalid_ref",
			spec: `openapi: 3.0.3
info:
  title: Test
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
`,
			convert: parser.ConvertV30,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join("testdata", tc.name+".yaml")
			node, err := parser.LoadFile(file, []byte(tc.spec))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			version, _ := parser.DetectVersion(node)
			spec, convertDiags, err := tc.convert(node)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			diags := convertDiags
			if spec != nil {
				diags = append(diags, parser.Validate(node, spec, version)...)
			}

			want := readGolden(t, tc.name+".golden")
			got := formatDiagnostics(diags)
			if got != want {
				t.Errorf("golden mismatch (-got +want):\n-got:\n%s\n+want:\n%s", got, want)
			}
		})
	}
}

func formatDiagnostics(diags []parser.Diagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, "\n")
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return strings.TrimRight(string(data), "\n\r")
}

func TestGoldenDiagnosticMessagesNilRoot(t *testing.T) {
	_, diags := parser.DetectVersion(nil)
	want := readGolden(t, "nil_root.golden")
	got := formatDiagnostics(diags)
	if got != want {
		t.Errorf("golden mismatch (-got +want):\n-got:\n%s\n+want:\n%s", got, want)
	}
}
