package transformer

import (
	"reflect"
	"strings"
	"testing"
)

func TestFlattenAllOfSimpleMerge(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type:       SchemaTypeObject,
			Required:   []string{"id"},
			Properties: map[string]*Schema{"id": {Type: SchemaTypeString}},
		},
		{
			Type:       SchemaTypeObject,
			Required:   []string{"status"},
			Properties: map[string]*Schema{"status": {Type: SchemaTypeString}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"id":     {Type: SchemaTypeString},
			"status": {Type: SchemaTypeString},
		},
		Required: []string{"id", "status"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flatten mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFlattenAllOfEmpty(t *testing.T) {
	got, err := FlattenAllOf(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &Schema{Type: SchemaTypeObject}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty flatten mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFlattenAllOfNested(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type: SchemaTypeObject,
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
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"status": {Type: SchemaTypeString}},
			Required:   []string{"status"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"id":     {Type: SchemaTypeString},
			"name":   {Type: SchemaTypeString},
			"status": {Type: SchemaTypeString},
		},
		Required: []string{"id", "status"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nested flatten mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFlattenAllOfDuplicateSameType(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type:       SchemaTypeObject,
			Required:   []string{"name"},
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
		},
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"name": {Type: SchemaTypeString},
		},
		Required: []string{"name"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate same-type mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFlattenAllOfDuplicateObjectMerge(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"owner": {
					Type:       SchemaTypeObject,
					Properties: map[string]*Schema{"id": {Type: SchemaTypeString}},
					Required:   []string{"id"},
				},
			},
		},
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"owner": {
					Type:       SchemaTypeObject,
					Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"owner": {
				Type: SchemaTypeObject,
				Properties: map[string]*Schema{
					"id":   {Type: SchemaTypeString},
					"name": {Type: SchemaTypeString},
				},
				Required: []string{"id"},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate object merge mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFlattenAllOfConflictingTypes(t *testing.T) {
	_, err := FlattenAllOf([]*Schema{
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"value": {Type: SchemaTypeString}},
		},
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"value": {Type: SchemaTypeInteger}},
		},
	})
	if err == nil {
		t.Fatalf("expected error for conflicting types, got nil")
	}
	if !strings.Contains(err.Error(), "conflicting types") {
		t.Fatalf("error does not mention conflicting types: %v", err)
	}
}

func TestFlattenAllOfNonObjectMember(t *testing.T) {
	_, err := FlattenAllOf([]*Schema{
		{Type: SchemaTypeString},
	})
	if err == nil {
		t.Fatalf("expected error for non-object allOf member, got nil")
	}
	if !strings.Contains(err.Error(), "not an object schema") {
		t.Fatalf("error does not mention non-object schema: %v", err)
	}
}

func TestFlattenAllOfMetadataMerge(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString, Description: "first", Format: "uuid", Enum: []interface{}{"a", "b"}}},
		},
		{
			Type:       SchemaTypeObject,
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString, Format: "", Enum: []interface{}{"a", "b"}}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := &Schema{
		Type: SchemaTypeObject,
		Properties: map[string]*Schema{
			"name": {Type: SchemaTypeString, Description: "first", Format: "uuid", Enum: []interface{}{"a", "b"}},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata merge mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestEnumSlicesEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []interface{}
		want bool
	}{
		{name: "equal strings", a: []interface{}{"a", "b"}, b: []interface{}{"a", "b"}, want: true},
		{name: "different lengths", a: []interface{}{"a"}, b: []interface{}{"a", "b"}, want: false},
		{name: "different elements", a: []interface{}{"a", "b"}, b: []interface{}{"a", "c"}, want: false},
		{name: "both empty", a: []interface{}{}, b: []interface{}{}, want: true},
		{name: "nil and empty", a: nil, b: []interface{}{}, want: true},
		{name: "different types", a: []interface{}{"1"}, b: []interface{}{1}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := enumSlicesEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("enumSlicesEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestFlattenAllOfMetadataConflict(t *testing.T) {
	cases := []struct {
		name string
		a, b *Schema
		want string
	}{
		{
			name: "description",
			a:    &Schema{Type: SchemaTypeString, Description: "first"},
			b:    &Schema{Type: SchemaTypeString, Description: "second"},
			want: "conflicting descriptions",
		},
		{
			name: "format",
			a:    &Schema{Type: SchemaTypeString, Format: "uuid"},
			b:    &Schema{Type: SchemaTypeString, Format: "email"},
			want: "conflicting formats",
		},
		{
			name: "enum",
			a:    &Schema{Type: SchemaTypeString, Enum: []interface{}{"a"}},
			b:    &Schema{Type: SchemaTypeString, Enum: []interface{}{"b"}},
			want: "conflicting enum values",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FlattenAllOf([]*Schema{
				{Type: SchemaTypeObject, Properties: map[string]*Schema{"value": tc.a}},
				{Type: SchemaTypeObject, Properties: map[string]*Schema{"value": tc.b}},
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not mention %q: %v", tc.want, err)
			}
		})
	}
}

func TestFlattenAllOfConstraintMerge(t *testing.T) {
	min5 := 5
	min10 := 10
	max20 := 20
	max30 := 30
	minVal := 1.0
	maxVal := 100.0

	result, err := FlattenAllOf([]*Schema{
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"name": {
					Type:      SchemaTypeString,
					MinLength: &min5,
					MaxLength: &max30,
					Pattern:   "^[a-z]+$",
				},
			},
		},
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"name": {
					Type:      SchemaTypeString,
					MinLength: &min10,
					MaxLength: &max20,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	name := result.Properties["name"]
	if name.MinLength == nil || *name.MinLength != 10 {
		t.Errorf("expected minLength 10 (stricter), got %v", name.MinLength)
	}
	if name.MaxLength == nil || *name.MaxLength != 20 {
		t.Errorf("expected maxLength 20 (stricter), got %v", name.MaxLength)
	}
	if name.Pattern != "^[a-z]+$" {
		t.Errorf("expected pattern preserved, got %q", name.Pattern)
	}

	result, err = FlattenAllOf([]*Schema{
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"count": {
					Type:    SchemaTypeInteger,
					Minimum: &minVal,
					Maximum: &maxVal,
				},
			},
		},
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"count": {
					Type:    SchemaTypeInteger,
					Minimum: &minVal,
					Maximum: &maxVal,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := result.Properties["count"]
	if count.Minimum == nil || *count.Minimum != 1.0 {
		t.Errorf("expected minimum 1.0, got %v", count.Minimum)
	}
	if count.Maximum == nil || *count.Maximum != 100.0 {
		t.Errorf("expected maximum 100.0, got %v", count.Maximum)
	}
}

func TestFlattenAllOfObjectKeywordMerge(t *testing.T) {
	minProps := 1
	maxProps := 5

	result, err := FlattenAllOf([]*Schema{
		{
			Type:          SchemaTypeObject,
			MinProperties: &minProps,
			Properties: map[string]*Schema{
				"a": {Type: SchemaTypeString},
			},
		},
		{
			Type:          SchemaTypeObject,
			MaxProperties: &maxProps,
			Discriminator: &Discriminator{PropertyName: "kind"},
			Properties: map[string]*Schema{
				"b": {Type: SchemaTypeString},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MinProperties == nil || *result.MinProperties != 1 {
		t.Errorf("expected minProperties 1, got %v", result.MinProperties)
	}
	if result.MaxProperties == nil || *result.MaxProperties != 5 {
		t.Errorf("expected maxProperties 5, got %v", result.MaxProperties)
	}
	if result.Discriminator == nil || result.Discriminator.PropertyName != "kind" {
		t.Errorf("expected discriminator kind, got %v", result.Discriminator)
	}
}

func TestFlattenAllOfPatternConflict(t *testing.T) {
	_, err := FlattenAllOf([]*Schema{
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"value": {Type: SchemaTypeString, Pattern: "^[a-z]+$"},
			},
		},
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"value": {Type: SchemaTypeString, Pattern: "^[A-Z]+$"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for conflicting patterns")
	}
	if !strings.Contains(err.Error(), "conflicting patterns") {
		t.Fatalf("expected pattern conflict error, got %v", err)
	}
}

// TestFlattenAllOfPreservesMemberMetadata locks in the M-43 fix: top-level
// member metadata (Description, Nullable, Format, Enum) is folded onto the
// merged object rather than silently dropped. Nullable is ORed; a single
// description/format is preserved.
func TestFlattenAllOfPreservesMemberMetadata(t *testing.T) {
	got, err := FlattenAllOf([]*Schema{
		{
			Type:        SchemaTypeObject,
			Description: "A pet",
			Nullable:    true,
			Properties:  map[string]*Schema{"id": {Type: SchemaTypeString}},
		},
		{
			Type:       SchemaTypeObject,
			Enum:       []interface{}{"cat", "dog"},
			Properties: map[string]*Schema{"name": {Type: SchemaTypeString}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Description != "A pet" {
		t.Errorf("expected description preserved, got %q", got.Description)
	}
	if !got.Nullable {
		t.Errorf("expected nullable folded to true")
	}
	if !reflect.DeepEqual(got.Enum, []interface{}{"cat", "dog"}) {
		t.Errorf("expected enum preserved, got %v", got.Enum)
	}
}

// TestFlattenAllOfArrayMergePreservesConstraints locks in the M-44 fix: merging
// two array definitions preserves MinItems/MaxItems/Description instead of
// discarding them and returning a bare {type: array, items: ...}.
func TestFlattenAllOfArrayMergePreservesConstraints(t *testing.T) {
	minA, maxA := 1, 100
	minB, maxB := 2, 50
	got, err := FlattenAllOf([]*Schema{
		{
			Type:        SchemaTypeObject,
			Description: "wrapper",
			Properties: map[string]*Schema{
				"tags": {Type: SchemaTypeArray, Items: &Schema{Type: SchemaTypeString}, MinItems: &minA, MaxItems: &maxA, Description: "tag list"},
			},
		},
		{
			Type: SchemaTypeObject,
			Properties: map[string]*Schema{
				"tags": {Type: SchemaTypeArray, Items: &Schema{Type: SchemaTypeString}, MinItems: &minB, MaxItems: &maxB},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tags := got.Properties["tags"]
	if tags == nil {
		t.Fatal("missing tags property")
	}
	if tags.Description != "tag list" {
		t.Errorf("expected array description preserved, got %q", tags.Description)
	}
	if tags.MinItems == nil || *tags.MinItems != 2 {
		t.Errorf("expected stricter minItems 2, got %v", tags.MinItems)
	}
	if tags.MaxItems == nil || *tags.MaxItems != 50 {
		t.Errorf("expected stricter maxItems 50, got %v", tags.MaxItems)
	}
}
