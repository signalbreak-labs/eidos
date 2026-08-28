package transformer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// PolymorphismStrategy selects how a polymorphic OpenAPI schema is mapped to
// Terraform Plugin Framework constructs.
type PolymorphismStrategy string

// Polymorphism context constants.
const (
	// StrategyDynamicUnion preserves the polymorphic union as a single Terraform
	// attribute: a DynamicAttribute when no discriminator exists, or a
	// discriminated SingleNestedAttribute when a discriminator is present.
	StrategyDynamicUnion PolymorphismStrategy = "dynamic_union"
	// StrategySplitResources emits one Terraform resource per concrete variant.
	StrategySplitResources PolymorphismStrategy = "split_resources"
)

// PolymorphismContext describes where the union appears in the spec so that
// heuristics can choose an appropriate default strategy.
type PolymorphismContext int

// Polymorphism context constants.
const (
	// ContextUnknown means the polymorphism context is not yet determined.
	ContextUnknown PolymorphismContext = iota
	ContextTopLevelResource
	ContextRequestBody
	ContextResponseBody
	ContextNestedAttribute
)

// PolymorphismConfig holds user overrides from generator.yaml.
type PolymorphismConfig struct {
	// Strategy is the global default when neither heuristics nor a per-schema
	// override apply.
	Strategy string
	// OneOf lists per-schema configurations keyed by the parent schema name.
	OneOf []PolymorphismOneOfConfig
}

// PolymorphismOneOfConfig configures a single oneOf schema.
type PolymorphismOneOfConfig struct {
	Schema   string
	Strategy string
	Variants []PolymorphismVariantConfig
}

// PolymorphismVariantConfig configures the generated name for a concrete
// variant when the split_resources strategy is chosen.
type PolymorphismVariantConfig struct {
	Schema         string
	ResourceName   string
	DataSourceName string
}

// SelectStrategy chooses a polymorphism mapping strategy using, in order:
//  1. A per-schema generator.yaml override.
//  2. The global generator.yaml default strategy.
//  3. Heuristics based on context and schema shape.
func SelectStrategy(schema *ir.SchemaIR, ctx PolymorphismContext, cfg PolymorphismConfig) (PolymorphismStrategy, error) {
	if schema == nil || schema.Union == nil {
		return "", fmt.Errorf("schema is not a polymorphic union")
	}

	schemaName := schema.Name

	for _, o := range cfg.OneOf {
		if o.Schema == schemaName && o.Strategy != "" {
			// L-104: validate the per-schema strategy so a typo like
			// `polymorphism.oneOf[i].strategy: bogus` is rejected here rather than
			// silently accepted (config.Validate only checks the global strategy).
			if err := validateStrategy(o.Strategy); err != nil {
				return "", fmt.Errorf("polymorphism.oneOf schema %q: %w", schemaName, err)
			}
			return PolymorphismStrategy(o.Strategy), nil
		}
	}

	if cfg.Strategy != "" {
		return PolymorphismStrategy(cfg.Strategy), nil
	}

	if ctx == ContextTopLevelResource &&
		(schema.Union.Kind == ir.OneOf || schema.Union.Kind == "") &&
		allVariantsObjectLike(schema.Union.Variants) &&
		allVariantsNamed(schema.Union.Variants) {
		return StrategySplitResources, nil
	}

	return StrategyDynamicUnion, nil
}

// validateStrategy reports whether s is a recognized polymorphism strategy.
// Used to reject invalid per-schema overrides (L-104).
func validateStrategy(s string) error {
	switch PolymorphismStrategy(s) {
	case StrategyDynamicUnion, StrategySplitResources:
		return nil
	default:
		return fmt.Errorf("invalid polymorphism strategy %q (want %q or %q)", s, StrategyDynamicUnion, StrategySplitResources)
	}
}

// ApplyDynamicUnion transforms a polymorphic union into a Terraform attribute
// schema. Unions without a discriminator become DynamicAttribute schemas;
// unions with a discriminator become a SingleNestedAttribute containing all
// variant fields plus the discriminator attribute.
func ApplyDynamicUnion(schema *ir.SchemaIR) (*ir.SchemaIR, error) {
	if schema == nil || schema.Union == nil {
		return nil, fmt.Errorf("schema is not a polymorphic union")
	}

	union := schema.Union

	if union.Discriminator == nil {
		return &ir.SchemaIR{
			Name:        schema.Name,
			Description: schema.Description,
			Type:        ir.TypeDynamic,
		}, nil
	}

	// L-103: a discriminator with an empty propertyName would yield an attribute
	// named "" and validator args containing "", producing invalid output with no
	// error. Surface it as an explicit error instead.
	if strings.TrimSpace(union.Discriminator.PropertyName) == "" {
		return nil, fmt.Errorf("polymorphic schema %q has a discriminator with an empty property name", schema.Name)
	}

	merged := ir.SchemaIR{
		Name:        schema.Name,
		Description: schema.Description,
	}

	seen := make(map[string]ir.SchemaIR)
	discName := ToSnakeCase(union.Discriminator.PropertyName)

	for _, variant := range union.Variants {
		for _, attr := range variant.Attributes {
			attrName := ToSnakeCase(attr.Name)
			if attrName == discName {
				continue
			}
			// Variants in a discriminated oneOf almost always share base fields
			// (that is the discriminator pattern: common base + variant-specific
			// fields). Identical duplicates (same name, same type) are deduped
			// first-wins, mirroring mergeAllOfSchemaSpec; a same-named attribute
			// with a conflicting type cannot be represented in the merged object
			// and is an error.
			if prev, ok := seen[attrName]; ok {
				if !schemaIRSameShape(prev, attr.Schema) {
					return nil, fmt.Errorf("conflicting attribute %q across union variants", attrName)
				}
				continue
			}
			seen[attrName] = attr.Schema

			copyAttr := attr
			copyAttr.Name = attrName
			copyAttr.Required = false
			copyAttr.Optional = true
			merged.Attributes = append(merged.Attributes, copyAttr)
		}

		for _, block := range variant.Blocks {
			if _, dup := seen[block.Name]; dup {
				return nil, fmt.Errorf("duplicate block %q across union variants", block.Name)
			}
			seen[block.Name] = ir.SchemaIR{Type: ir.TypeDynamic}
			merged.Blocks = append(merged.Blocks, block)
		}
	}

	discAttr := ir.AttributeIR{
		Name:     discName,
		Required: true,
		Schema: ir.SchemaIR{
			Type:       ir.TypeString,
			EnumValues: stringKeysToAny(union.Discriminator.Mapping),
		},
	}
	merged.Attributes = append([]ir.AttributeIR{discAttr}, merged.Attributes...)

	args := []string{strconv.Quote(discName)}
	for _, key := range sortedStringKeys(union.Discriminator.Mapping) {
		args = append(args, strconv.Quote(key), strconv.Quote(union.Discriminator.Mapping[key]))
	}
	merged.Validators = []ir.ValidatorIR{{
		Type: "validators.DiscriminatorValidator",
		Args: args,
	}}

	return &merged, nil
}

// SplitResources emits a separate ResourceIR for each concrete variant of a
// top-level polymorphic union. The original discriminator property is omitted
// because the Terraform resource type itself encodes the selected variant.
func SplitResources(baseName string, schema *ir.SchemaIR, cfg PolymorphismConfig) ([]ir.ResourceIR, error) {
	if schema == nil || schema.Union == nil {
		return nil, fmt.Errorf("schema is not a polymorphic union")
	}

	union := schema.Union

	// The discriminator property is removed from each variant because the
	// Terraform resource type itself encodes the selected variant. Variant
	// attributes are snake_cased during schema conversion, so the discriminator
	// name must be snake_cased too — a raw PropertyName like "petType" would
	// otherwise silently fail the exact case-sensitive compare in
	// filterAttributeByName and leave pet_type in every variant (M-6).
	discProp := ""
	if union.Discriminator != nil {
		discProp = ToSnakeCase(union.Discriminator.PropertyName)
	}

	var resources []ir.ResourceIR

	for _, variant := range union.Variants {
		variantName := variant.Name
		if variantName == "" {
			return nil, fmt.Errorf("unnamed variant in polymorphic schema %q", baseName)
		}

		override := findVariantOverride(cfg.OneOf, baseName, variantName)

		var resourceName string
		if override != nil && override.ResourceName != "" {
			resourceName = override.ResourceName
		} else {
			resourceName = ToSnakeCase(variantName)
		}

		obj := ir.ObjectSchemaIR{}
		if len(variant.Attributes) > 0 {
			obj.Attributes = append(obj.Attributes, variant.Attributes...)
		}
		if len(variant.Blocks) > 0 {
			obj.Blocks = append(obj.Blocks, variant.Blocks...)
		}

		if discProp != "" {
			obj.Attributes = filterAttributeByName(obj.Attributes, discProp)
		}

		resources = append(resources, ir.ResourceIR{
			Name:        variantName,
			TypeName:    resourceName,
			Description: variant.Description,
			Schema:      obj,
		})
	}

	return resources, nil
}

// schemaIRSameShape reports whether two schemas have the same renderable
// shape for the purpose of deduplicating shared variant fields in a merged
// discriminated union: primitive type, format, collection kind, and whether
// they are object-like. It is a shallow structural comparison — two
// same-named variant attributes that agree on these are the same field
// declared on both variants (the common discriminator base), while any
// difference makes the duplicate a genuine conflict.
func schemaIRSameShape(a, b ir.SchemaIR) bool {
	if a.Type != b.Type || a.Format != b.Format {
		return false
	}
	if (a.Collection == nil) != (b.Collection == nil) {
		return false
	}
	if a.Collection != nil && a.Collection.Kind != b.Collection.Kind {
		return false
	}
	if (a.Union == nil) != (b.Union == nil) {
		return false
	}
	return isObjectLike(a) == isObjectLike(b)
}

// findVariantOverride looks up the per-variant name override for variantName
// across all OneOf config entries matching baseName. Matching is case-insensitive
// and consults every matching entry (not just the first), consistent with the
// rest of the transformer (L-105).
func findVariantOverride(oneOf []PolymorphismOneOfConfig, baseName, variantName string) *PolymorphismVariantConfig {
	for _, o := range oneOf {
		if o.Schema != baseName {
			continue
		}
		for _, v := range o.Variants {
			if strings.EqualFold(v.Schema, variantName) {
				cp := v
				return &cp
			}
		}
	}
	return nil
}

func allVariantsObjectLike(variants []ir.SchemaIR) bool {
	for _, v := range variants {
		if !isObjectLike(v) {
			return false
		}
	}
	return len(variants) > 0
}

func allVariantsNamed(variants []ir.SchemaIR) bool {
	for _, v := range variants {
		if v.Name == "" {
			return false
		}
	}
	return true
}

func isObjectLike(v ir.SchemaIR) bool {
	if len(v.Attributes) > 0 || len(v.Blocks) > 0 {
		return true
	}
	// A bare object schema may have no populated Attributes/Blocks yet; treat an
	// empty schema node as object-like as long as it is not a primitive or
	// collection.
	if v.Type == "" && v.Collection == nil && v.Union == nil {
		return true
	}
	return false
}

func filterAttributeByName(attrs []ir.AttributeIR, name string) []ir.AttributeIR {
	out := attrs[:0]
	for _, a := range attrs {
		if a.Name != name {
			out = append(out, a)
		}
	}
	return out
}

func stringKeysToAny(m map[string]string) []any {
	keys := sortedStringKeys(m)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
