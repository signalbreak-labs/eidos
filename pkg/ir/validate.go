package ir

import (
	"fmt"
	"sort"
)

// ValidateProviderIR checks the IR invariants enforced by SchemaIR.Validate
// (Type/Collection/Union exclusivity, Required && Optional) across every schema
// node reachable from a provider: the config schema, resources, data sources,
// actions, ephemeral resources, list resources, and function signatures. It
// returns one error per offending node, keyed by a dotted path so the offending
// construct is identifiable.
//
// This is the production enforcement site that makes SchemaIR.Validate
// reachable (N-48). Previously the method had zero production callers, so a
// transformer bug producing e.g. both Type and Collection on one node passed
// silently until the generated provider failed to compile. The API layer calls
// this after the provider is fully assembled (post-overrides) and surfaces any
// violation as a fail-loud Error diagnostic.
func ValidateProviderIR(provider *ProviderIR) []error {
	if provider == nil {
		return nil
	}
	var errs []error
	errs = append(errs, validateObjectSchema(provider.ConfigSchema, "config_schema")...)
	for i := range provider.Resources {
		errs = append(errs, validateObjectSchema(provider.Resources[i].Schema, fmt.Sprintf("resources[%d].schema", i))...)
		if provider.Resources[i].IdentitySchema != nil {
			errs = append(errs, validateObjectSchema(*provider.Resources[i].IdentitySchema, fmt.Sprintf("resources[%d].identity_schema", i))...)
		}
	}
	for i := range provider.DataSources {
		errs = append(errs, validateObjectSchema(provider.DataSources[i].Schema, fmt.Sprintf("data_sources[%d].schema", i))...)
	}
	for i := range provider.Actions {
		errs = append(errs, validateObjectSchema(provider.Actions[i].ConfigSchema, fmt.Sprintf("actions[%d].config_schema", i))...)
	}
	for i := range provider.EphemeralResources {
		errs = append(errs, validateObjectSchema(provider.EphemeralResources[i].ConfigSchema, fmt.Sprintf("ephemeral_resources[%d].config_schema", i))...)
		errs = append(errs, validateObjectSchema(provider.EphemeralResources[i].ResultSchema, fmt.Sprintf("ephemeral_resources[%d].result_schema", i))...)
	}
	for i := range provider.ListResources {
		errs = append(errs, validateObjectSchema(provider.ListResources[i].ConfigSchema, fmt.Sprintf("list_resources[%d].config_schema", i))...)
		errs = append(errs, validateObjectSchema(provider.ListResources[i].IdentitySchema, fmt.Sprintf("list_resources[%d].identity_schema", i))...)
		if provider.ListResources[i].ResourceSchema != nil {
			errs = append(errs, validateObjectSchema(*provider.ListResources[i].ResourceSchema, fmt.Sprintf("list_resources[%d].resource_schema", i))...)
		}
	}
	for i := range provider.Functions {
		fn := &provider.Functions[i]
		for j := range fn.Arguments {
			errs = append(errs, validateSchemaNode(fn.Arguments[j].Schema, fmt.Sprintf("functions[%d].arguments[%d].schema", i, j))...)
		}
		errs = append(errs, validateSchemaNode(fn.ReturnType, fmt.Sprintf("functions[%d].return_type", i))...)
	}
	return errs
}

// validateObjectSchema validates the schema nodes reachable from an
// object-level schema (attributes plus blocks).
func validateObjectSchema(obj ObjectSchemaIR, path string) []error {
	errs := make([]error, 0, len(obj.Attributes)+len(obj.Blocks))
	for i := range obj.Attributes {
		errs = append(errs, validateSchemaNode(obj.Attributes[i].Schema, fmt.Sprintf("%s.attributes[%d].schema", path, i))...)
	}
	for i := range obj.Blocks {
		errs = append(errs, validateObjectSchema(obj.Blocks[i].Schema, fmt.Sprintf("%s.blocks[%d].schema", path, i))...)
	}
	return errs
}

// validateSchemaNode validates one schema node and recurses into every
// reachable child (nested attributes/blocks, collection element types, union
// variants, and the JSON Schema conditional/pattern siblings).
func validateSchemaNode(s SchemaIR, path string) []error {
	var errs []error
	if err := s.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", path, err))
	}
	for i := range s.Attributes {
		errs = append(errs, validateSchemaNode(s.Attributes[i].Schema, fmt.Sprintf("%s.attributes[%d].schema", path, i))...)
	}
	for i := range s.Blocks {
		errs = append(errs, validateObjectSchema(s.Blocks[i].Schema, fmt.Sprintf("%s.blocks[%d].schema", path, i))...)
	}
	if s.Collection != nil {
		errs = append(errs, validateSchemaNode(s.Collection.ElementType, path+".collection.element_type")...)
	}
	if s.Union != nil {
		for i := range s.Union.Variants {
			errs = append(errs, validateSchemaNode(s.Union.Variants[i], fmt.Sprintf("%s.union.variants[%d]", path, i))...)
		}
	}
	for _, sub := range []*SchemaIR{s.Not, s.IfSchema, s.ThenSchema, s.ElseSchema} {
		if sub != nil {
			errs = append(errs, validateSchemaNode(*sub, path+".subschema")...)
		}
	}
	// Maps are iterated in sorted key order so the emitted errors (when several
	// children are invalid) are deterministic across runs (N-42).
	for _, name := range sortedSchemaKeys(s.DependentSchemas) {
		errs = append(errs, validateSchemaNode(*s.DependentSchemas[name], fmt.Sprintf("%s.dependent_schemas[%q]", path, name))...)
	}
	for _, name := range sortedSchemaKeys(s.PatternProperties) {
		errs = append(errs, validateSchemaNode(*s.PatternProperties[name], fmt.Sprintf("%s.pattern_properties[%q]", path, name))...)
	}
	if s.PropertyNames != nil {
		errs = append(errs, validateSchemaNode(*s.PropertyNames, path+".property_names")...)
	}
	if s.UnevaluatedProperties != nil {
		errs = append(errs, validateSchemaNode(*s.UnevaluatedProperties, path+".unevaluated_properties")...)
	}
	return errs
}

// sortedSchemaKeys returns the keys of a schema map in sorted order so
// validation errors are deterministic.
func sortedSchemaKeys(m map[string]*SchemaIR) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
