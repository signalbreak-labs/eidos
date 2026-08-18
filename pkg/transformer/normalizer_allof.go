package transformer

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// FlattenAllOf merges every schema in allOf into a single flat object schema.
// It recursively flattens nested allOf declarations and combines required
// fields. If two subschemas define the same property with conflicting types,
// it returns an error.
//
// A nil or empty allOf list is normalized to an empty object schema. This
// differs from the OpenAPI/JSON Schema vacuously-true interpretation, but it
// matches the downstream IR expectation that allOf always resolves to an
// object schema in this transformer.
//
// Deprecated: unreachable from production. The live pipeline flattens allOf in
// schemaSpecFromParser (from_parser.go) instead; this normalizer and its
// helpers are retained only for their test coverage and must not be extended
// (M-7). See AUDIT.md.
func FlattenAllOf(schemas []*Schema) (*Schema, error) {
	if len(schemas) == 0 {
		return &Schema{Type: SchemaTypeObject}, nil
	}

	resolved := make([]*Schema, 0, len(schemas))
	for i, s := range schemas {
		if s == nil {
			return nil, fmt.Errorf("allOf entry %d is nil", i)
		}
		r, err := flattenSchema(s)
		if err != nil {
			return nil, fmt.Errorf("allOf entry %d: %w", i, err)
		}
		resolved = append(resolved, r)
	}

	return mergeObjectSchemas(resolved)
}

// flattenSchema normalizes a schema by collapsing any allOf composition. If the
// schema is not itself composed, it is returned unchanged.
func flattenSchema(s *Schema) (*Schema, error) {
	if len(s.AllOf) > 0 {
		return FlattenAllOf(s.AllOf)
	}
	return s, nil
}

// mergeObjectSchemas merges a list of object-like schemas into one flat schema.
// Every subschema must be object-like (explicit type "object" or carrying
// properties). Non-object subschemas are rejected.
func mergeObjectSchemas(schemas []*Schema) (*Schema, error) {
	result := &Schema{
		Type:              SchemaTypeObject,
		Properties:        make(map[string]*Schema),
		PatternProperties: make(map[string]*Schema),
	}
	required := make(map[string]struct{})

	for i, s := range schemas {
		if !allOfIsObjectLike(s) {
			return nil, fmt.Errorf("allOf entry %d is not an object schema (type=%q)", i, s.Type)
		}

		if err := mergeAllOfProperties(result.Properties, s.Properties, i); err != nil {
			return nil, err
		}
		if err := mergeAllOfPatternProperties(result.PatternProperties, s.PatternProperties, i); err != nil {
			return nil, err
		}

		for _, r := range s.Required {
			required[r] = struct{}{}
		}

		result.MinProperties = mergeIntPtrMax(result.MinProperties, s.MinProperties, "minProperties")
		result.MaxProperties = mergeIntPtrMin(result.MaxProperties, s.MaxProperties, "maxProperties")

		if s.Discriminator != nil {
			if result.Discriminator != nil {
				return nil, fmt.Errorf("allOf entry %d: conflicting discriminator declarations", i)
			}
			result.Discriminator = s.Discriminator
		}
	}

	// Fold scalar metadata (Description, Nullable, Format, Enum, Pattern, length
	// and numeric bounds) from each member into the result so it is not silently
	// dropped (M-43). Conflicting non-mergeable fields (Description, Format, Enum,
	// Pattern, Const) error, matching mergeSchemaMetadata; nullable is ORed and
	// numeric/length bounds take the stricter value. OneOf/AnyOf are preserved by
	// taking the first non-empty declaration.
	if err := foldObjectMetadata(result, schemas); err != nil {
		return nil, err
	}

	if len(required) > 0 {
		result.Required = make([]string, 0, len(required))
		for r := range required {
			result.Required = append(result.Required, r)
		}
		sort.Strings(result.Required)
	}

	if len(result.PatternProperties) == 0 {
		result.PatternProperties = nil
	}

	return result, nil
}

func mergeAllOfProperties(dst, src map[string]*Schema, entryIdx int) error {
	return mergeAllOfSchemaMap(dst, src, entryIdx, "property")
}

func mergeAllOfPatternProperties(dst, src map[string]*Schema, entryIdx int) error {
	return mergeAllOfSchemaMap(dst, src, entryIdx, "patternProperty")
}

func mergeAllOfSchemaMap(dst, src map[string]*Schema, entryIdx int, kind string) error {
	for name, prop := range src {
		prop, err := flattenSchema(prop)
		if err != nil {
			return fmt.Errorf("allOf entry %d, %s %q: %w", entryIdx, kind, name, err)
		}
		if existing, ok := dst[name]; ok {
			merged, err := mergePropertySchemas(existing, prop)
			if err != nil {
				return fmt.Errorf("%s %q: %w", kind, name, err)
			}
			dst[name] = merged
		} else {
			dst[name] = prop
		}
	}
	return nil
}

// mergePropertySchemas merges two schemas that describe the same property. If
// both are object-like, their properties are recursively merged. If both are
// arrays, their item schemas are merged. If both share the same primitive
// type, one is kept. Otherwise the types conflict and an error is returned.
func mergePropertySchemas(a, b *Schema) (*Schema, error) {
	a, err := flattenSchema(a)
	if err != nil {
		return nil, err
	}
	b, err = flattenSchema(b)
	if err != nil {
		return nil, err
	}

	if allOfIsObjectLike(a) && allOfIsObjectLike(b) {
		return mergeObjectSchemas([]*Schema{a, b})
	}

	if a.Type == SchemaTypeArray && b.Type == SchemaTypeArray {
		if a.Items == nil || b.Items == nil {
			return nil, errors.New("array property has missing item schema")
		}
		items, err := mergePropertySchemas(a.Items, b.Items)
		if err != nil {
			return nil, fmt.Errorf("array items: %w", err)
		}
		// Merge metadata (MinItems, MaxItems, Description, Nullable, Format,
		// Enum, etc.) instead of discarding it, then attach the merged item
		// schema (M-44).
		merged, err := mergeSchemaMetadata(a, b)
		if err != nil {
			return nil, err
		}
		merged.Items = items
		return merged, nil
	}

	if a.Type != b.Type {
		return nil, fmt.Errorf("conflicting types: %q and %q", a.Type, b.Type)
	}

	// Same non-object type: keep the type and merge non-conflicting metadata.
	// If both schemas supply different descriptions, formats, or enum values,
	// the merge is rejected so callers are not surprised by silent data loss.
	return mergeSchemaMetadata(a, b)
}

// mergeSchemaMetadata combines metadata and validation constraints from two
// schemas of the same type. Description, Format, and Enum keep a single value or
// error on conflict. Numeric and length constraints use the stricter of the two
// values (larger minimums, smaller maximums). Pattern and const values error
// on conflict. Nullable is ORed.
func mergeSchemaMetadata(a, b *Schema) (*Schema, error) {
	out := *a

	if a.Description == "" {
		out.Description = b.Description
	} else if b.Description != "" && a.Description != b.Description {
		return nil, fmt.Errorf("conflicting descriptions: %q and %q", a.Description, b.Description)
	}

	if a.Format == "" {
		out.Format = b.Format
	} else if b.Format != "" && a.Format != b.Format {
		return nil, fmt.Errorf("conflicting formats: %q and %q", a.Format, b.Format)
	}

	if len(a.Enum) == 0 {
		out.Enum = b.Enum
	} else if len(b.Enum) > 0 && !enumSlicesEqual(a.Enum, b.Enum) {
		return nil, fmt.Errorf("conflicting enum values")
	}

	// Stricter string/array/object cardinality constraints.
	out.MinLength = mergeIntPtrMax(a.MinLength, b.MinLength, "minLength")
	out.MaxLength = mergeIntPtrMin(a.MaxLength, b.MaxLength, "maxLength")
	out.MinItems = mergeIntPtrMax(a.MinItems, b.MinItems, "minItems")
	out.MaxItems = mergeIntPtrMin(a.MaxItems, b.MaxItems, "maxItems")
	out.MinProperties = mergeIntPtrMax(a.MinProperties, b.MinProperties, "minProperties")
	out.MaxProperties = mergeIntPtrMin(a.MaxProperties, b.MaxProperties, "maxProperties")

	// Stricter numeric constraints.
	out.Minimum = mergeFloatPtrMax(a.Minimum, b.Minimum, "minimum")
	out.Maximum = mergeFloatPtrMin(a.Maximum, b.Maximum, "maximum")
	out.ExclusiveMinimum = mergeFloatPtrMax(a.ExclusiveMinimum, b.ExclusiveMinimum, "exclusiveMinimum")
	out.ExclusiveMaximum = mergeFloatPtrMin(a.ExclusiveMaximum, b.ExclusiveMaximum, "exclusiveMaximum")
	out.MultipleOf = mergeFloatPtrMax(a.MultipleOf, b.MultipleOf, "multipleOf")

	// Pattern and const must agree when both are present.
	if a.Pattern != "" && b.Pattern != "" && a.Pattern != b.Pattern {
		return nil, fmt.Errorf("conflicting patterns: %q and %q", a.Pattern, b.Pattern)
	}
	if out.Pattern == "" {
		out.Pattern = b.Pattern
	}
	if a.Const != nil && b.Const != nil && !reflect.DeepEqual(a.Const, b.Const) {
		return nil, fmt.Errorf("conflicting const values")
	}
	if out.Const == nil {
		out.Const = b.Const
	}

	if a.Nullable || b.Nullable {
		out.Nullable = true
	}

	return &out, nil
}

// foldObjectMetadata merges the scalar metadata of object-like allOf members
// into result, so member Description/Nullable/Format/Enum/Pattern/length/numeric
// constraints are preserved rather than silently dropped (M-43). It uses
// mergeSchemaMetadata pairwise over scalar-only clones so the object-specific
// fields (Properties, PatternProperties, Items, Required, Discriminator) already
// merged onto result are not touched. OneOf/AnyOf are preserved by taking the
// first non-empty declaration (composing them is out of scope and would change
// semantics).
func foldObjectMetadata(result *Schema, schemas []*Schema) error {
	meta := &Schema{Type: SchemaTypeObject}
	for _, s := range schemas {
		member := &Schema{
			Type:             SchemaTypeObject,
			Description:      s.Description,
			Nullable:         s.Nullable,
			Format:           s.Format,
			Enum:             s.Enum,
			Pattern:          s.Pattern,
			MinLength:        s.MinLength,
			MaxLength:        s.MaxLength,
			Minimum:          s.Minimum,
			Maximum:          s.Maximum,
			ExclusiveMinimum: s.ExclusiveMinimum,
			ExclusiveMaximum: s.ExclusiveMaximum,
			MultipleOf:       s.MultipleOf,
			Const:            s.Const,
		}
		merged, err := mergeSchemaMetadata(meta, member)
		if err != nil {
			return err
		}
		meta = merged
		// Preserve the first non-empty OneOf/AnyOf declaration.
		if len(result.OneOf) == 0 && len(s.OneOf) > 0 {
			result.OneOf = s.OneOf
		}
		if len(result.AnyOf) == 0 && len(s.AnyOf) > 0 {
			result.AnyOf = s.AnyOf
		}
	}
	result.Description = meta.Description
	result.Nullable = meta.Nullable
	result.Format = meta.Format
	result.Enum = meta.Enum
	result.Pattern = meta.Pattern
	result.MinLength = meta.MinLength
	result.MaxLength = meta.MaxLength
	result.Minimum = meta.Minimum
	result.Maximum = meta.Maximum
	result.ExclusiveMinimum = meta.ExclusiveMinimum
	result.ExclusiveMaximum = meta.ExclusiveMaximum
	result.MultipleOf = meta.MultipleOf
	result.Const = meta.Const
	return nil
}

// mergeIntPtrMax returns the larger of a and b. If only one is non-nil, it is
// returned. If both are non-nil and differ, the larger (stricter minimum) is
// returned. name is used only for potential future diagnostics.
func mergeIntPtrMax(a, b *int, _ string) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b > *a {
		return b
	}
	return a
}

// mergeIntPtrMin returns the smaller of a and b (stricter maximum).
func mergeIntPtrMin(a, b *int, _ string) *int {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b < *a {
		return b
	}
	return a
}

// mergeFloatPtrMax returns the larger of a and b (stricter minimum bound).
func mergeFloatPtrMax(a, b *float64, _ string) *float64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b > *a {
		return b
	}
	return a
}

// mergeFloatPtrMin returns the smaller of a and b (stricter maximum bound).
func mergeFloatPtrMin(a, b *float64, _ string) *float64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *b < *a {
		return b
	}
	return a
}

// enumSlicesEqual reports whether two []interface{} slices contain the same
// values in the same order. A direct == comparison is not valid for
// []interface{}, so we compare element-wise using reflect.DeepEqual.
func enumSlicesEqual(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// allOfIsObjectLike reports whether a schema represents an object, either because
// it explicitly declares type "object" or because it carries properties.
func allOfIsObjectLike(s *Schema) bool {
	if s == nil {
		return false
	}
	return s.Type == SchemaTypeObject || (s.Type == "" && len(s.Properties) > 0)
}
