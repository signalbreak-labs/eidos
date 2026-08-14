package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestParseImportIDFormat verifies that import ID format strings are parsed
// into simple and composite attribute mappings.
func TestParseImportIDFormat(t *testing.T) {
	cases := []struct {
		name       string
		format     string
		idAttr     string
		wantAttrs  []string
		wantDelim  string
		wantSimple bool
		wantErr    bool
	}{
		{
			name:       "empty defaults to id",
			format:     "",
			idAttr:     "",
			wantAttrs:  []string{"id"},
			wantSimple: true,
		},
		{
			name:       "empty honors id_attribute",
			format:     "",
			idAttr:     "petId",
			wantAttrs:  []string{"petId"},
			wantSimple: true,
		},
		{
			name:       "bare token",
			format:     "petId",
			idAttr:     "",
			wantAttrs:  []string{"petId"},
			wantSimple: true,
		},
		{
			name:       "bare token mapped to id_attribute",
			format:     "petId",
			idAttr:     "id",
			wantAttrs:  []string{"id"},
			wantSimple: true,
		},
		{
			name:       "braced token mapped to id_attribute",
			format:     "{petId}",
			idAttr:     "id",
			wantAttrs:  []string{"id"},
			wantSimple: true,
		},
		{
			name:       "path template keeps only braced parameter",
			format:     "/pets/{petId}",
			idAttr:     "",
			wantAttrs:  []string{"petId"},
			wantSimple: true,
		},
		{
			name:       "path template maps braced parameter to id_attribute",
			format:     "/pets/{petId}",
			idAttr:     "id",
			wantAttrs:  []string{"id"},
			wantSimple: true,
		},
		{
			name:       "composite braced colon",
			format:     "{project_id}:{resource_id}",
			idAttr:     "",
			wantAttrs:  []string{"project_id", "resource_id"},
			wantDelim:  ":",
			wantSimple: false,
		},
		{
			name:       "composite braced slash",
			format:     "{project_id}/{resource_id}",
			idAttr:     "",
			wantAttrs:  []string{"project_id", "resource_id"},
			wantDelim:  "/",
			wantSimple: false,
		},
		{
			name:       "composite multi-char delimiter",
			format:     "{project_id}::{resource_id}",
			idAttr:     "",
			wantAttrs:  []string{"project_id", "resource_id"},
			wantDelim:  "::",
			wantSimple: false,
		},
		{
			name:       "composite three braced attributes",
			format:     "{a},{b},{c}",
			idAttr:     "",
			wantAttrs:  []string{"a", "b", "c"},
			wantDelim:  ",",
			wantSimple: false,
		},
		{
			name:    "unbraced composite colon is an error",
			format:  "project_id:resource_id",
			idAttr:  "",
			wantErr: true,
		},
		{
			name:    "unbraced composite slash is an error",
			format:  "project_id/resource_id",
			idAttr:  "",
			wantErr: true,
		},
		{
			name:    "braces with no attribute is an error",
			format:  "{}",
			idAttr:  "",
			wantErr: true,
		},
		{
			name:    "inconsistent delimiters is an error",
			format:  "a:b,c",
			idAttr:  "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseImportIDFormat(tc.format, tc.idAttr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseImportIDFormat(%q, %q) error = %v, wantErr %v", tc.format, tc.idAttr, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got.simple != tc.wantSimple {
				t.Errorf("simple = %v, want %v", got.simple, tc.wantSimple)
			}
			if got.delimiter != tc.wantDelim {
				t.Errorf("delimiter = %q, want %q", got.delimiter, tc.wantDelim)
			}
			if !stringSlicesEqual(got.attrs, tc.wantAttrs) {
				t.Errorf("attrs = %v, want %v", got.attrs, tc.wantAttrs)
			}
		})
	}
}

// TestResourceFile_ImportState_Simple verifies that a resource with a simple
// import format stores the whole import ID in its identifier attribute.
func TestResourceFile_ImportState_Simple(t *testing.T) {
	r := sampleResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *PetResource) ImportState",
		"path.Root(\"id\")",
		"req.ID",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}

	// A simple resource should not generate composite parsing code.
	absent := []string{
		"strings.Split(req.ID",
		"ImportIDParts",
		"Unexpected Import Identifier",
	}
	for _, want := range absent {
		if strings.Contains(got, want) {
			t.Errorf("generated resource file unexpectedly contains %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_ImportState_IntID verifies that a resource with an integer
// identifier parses the string import ID with strconv.ParseInt before storing
// it, because storing a Go string into an Int64 attribute makes the framework's
// SetAttribute fail with a tftypes conversion error ("can't unmarshal
// tftypes.String into *big.Float"). A failed parse surfaces a loud diagnostic
// instead of that confusing value-conversion error.
func TestResourceFile_ImportState_IntID(t *testing.T) {
	r := sampleResourceIR()
	for i := range r.Schema.Attributes {
		if r.Schema.Attributes[i].Name == "id" {
			r.Schema.Attributes[i].Schema.Type = ir.TypeInt
		}
	}

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *PetResource) ImportState",
		`importId, err := strconv.ParseInt(req.ID, 10, 64)`,
		`resp.State.SetAttribute(ctx, path.Root("id"), importId)`,
		`"Error importing mycloud_pet"`,
		`Could not parse import identifier %q as an integer: %s`,
		`"strconv"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
	// The string-typed path must be absent: req.ID is parsed before being stored.
	if strings.Contains(got, `resp.State.SetAttribute(ctx, path.Root("id"), req.ID)`) {
		t.Errorf("generated resource file must not store req.ID verbatim into an Int64 attribute\ncontent:\n%s", got)
	}
}

// TestResourceFile_ImportState_SingleBrace verifies that a resource with a
// single braced import attribute and no explicit id_attribute emits
// path.Root for that parsed attribute name.
func TestResourceFile_ImportState_SingleBrace(t *testing.T) {
	r := sampleResourceIR()
	r.IDAttribute = ""
	r.ImportIDFormat = "{petId}"

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *PetResource) ImportState",
		"path.Root(\"petId\")",
		"req.ID",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}

	absent := []string{
		"path.Root(\"id\")",
		"strings.Split(req.ID",
	}
	for _, want := range absent {
		if strings.Contains(got, want) {
			t.Errorf("generated resource file unexpectedly contains %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_ImportState_PathTemplate verifies that an import format
// derived from a URI template, such as "/pets/{petId}", keeps only the braced
// parameter and emits path.Root for that attribute.
func TestResourceFile_ImportState_PathTemplate(t *testing.T) {
	r := sampleResourceIR()
	r.IDAttribute = ""
	r.ImportIDFormat = "/pets/{petId}"

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *PetResource) ImportState",
		"path.Root(\"petId\")",
		"req.ID",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}

	absent := []string{
		"path.Root(\"id\")",
		"strings.Split(req.ID",
		// NOTE: the API path template (e.g. "/pets/") legitimately appears in
		// the file now that CRUD bodies are wired to the client; only the
		// ImportState-specific assertions above remain.
	}
	for _, want := range absent {
		if strings.Contains(got, want) {
			t.Errorf("generated resource file unexpectedly contains %q\ncontent:\n%s", want, got)
		}
	}
}

// TestResourceFile_ImportState_MalformedFormat verifies that malformed
// ImportIDFormat values are rejected at generation time rather than deferred
// to runtime diagnostics.
func TestResourceFile_ImportState_MalformedFormat(t *testing.T) {
	cases := []struct {
		name   string
		format string
	}{
		{
			name:   "empty braces",
			format: "{}",
		},
		{
			name:   "inconsistent delimiters",
			format: "{a}:{b},{c}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := sampleResourceIR()
			r.ImportIDFormat = tc.format

			file := ResourceFile(r, testClientImport)
			var buf bytes.Buffer
			err := file.Render(&buf)
			if err == nil {
				t.Fatalf("Render() expected error for malformed import format %q, got nil", tc.format)
			}
			got := err.Error()
			for _, want := range []string{
				"invalid ImportIDFormat",
				tc.format,
			} {
				if !strings.Contains(got, want) {
					t.Errorf("error missing %q\nerror:\n%s", want, got)
				}
			}
		})
	}
}

// TestResourceFile_ImportState_Composite verifies that a resource with a
// composite import format splits the import ID and stores each segment in the
// matching attribute.
func TestResourceFile_ImportState_Composite(t *testing.T) {
	r := sampleCompositeResourceIR()

	file := ResourceFile(r, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"func (r *WidgetResource) ImportState",
		"widgetImportIDParts := strings.Split(req.ID, \":\")",
		"len(widgetImportIDParts) != 2",
		"Unexpected Import Identifier",
		"Expected import identifier with format",
		"path.Root(\"project_id\")",
		"path.Root(\"widget_id\")",
		"widgetImportIDParts[0]",
		"widgetImportIDParts[1]",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated resource file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// sampleCompositeResourceIR returns a ResourceIR that uses a composite import
// identifier.
func sampleCompositeResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:        "widget",
		TypeName:    "mycloud_widget",
		Description: "A widget resource.",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "project_id",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "widget_id",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "name",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Read: ir.OperationMappingIR{
				Method:       "GET",
				PathTemplate: "/projects/{project_id}/widgets/{widget_id}",
			},
		},
		Importable:     true,
		ImportIDFormat: "{project_id}:{widget_id}",
	}
}

// stringSlicesEqual reports whether a and b contain the same elements in the
// same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
