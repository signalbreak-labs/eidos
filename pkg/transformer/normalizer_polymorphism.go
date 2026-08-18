package transformer

import (
	"errors"
	"fmt"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// NormalizeOneOf converts a list of oneOf variant schemas and an optional
// discriminator into a SchemaIR whose Union field holds the variants. When a
// discriminator is supplied, it is attached to the Union as a DiscriminatorIR.
// Non-fatal diagnostics encountered during conversion are appended to diags;
// nil is accepted and ignored.
//
// Deprecated: unreachable from production. The live pipeline converts
// polymorphism in schemaSpecFromParser (from_parser.go) instead; this function
// and its helpers are retained only for their test coverage and must not be
// extended (M-7). See AUDIT.md.
func NormalizeOneOf(schemas []*Schema, discriminator *Discriminator, diags *diagnostics.Diagnostics) (*ir.SchemaIR, error) {
	if len(schemas) == 0 {
		return nil, errors.New("oneOf must contain at least one variant schema")
	}

	variants, err := normalizeVariantSchemas(schemas, diags)
	if err != nil {
		return nil, fmt.Errorf("oneOf: %w", err)
	}

	union := &ir.UnionType{Variants: variants}
	if discriminator != nil {
		union.Discriminator = &ir.DiscriminatorIR{
			PropertyName: discriminator.PropertyName,
			Mapping:      copyMapping(discriminator.Mapping),
		}
	}

	return &ir.SchemaIR{Union: union}, nil
}

// NormalizeAnyOf converts a list of anyOf variant schemas into a SchemaIR whose
// Union field holds the variants. anyOf never carries a discriminator.
// Non-fatal diagnostics encountered during conversion are appended to diags;
// nil is accepted and ignored.
//
// Deprecated: unreachable from production. The live pipeline converts
// polymorphism in schemaSpecFromParser (from_parser.go) instead; this function
// and its helpers are retained only for their test coverage and must not be
// extended (M-7). See AUDIT.md.
func NormalizeAnyOf(schemas []*Schema, diags *diagnostics.Diagnostics) (*ir.SchemaIR, error) {
	if len(schemas) == 0 {
		return nil, errors.New("anyOf must contain at least one variant schema")
	}

	variants, err := normalizeVariantSchemas(schemas, diags)
	if err != nil {
		return nil, fmt.Errorf("anyOf: %w", err)
	}

	return &ir.SchemaIR{Union: &ir.UnionType{Variants: variants}}, nil
}

// normalizeVariantSchemas converts each version-agnostic Schema into a
// SchemaIR. Nil entries are rejected; nested allOf composition is flattened by
// schemaToIR during conversion.
func normalizeVariantSchemas(schemas []*Schema, diags *diagnostics.Diagnostics) ([]ir.SchemaIR, error) {
	variants := make([]ir.SchemaIR, 0, len(schemas))
	for i, s := range schemas {
		if s == nil {
			return nil, fmt.Errorf("variant %d is nil", i)
		}

		variant, err := schemaToIR(s, diags)
		if err != nil {
			return nil, fmt.Errorf("variant %d: %w", i, err)
		}
		variants = append(variants, *variant)
	}
	return variants, nil
}

// schemaToIR converts a version-agnostic Schema into the normalized IR.
// It first flattens any allOf composition, then maps primitive and container
// types and flattens object properties into framework-style attributes.
// Array schemas always become CollectionType with kind ir.List because the
// transformer SchemaType enum currently only distinguishes arrays.
//
// Declarative metadata such as description, format, and enum values are
// preserved on the IR. This is a behavior change relative to earlier
// normalizer passes that silently dropped them; downstream consumers may now
// observe these fields populated.
// Non-fatal diagnostics encountered during keyword mapping are appended to
// diags; nil is accepted and ignored.
func schemaToIR(s *Schema, diags *diagnostics.Diagnostics) (*ir.SchemaIR, error) {
	if s == nil {
		return &ir.SchemaIR{}, nil
	}

	// allOf is flattened first so the rest of the conversion only has to deal
	// with a single, merged object schema.
	if len(s.AllOf) > 0 {
		flat, err := FlattenAllOf(s.AllOf)
		if err != nil {
			return nil, err
		}
		s = flat
	}

	result := &ir.SchemaIR{
		Description: s.Description,
		Format:      s.Format,
		EnumValues:  convertEnum(s.Enum),
	}

	switch s.Type {
	case SchemaTypeString:
		result.Type = ir.TypeString
	case SchemaTypeInteger:
		result.Type = ir.TypeInt
	case SchemaTypeNumber:
		result.Type = ir.TypeFloat
	case SchemaTypeBoolean:
		result.Type = ir.TypeBool
	case SchemaTypeNull:
		result.Type = ir.TypeNull
	case SchemaTypeArray:
		// The transformer schema model only has SchemaTypeArray, so arrays always map
		// to the IR list collection kind.
		if s.Items == nil {
			return nil, errors.New("array schema is missing items")
		}
		elem, err := schemaToIR(s.Items, diags)
		if err != nil {
			return nil, fmt.Errorf("items: %w", err)
		}
		result.Collection = &ir.CollectionType{
			Kind:        ir.List,
			ElementType: *elem,
		}
	case SchemaTypeObject, "":
		// An omitted/empty type defaults to object per OpenAPI/JSON Schema
		// semantics; resolve it from the declared properties.
		attrs, err := propertiesToAttributes(s.Properties, s.Required, diags)
		if err != nil {
			return nil, err
		}
		result.Attributes = attrs
	default:
		return nil, fmt.Errorf("unsupported schema type %q", s.Type)
	}

	if err := ApplyAdvancedKeywords(s, result, diags); err != nil {
		return nil, err
	}

	return result, nil
}

// propertiesToAttributes converts a property map into a sorted list of
// AttributeIR values. Required properties are marked on the attribute itself.
// Properties whose names collide after ToSnakeCase normalization (e.g. "fooBar"
// and "foo_bar") are deduplicated: the property with the lexicographically
// smaller original name wins, and a warning diagnostic is emitted for each
// dropped property so the collision is not silent (L-100).
func propertiesToAttributes(props map[string]*Schema, required []string, diags *diagnostics.Diagnostics) ([]ir.AttributeIR, error) {
	requiredSet := make(map[string]struct{}, len(required))
	for _, r := range required {
		requiredSet[r] = struct{}{}
	}

	// Iterate original property names in sorted order so the dedup winner is
	// deterministic rather than dependent on map iteration order.
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	attrs := make([]ir.AttributeIR, 0, len(props))
	seen := make(map[string]string, len(props))
	for _, name := range names {
		prop := props[name]
		schema, err := schemaToIR(prop, diags)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}

		snake := SanitizeAttributeName(name)
		if prev, dup := seen[snake]; dup {
			if diags != nil {
				*diags = diags.Append(diagnostics.Diagnostic{
					Severity: diagnostics.Warning,
					Summary:  "duplicate attribute after name normalization",
					Detail:   fmt.Sprintf("properties %q and %q both normalize to %q; dropping %q", prev, name, snake, name),
				})
			}
			continue
		}
		seen[snake] = name

		attr := ir.AttributeIR{
			Name:   snake,
			Schema: *schema,
		}
		if _, ok := requiredSet[name]; ok {
			attr.Required = true
		}
		attrs = append(attrs, attr)
	}

	// Sort for deterministic output.
	sortAttributes(attrs)
	return attrs, nil
}

// convertEnum copies the version-agnostic enum values into the IR. It returns
// nil when the input slice is empty so the IR omits the field. The returned
// slice is a shallow copy: the interface{} containers are copied, but any
// mutable values inside them are shared with the source slice.
func convertEnum(values []interface{}) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, len(values))
	copy(out, values)
	return out
}

// sortAttributes sorts attributes in place by name.
func sortAttributes(attrs []ir.AttributeIR) {
	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].Name < attrs[j].Name
	})
}

// copyMapping returns a shallow copy of the discriminator mapping. If the
// input is nil, the returned map is also nil.
func copyMapping(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
