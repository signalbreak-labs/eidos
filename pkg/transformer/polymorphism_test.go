package transformer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestSelectStrategyHeuristics(t *testing.T) {
	tests := []struct {
		name    string
		ctx     PolymorphismContext
		schema  *ir.SchemaIR
		want    PolymorphismStrategy
		wantErr bool
	}{
		{
			name: "top-level oneOf of named object variants chooses split_resources",
			ctx:  ContextTopLevelResource,
			schema: &ir.SchemaIR{
				Name: "Pet",
				Union: &ir.UnionType{
					Kind: ir.OneOf,
					Variants: []ir.SchemaIR{
						{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
						{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
					},
				},
			},
			want: StrategySplitResources,
		},
		{
			name: "nested oneOf with discriminator chooses dynamic_union",
			ctx:  ContextNestedAttribute,
			schema: &ir.SchemaIR{
				Name: "Pet",
				Union: &ir.UnionType{
					Kind: ir.OneOf,
					Variants: []ir.SchemaIR{
						{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
						{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
					},
					Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
				},
			},
			want: StrategyDynamicUnion,
		},
		{
			name: "anyOf top-level chooses dynamic_union",
			ctx:  ContextTopLevelResource,
			schema: &ir.SchemaIR{
				Name: "Pet",
				Union: &ir.UnionType{
					Kind: ir.AnyOf,
					Variants: []ir.SchemaIR{
						{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
						{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
					},
				},
			},
			want: StrategyDynamicUnion,
		},
		{
			name: "non-object variants choose dynamic_union",
			ctx:  ContextTopLevelResource,
			schema: &ir.SchemaIR{
				Name: "Value",
				Union: &ir.UnionType{
					Kind:     ir.OneOf,
					Variants: []ir.SchemaIR{{Name: "StringValue", Type: ir.TypeString}},
				},
			},
			want: StrategyDynamicUnion,
		},
		{
			name:    "non-union schema returns error",
			ctx:     ContextTopLevelResource,
			schema:  &ir.SchemaIR{Name: "Pet", Type: ir.TypeString},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectStrategy(tt.schema, tt.ctx, PolymorphismConfig{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("SelectStrategy() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SelectStrategy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectStrategyConfigOverrides(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	t.Run("global override", func(t *testing.T) {
		cfg := PolymorphismConfig{Strategy: "split_resources"}
		got, err := SelectStrategy(schema, ContextNestedAttribute, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != StrategySplitResources {
			t.Errorf("SelectStrategy() = %q, want %q", got, StrategySplitResources)
		}
	})

	t.Run("per-schema override beats global default", func(t *testing.T) {
		cfg := PolymorphismConfig{
			Strategy: "split_resources",
			OneOf: []PolymorphismOneOfConfig{{
				Schema:   "Pet",
				Strategy: "dynamic_union",
			}},
		}
		got, err := SelectStrategy(schema, ContextTopLevelResource, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != StrategyDynamicUnion {
			t.Errorf("SelectStrategy() = %q, want %q", got, StrategyDynamicUnion)
		}
	})
}

func TestApplyDynamicUnionNoDiscriminator(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	got, err := ApplyDynamicUnion(schema)
	if err != nil {
		t.Fatalf("ApplyDynamicUnion() unexpected error: %v", err)
	}

	if got.Type != ir.TypeDynamic {
		t.Errorf("expected TypeDynamic, got %q", got.Type)
	}
	if got.Name != "Pet" {
		t.Errorf("expected name Pet, got %q", got.Name)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for dynamic union, got %v", got.Attributes)
	}
}

func TestApplyDynamicUnionWithDiscriminator(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{
					Name: "Cat",
					Attributes: []ir.AttributeIR{
						{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "livesRemaining", Schema: ir.SchemaIR{Type: ir.TypeInt}},
					},
				},
				{
					Name: "Dog",
					Attributes: []ir.AttributeIR{
						{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
			Discriminator: &ir.DiscriminatorIR{
				PropertyName: "petType",
				Mapping: map[string]string{
					"cat": "Cat",
					"dog": "Dog",
				},
			},
		},
	}

	got, err := ApplyDynamicUnion(schema)
	if err != nil {
		t.Fatalf("ApplyDynamicUnion() unexpected error: %v", err)
	}

	if got.Type != "" {
		t.Errorf("expected nested object schema with empty Type, got %q", got.Type)
	}
	if len(got.Attributes) != 4 {
		t.Fatalf("expected 4 attributes (discriminator + 3 fields), got %d: %v", len(got.Attributes), got.Attributes)
	}

	if got.Attributes[0].Name != "pet_type" {
		t.Errorf("expected first attribute to be discriminator pet_type, got %q", got.Attributes[0].Name)
	}
	if !got.Attributes[0].Required {
		t.Errorf("discriminator attribute should be required")
	}

	expectedNames := map[string]bool{
		"pet_type":        true,
		"name":            true,
		"lives_remaining": true,
		"breed":           true,
	}
	for _, attr := range got.Attributes {
		if !expectedNames[attr.Name] {
			t.Errorf("unexpected attribute %q", attr.Name)
		}
		if attr.Name != "pet_type" && attr.Required {
			t.Errorf("variant field %q should be optional in merged schema", attr.Name)
		}
	}

	if len(got.Validators) != 1 || got.Validators[0].Type != "validators.DiscriminatorValidator" {
		t.Errorf("expected DiscriminatorValidator, got %v", got.Validators)
	}
}

// TestApplyDynamicUnionDuplicateAttributeDeduped asserts identical duplicates
// (same name, same type — the common discriminator base field) are deduped
// first-wins instead of erroring, so the dynamic-union strategy works for
// real oneOf hierarchies where every variant carries the base fields.
func TestApplyDynamicUnionDuplicateAttributeDeduped(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
		},
	}

	merged, err := ApplyDynamicUnion(schema)
	if err != nil {
		t.Fatalf("ApplyDynamicUnion() error = %v, want nil (identical duplicates dedupe)", err)
	}
	var nameCount int
	for _, a := range merged.Attributes {
		if a.Name == "name" {
			nameCount++
		}
	}
	if nameCount != 1 {
		t.Errorf("merged attribute %q count = %d, want 1 (deduped)", "name", nameCount)
	}
}

// TestApplyDynamicUnionConflictingAttributeError asserts a same-named
// attribute with a conflicting type across variants is still an error: the
// merged object cannot represent both.
func TestApplyDynamicUnionConflictingAttributeError(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "tag", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "tag", Schema: ir.SchemaIR{Type: ir.TypeInt}}}},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
		},
	}

	if _, err := ApplyDynamicUnion(schema); err == nil {
		t.Fatalf("expected conflicting attribute error")
	}
}

func TestSplitResources(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{
					Name: "Cat",
					Attributes: []ir.AttributeIR{
						{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
				{
					Name: "Dog",
					Attributes: []ir.AttributeIR{
						{Name: "breed", Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
		},
	}

	resources, err := SplitResources("Pet", schema, PolymorphismConfig{})
	if err != nil {
		t.Fatalf("SplitResources() unexpected error: %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	if resources[0].Name != "Cat" || resources[0].TypeName != "cat" {
		t.Errorf("first resource = %q/%q, want Cat/cat", resources[0].Name, resources[0].TypeName)
	}
	if resources[1].Name != "Dog" || resources[1].TypeName != "dog" {
		t.Errorf("second resource = %q/%q, want Dog/dog", resources[1].Name, resources[1].TypeName)
	}

	for _, r := range resources {
		for _, a := range r.Schema.Attributes {
			if a.Name == "petType" {
				t.Errorf("resource %q should omit discriminator attribute", r.Name)
			}
		}
	}
}

func TestSplitResourcesVariantNameOverride(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	cfg := PolymorphismConfig{
		OneOf: []PolymorphismOneOfConfig{{
			Schema: "Pet",
			Variants: []PolymorphismVariantConfig{{
				Schema:       "Cat",
				ResourceName: "feline",
			}},
		}},
	}

	resources, err := SplitResources("Pet", schema, cfg)
	if err != nil {
		t.Fatalf("SplitResources() unexpected error: %v", err)
	}

	if len(resources) != 1 || resources[0].TypeName != "feline" {
		t.Errorf("expected resource type name feline, got %v", resources)
	}
}

func TestSplitResourcesUnnamedVariantError(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind:     ir.OneOf,
			Variants: []ir.SchemaIR{{Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}}},
		},
	}

	_, err := SplitResources("Pet", schema, PolymorphismConfig{})
	if err == nil {
		t.Fatalf("expected error for unnamed variant")
	}
}

func TestIsObjectLike(t *testing.T) {
	tests := []struct {
		name string
		v    ir.SchemaIR
		want bool
	}{
		{"object with attributes", ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "id"}}}, true},
		{"object with blocks", ir.SchemaIR{Blocks: []ir.BlockIR{{Name: "config"}}}, true},
		{"empty object node", ir.SchemaIR{}, true},
		{"string primitive", ir.SchemaIR{Type: ir.TypeString}, false},
		{"array collection", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List}}, false},
		{"nested union", ir.SchemaIR{Union: &ir.UnionType{}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isObjectLike(tt.v); got != tt.want {
				t.Errorf("isObjectLike(%+v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestStringKeysToAny(t *testing.T) {
	got := stringKeysToAny(map[string]string{"b": "2", "a": "1"})
	want := []any{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stringKeysToAny() = %v, want %v", got, want)
	}
}

// TestApplyDynamicUnionEmptyDiscriminatorPropertyName locks in the L-103 fix: a
// discriminator with an empty property name is rejected with an error rather
// than producing an attribute named "" and validator args containing "".
func TestApplyDynamicUnionEmptyDiscriminatorPropertyName(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
			Discriminator: &ir.DiscriminatorIR{PropertyName: ""},
		},
	}

	_, err := ApplyDynamicUnion(schema)
	if err == nil {
		t.Fatalf("expected error for empty discriminator property name, got nil")
	}
	if !strings.Contains(err.Error(), "empty property name") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestSelectStrategyInvalidPerSchemaOverride locks in the L-104 fix: an invalid
// per-schema strategy is rejected rather than silently accepted.
func TestSelectStrategyInvalidPerSchemaOverride(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}
	cfg := PolymorphismConfig{
		OneOf: []PolymorphismOneOfConfig{{
			Schema:   "Pet",
			Strategy: "bogus",
		}},
	}

	_, err := SelectStrategy(schema, ContextTopLevelResource, cfg)
	if err == nil {
		t.Fatalf("expected error for invalid per-schema strategy, got nil")
	}
	if !strings.Contains(err.Error(), "invalid polymorphism strategy") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestSplitResourcesVariantNameOverrideCaseInsensitive locks in the L-105 fix:
// variant name overrides match case-insensitively, consistent with the rest of
// the transformer.
func TestSplitResourcesVariantNameOverrideCaseInsensitive(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	cfg := PolymorphismConfig{
		OneOf: []PolymorphismOneOfConfig{{
			Schema: "Pet",
			Variants: []PolymorphismVariantConfig{{
				Schema:       "cat", // lowercase, variant is "Cat"
				ResourceName: "feline",
			}},
		}},
	}

	resources, err := SplitResources("Pet", schema, cfg)
	if err != nil {
		t.Fatalf("SplitResources() unexpected error: %v", err)
	}

	if len(resources) != 1 || resources[0].TypeName != "feline" {
		t.Errorf("expected case-insensitive match to apply override (type name feline), got %v", resources)
	}
}

// TestSplitResourcesVariantOverrideAcrossOneOfEntries locks in the L-105 fix:
// SplitResources consults all matching OneOf config entries for a variant
// override, not just the first.
func TestSplitResourcesVariantOverrideAcrossOneOfEntries(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	cfg := PolymorphismConfig{
		OneOf: []PolymorphismOneOfConfig{
			{Schema: "Pet"}, // first matching entry has no variant override
			{
				Schema: "Pet",
				Variants: []PolymorphismVariantConfig{{
					Schema:       "Cat",
					ResourceName: "feline",
				}},
			},
		},
	}

	resources, err := SplitResources("Pet", schema, cfg)
	if err != nil {
		t.Fatalf("SplitResources() unexpected error: %v", err)
	}

	if len(resources) != 1 || resources[0].TypeName != "feline" {
		t.Errorf("expected override from second OneOf entry to apply (type name feline), got %v", resources)
	}
}
