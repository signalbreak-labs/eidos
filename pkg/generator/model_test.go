package generator

import (
	"bytes"
	"strings"
	"testing"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestModelFile_Render verifies that ModelFile emits a plain Go struct for a
// resource with primitive and nested fields.
func TestModelFile_Render(t *testing.T) {
	r := sampleModelResourceIR()

	file := ModelFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"// PetModel describes the API-facing shape for this resource.",
		"type PetModel struct",
		"Name   *string  `json:\"name,omitempty\"`",
		"Age    int64    `json:\"age\"`",
		"Weight *float64 `json:\"weight,omitempty\"`",
		"Happy  bool     `json:\"happy\"`",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated model file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestModelFile_Nested verifies that nested object attributes and collection
// elements produce uniquely named child structs.
func TestModelFile_Nested(t *testing.T) {
	r := ir.ResourceIR{
		Name: "pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "owner",
					Required: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "tags",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "value", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}},
						},
					},
				},
				{
					Name: "settings",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "enabled", Required: true, Schema: ir.SchemaIR{Type: ir.TypeBool}}}},
						},
					},
				},
			},
		},
	}

	file := ModelFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"type PetModel struct",
		"type PetModelOwner struct",
		"type PetModelTagsElem struct",
		"type PetModelSettingsMapElem struct",
		"Owner    PetModelOwner",
		"Tags     []PetModelTagsElem",
		"Settings map[string]PetModelSettingsMapElem",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated nested model file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestModelJSONTag verifies the JSON tag helper distinguishes required and
// optional attributes.
func TestModelJSONTag(t *testing.T) {
	cases := []struct {
		attr ir.AttributeIR
		want string
	}{
		{attr: ir.AttributeIR{Name: "id", Required: true}, want: "id"},
		{attr: ir.AttributeIR{Name: "id", Required: false}, want: "id,omitempty"},
		// L-49: a property name containing a comma would be parsed by
		// encoding/json as tag options (key "a", option "b"); it is sanitized so
		// the emitted tag stays structurally valid and deterministic.
		{attr: ir.AttributeIR{Name: "a,b", Required: true}, want: "a_b"},
		{attr: ir.AttributeIR{Name: `a"b`, Required: false}, want: `a_b,omitempty`},
	}
	for _, tc := range cases {
		if got := schema.ModelJSONTag(tc.attr); got != tc.want {
			t.Errorf("schema.ModelJSONTag(%+v) = %q, want %q", tc.attr, got, tc.want)
		}
	}
}

// TestGoFieldName_ValidIdentifiers is a regression test for H-6: goFieldName
// must always return a valid, exportable Go identifier even for hostile or
// empty spec property names. Previously goFieldName was bare pascalCase, so
// "2fa" produced the invalid identifier "2fa" (leading digit) and "---"/" "
// produced an empty name, and go/format printed them without error, so eidos
// exited 0 while emitting a provider that cannot compile.
func TestGoFieldName_ValidIdentifiers(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"name", "Name"},
		{"api_key", "ApiKey"},
		{"fooBar", "FooBar"},
		{"foo_bar", "FooBar"},
		{"2fa", "X2fa"},        // leading digit -> prefixed with X
		{"123", "X123"},        // all digits -> prefixed with X
		{"---", "X"},           // all separators -> empty -> X
		{"", "X"},              // empty -> X
		{" ", "X"},             // whitespace only -> X
		{"$var", "Var"},        // leading non-letter stripped, "var" capitalized
		{"data1", "Data1"},     // single alphanumeric word -> Data1
		{"URLPath", "URLPath"}, // acronym + word kept whole
	}
	for _, tc := range cases {
		got := naming.GoFieldName(tc.in)
		if got != tc.want {
			t.Errorf("naming.GoFieldName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// Every result must be a valid, exportable Go identifier.
		if got == "" {
			t.Errorf("naming.GoFieldName(%q) returned empty", tc.in)
			continue
		}
		r := []rune(got)
		if !unicode.IsUpper(r[0]) {
			t.Errorf("naming.GoFieldName(%q) = %q must start with an uppercase letter", tc.in, got)
		}
	}
}

// TestResolveFieldNames_Collisions is a regression test for H-7: two properties
// that differ only in separator style (foo_bar and fooBar) both normalize to
// FooBar and must be disambiguated so generated structs do not contain
// duplicate fields and value_mappers.go does not contain duplicate
// FooBarType/FromValue/ToValue functions.
func TestResolveFieldNames_Collisions(t *testing.T) {
	attrs := []ir.AttributeIR{
		{Name: "foo_bar", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "fooBar", Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}},
	}
	got := schema.ResolveFieldNames(attrs)

	// First occurrence keeps the base name; the collision gets a numeric suffix.
	if got["foo_bar"] != "FooBar" {
		t.Errorf("first collision occurrence = %q, want FooBar", got["foo_bar"])
	}
	if got["fooBar"] != "FooBar2" {
		t.Errorf("second collision occurrence = %q, want FooBar2", got["fooBar"])
	}
	if got["id"] != "Id" {
		t.Errorf("non-colliding name = %q, want Id", got["id"])
	}

	// Resolved names must be unique.
	seen := make(map[string]struct{})
	for _, name := range got {
		if _, dup := seen[name]; dup {
			t.Errorf("resolved name %q is not unique", name)
		}
		seen[name] = struct{}{}
	}
}

// TestModelFile_NameCollisionsCompiles is a regression test for H-7: a resource
// with attributes that collide after PascalCase normalization (foo_bar and
// fooBar) must emit a model_<name>.go with distinct, non-duplicate struct
// fields and nested types.
func TestModelFile_NameCollisionsCompiles(t *testing.T) {
	r := ir.ResourceIR{
		Name: "thing",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "foo_bar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "fooBar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{
					Name:     "owner_bar",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "x", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "ownerBar",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "y", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ModelFile(r)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Distinct top-level field names.
	for _, want := range []string{"FooBar ", "FooBar2 "} {
		if !strings.Contains(got, want) {
			t.Errorf("model file missing distinct field %q\ncontent:\n%s", want, got)
		}
	}
	// Distinct nested type names for the colliding object attributes.
	for _, want := range []string{"type ThingModelOwnerBar struct", "type ThingModelOwnerBar2 struct"} {
		if !strings.Contains(got, want) {
			t.Errorf("model file missing distinct nested type %q\ncontent:\n%s", want, got)
		}
	}
}

// sampleModelResourceIR returns a small ResourceIR used by model and mapper tests.
func sampleModelResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "name",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "age",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeInt},
				},
				{
					Name:     "weight",
					Optional: true,
					Schema:   ir.SchemaIR{Type: ir.TypeFloat},
				},
				{
					Name:     "happy",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeBool},
				},
			},
		},
	}
}
