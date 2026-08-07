package transformer

import (
	_ "embed"
	"reflect"
	"strings"
	"testing"

	// test-only YAML dependency; remove once pkg/parser feeds the transformer pipeline.
	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestNormalizeOneOfSimple(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{Type: SchemaTypeString},
		{Type: SchemaTypeInteger},
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{Type: ir.TypeString},
				{Type: ir.TypeInt},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oneOf mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOneOfWithDiscriminator(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
			Required:   []string{"name"},
		},
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"age": {Type: SchemaTypeInteger}},
			Required:   []string{"age"},
		},
	}, &Discriminator{PropertyName: "kind"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{
					Attributes: []ir.AttributeIR{
						{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}, Required: true},
					},
				},
				{
					Attributes: []ir.AttributeIR{
						{Name: "age", Schema: ir.SchemaIR{Type: ir.TypeInt}, Required: true},
					},
				},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "kind"},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oneOf discriminator mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOneOfWithMapping(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{Type: SchemaTypeObject, Properties: map[string]*Schema{"meows": {Type: SchemaTypeBoolean}}},
		{Type: SchemaTypeObject, Properties: map[string]*Schema{"barks": {Type: SchemaTypeBoolean}}},
	}, &Discriminator{
		PropertyName: "petType",
		Mapping: map[string]string{
			"kitty": "Cat",
			"puppy": "Dog",
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{
					Attributes: []ir.AttributeIR{
						{Name: "meows", Schema: ir.SchemaIR{Type: ir.TypeBool}},
					},
				},
				{
					Attributes: []ir.AttributeIR{
						{Name: "barks", Schema: ir.SchemaIR{Type: ir.TypeBool}},
					},
				},
			},
			Discriminator: &ir.DiscriminatorIR{
				PropertyName: "petType",
				Mapping: map[string]string{
					"kitty": "Cat",
					"puppy": "Dog",
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oneOf mapping mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOneOfEmpty(t *testing.T) {
	_, err := NormalizeOneOf(nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error for empty oneOf, got nil")
	}
	if !strings.Contains(err.Error(), "oneOf must contain at least one variant schema") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNormalizeOneOfNilVariant(t *testing.T) {
	_, err := NormalizeOneOf([]*Schema{{Type: SchemaTypeString}, nil}, nil, nil)
	if err == nil {
		t.Fatalf("expected error for nil oneOf variant, got nil")
	}
	if !strings.Contains(err.Error(), "variant 1 is nil") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNormalizeAnyOf(t *testing.T) {
	got, err := NormalizeAnyOf([]*Schema{
		{Type: SchemaTypeString},
		{Type: SchemaTypeNumber},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{Type: ir.TypeString},
				{Type: ir.TypeFloat},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anyOf mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeAnyOfEmpty(t *testing.T) {
	_, err := NormalizeAnyOf(nil, nil)
	if err == nil {
		t.Fatalf("expected error for empty anyOf, got nil")
	}
	if !strings.Contains(err.Error(), "anyOf must contain at least one variant schema") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNormalizeOneOfNestedAllOf(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{
			AllOf: []*Schema{
				{
					Type:       SchemaTypeObject,
					Properties: map[string]*Schema{"id": {Type: SchemaTypeString}},
					Required:   []string{"id"},
				},
				{
					Type:       SchemaTypeObject,
					Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
				},
			},
		},
		{Type: SchemaTypeInteger},
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{
					Attributes: []ir.AttributeIR{
						{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}, Required: true},
						{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
				{Type: ir.TypeInt},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oneOf nested allOf mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOneOfArrayVariant(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{
			Type:  SchemaTypeArray,
			Items: &Schema{Type: SchemaTypeString},
		},
		{Type: SchemaTypeBoolean},
	}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Union: &ir.UnionType{
			Variants: []ir.SchemaIR{
				{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
				{Type: ir.TypeBool},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("oneOf array variant mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestSchemaToIRObject(t *testing.T) {
	got, err := schemaToIR(&Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"id":   {Type: SchemaTypeString},
			"tags": {Type: SchemaTypeArray, Items: &Schema{Type: SchemaTypeString}},
		},
		Required: []string{"id"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}, Required: true},
			{
				Name: "tags",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("object schema mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestSchemaToIRUnsupportedType(t *testing.T) {
	_, err := schemaToIR(&Schema{Type: "unsupported"}, nil)
	if err == nil {
		t.Fatalf("expected error for unsupported type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported schema type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNormalizeOneOfProducesValidIR(t *testing.T) {
	got, err := NormalizeOneOf([]*Schema{
		{Type: SchemaTypeObject, Properties: map[string]*Schema{"a": {Type: SchemaTypeString}}},
		{Type: SchemaTypeInteger},
	}, &Discriminator{PropertyName: "kind"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("normalized oneOf IR is invalid: %v", err)
	}
}

func TestNormalizeAnyOfProducesValidIR(t *testing.T) {
	got, err := NormalizeAnyOf([]*Schema{
		{Type: SchemaTypeString},
		{Type: SchemaTypeInteger},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("normalized anyOf IR is invalid: %v", err)
	}
}

func TestSchemaToIR_PreservesEnumAndAllOf(t *testing.T) {
	s := &Schema{
		AllOf: []*Schema{
			{
				Type: SchemaTypeObject,
				Properties: map[string]*Schema{
					"status": {Type: SchemaTypeString, Enum: []interface{}{"active", "inactive"}},
				},
			},
		},
	}

	got, err := schemaToIR(s, nil)
	if err != nil {
		t.Fatalf("schemaToIR error: %v", err)
	}

	if len(got.Attributes) != 1 {
		t.Fatalf("Attributes length = %d, want 1", len(got.Attributes))
	}
	status := got.Attributes[0]
	if status.Name != "status" {
		t.Fatalf("attribute name = %q, want \"status\"", status.Name)
	}
	wantEnum := []any{"active", "inactive"}
	if !reflect.DeepEqual(status.Schema.EnumValues, wantEnum) {
		t.Errorf("status.EnumValues = %v, want %v", status.Schema.EnumValues, wantEnum)
	}
}

//go:embed testdata/pet_oneof.yaml
var petOneOfYAML []byte

//go:embed testdata/metric_anyof.yaml
var metricAnyOfYAML []byte

// TestNormalizeOneOfFromYAML exercises the path from a serialized OpenAPI
// schema fragment through the normalizer. This is the closest integration-level
// coverage the current package can provide because a full parser does not yet
// exist.
func TestNormalizeOneOfFromYAML(t *testing.T) {
	var schema Schema
	if err := yaml.Unmarshal(petOneOfYAML, &schema); err != nil {
		t.Fatalf("unmarshal pet oneOf fixture: %v", err)
	}
	if len(schema.OneOf) == 0 {
		t.Fatalf("fixture did not produce oneOf variants")
	}

	got, err := NormalizeOneOf(schema.OneOf, schema.Discriminator, nil)
	if err != nil {
		t.Fatalf("NormalizeOneOf error: %v", err)
	}

	if got.Union == nil || len(got.Union.Variants) != 2 {
		t.Fatalf("expected two oneOf variants, got %#v", got)
	}
	if got.Union.Discriminator == nil || got.Union.Discriminator.PropertyName != "petType" {
		t.Fatalf("expected discriminator petType, got %#v", got.Union.Discriminator)
	}
	wantMapping := map[string]string{"cat": "Cat", "dog": "Dog"}
	if !reflect.DeepEqual(got.Union.Discriminator.Mapping, wantMapping) {
		t.Fatalf("discriminator mapping mismatch:\ngot:  %#v\nwant: %#v", got.Union.Discriminator.Mapping, wantMapping)
	}
}

// TestNormalizeAnyOfFromYAML exercises the path from a serialized OpenAPI
// schema fragment through the normalizer.
func TestNormalizeAnyOfFromYAML(t *testing.T) {
	var schema Schema
	if err := yaml.Unmarshal(metricAnyOfYAML, &schema); err != nil {
		t.Fatalf("unmarshal metric anyOf fixture: %v", err)
	}
	if len(schema.AnyOf) == 0 {
		t.Fatalf("fixture did not produce anyOf variants")
	}

	got, err := NormalizeAnyOf(schema.AnyOf, nil)
	if err != nil {
		t.Fatalf("NormalizeAnyOf error: %v", err)
	}

	if got.Union == nil || len(got.Union.Variants) != 2 {
		t.Fatalf("expected two anyOf variants, got %#v", got)
	}
}

// TestPropertiesToAttributesDedupCollisions locks in the L-100 fix: properties
// whose names normalize to the same snake_case attribute (e.g. "fooBar" and
// "foo_bar") are deduplicated with a warning rather than emitted as duplicate
// attributes.
func TestPropertiesToAttributesDedupCollisions(t *testing.T) {
	props := map[string]*Schema{
		"fooBar":  {Type: SchemaTypeString},
		"foo_bar": {Type: SchemaTypeInteger},
	}
	var diags diagnostics.Diagnostics
	attrs, err := propertiesToAttributes(props, nil, &diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(attrs) != 1 {
		t.Fatalf("expected 1 deduplicated attribute, got %d: %+v", len(attrs), attrs)
	}
	// Both names normalize to "foo_bar"; the first in sorted original-name
	// order wins. "fooBar" sorts before "foo_bar" (uppercase 'B' < '_'), so the
	// string-typed property survives.
	if attrs[0].Name != "foo_bar" {
		t.Errorf("attribute name = %q, want %q", attrs[0].Name, "foo_bar")
	}
	if attrs[0].Schema.Type != ir.TypeString {
		t.Errorf("surviving schema type = %v, want %v (fooBar should win)", attrs[0].Schema.Type, ir.TypeString)
	}
	var warned bool
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Detail, "fooBar") && strings.Contains(d.Detail, "foo_bar") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a collision warning mentioning both property names, got %v", diags)
	}
}
