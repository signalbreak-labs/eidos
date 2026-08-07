package generator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ResourceExampleFile returns the generated examples/resources/<name>/resource.tf
// file for a single Terraform managed resource built from the supplied ResourceIR.
func ResourceExampleFile(r ir.ResourceIR) File {
	path := filepath.Join("examples", "resources", naming.SnakeCase(r.Name), "resource.tf")
	return staticFile(path, generateResourceExampleHCL(r))
}

// ResourceExampleFiles returns the generated resource example files for every
// ResourceIR in the provider. Files are emitted in the order the resources are
// supplied.
func ResourceExampleFiles(resources []ir.ResourceIR) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		files = append(files, ResourceExampleFile(r))
	}
	return files
}

// DataSourceExampleFile returns the generated examples/data-sources/<name>/data-source.tf
// file for a single Terraform data source built from the supplied DataSourceIR.
func DataSourceExampleFile(ds ir.DataSourceIR) File {
	path := filepath.Join("examples", "data-sources", naming.SnakeCase(ds.Name), "data-source.tf")
	return staticFile(path, generateDataSourceExampleHCL(ds))
}

// DataSourceExampleFiles returns the generated data source example files for every
// DataSourceIR in the provider. Files are emitted in the order supplied.
func DataSourceExampleFiles(dataSources []ir.DataSourceIR) []File {
	files := make([]File, 0, len(dataSources))
	for _, ds := range dataSources {
		files = append(files, DataSourceExampleFile(ds))
	}
	return files
}

// EphemeralResourceExampleFile returns the generated
// examples/ephemeral-resources/<name>/ephemeral-resource.tf file for a single
// Terraform ephemeral resource built from the supplied EphemeralResourceIR.
func EphemeralResourceExampleFile(er ir.EphemeralResourceIR) File {
	path := filepath.Join("examples", "ephemeral-resources", naming.SnakeCase(er.Name), "ephemeral-resource.tf")
	return staticFile(path, generateEphemeralResourceExampleHCL(er))
}

// EphemeralResourceExampleFiles returns the generated ephemeral resource example
// files for every EphemeralResourceIR in the provider.
func EphemeralResourceExampleFiles(ers []ir.EphemeralResourceIR) []File {
	files := make([]File, 0, len(ers))
	for _, er := range ers {
		files = append(files, EphemeralResourceExampleFile(er))
	}
	return files
}

// ActionExampleFile returns the generated examples/actions/<name>/action.tf file
// for a single Terraform action built from the supplied ActionIR.
func ActionExampleFile(a ir.ActionIR) File {
	path := filepath.Join("examples", "actions", naming.SnakeCase(a.Name), "action.tf")
	return staticFile(path, generateActionExampleHCL(a))
}

// ActionExampleFiles returns the generated action example files for every
// ActionIR in the provider.
func ActionExampleFiles(actions []ir.ActionIR) []File {
	files := make([]File, 0, len(actions))
	for _, a := range actions {
		files = append(files, ActionExampleFile(a))
	}
	return files
}

// ExampleFiles returns the complete set of generated HCL example files for a
// provider. It includes resources, data sources, ephemeral resources, and
// actions when the provider defines them. Both a nil provider and a non-nil
// provider that produces no files return nil, so callers can rely on a single
// zero-state value.
func ExampleFiles(pir *ir.ProviderIR) []File {
	if pir == nil {
		return nil
	}

	files := make([]File, 0, 4)
	files = append(files, ResourceExampleFiles(pir.Resources)...)
	files = append(files, DataSourceExampleFiles(pir.DataSources)...)
	files = append(files, EphemeralResourceExampleFiles(pir.EphemeralResources)...)
	files = append(files, ActionExampleFiles(pir.Actions)...)
	if len(files) == 0 {
		return nil
	}
	return files
}

// hclBuilder accumulates indented HCL text.
type hclBuilder struct {
	b      strings.Builder
	indent int
}

// writeLinef appends an indented formatted line followed by a newline.
func (h *hclBuilder) writeLinef(format string, args ...any) {
	h.b.WriteString(strings.Repeat("  ", h.indent))
	fmt.Fprintf(&h.b, format, args...)
	h.b.WriteByte('\n')
}

// generateResourceExampleHCL builds the HCL body for a managed resource example.
func generateResourceExampleHCL(r ir.ResourceIR) string {
	var h hclBuilder
	h.writeLinef(`resource "%s" "example" {`, resourceTypeName(r))
	h.indent++
	writeHCLBody(&h, r.Schema)
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// generateDataSourceExampleHCL builds the HCL body for a data source example.
func generateDataSourceExampleHCL(ds ir.DataSourceIR) string {
	var h hclBuilder
	h.writeLinef(`data "%s" "example" {`, dataSourceTypeName(ds))
	h.indent++
	writeHCLBody(&h, ds.Schema)
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// generateEphemeralResourceExampleHCL builds the HCL body for an ephemeral resource example.
func generateEphemeralResourceExampleHCL(er ir.EphemeralResourceIR) string {
	var h hclBuilder
	h.writeLinef(`ephemeral "%s" "example" {`, ephemeralResourceTypeName(er))
	h.indent++
	writeHCLBody(&h, er.ConfigSchema)
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// generateActionExampleHCL builds the HCL body for an action example.
func generateActionExampleHCL(a ir.ActionIR) string {
	var h hclBuilder
	h.writeLinef(`action "%s" "example" {`, actionExampleTypeName(a))
	h.indent++
	writeHCLBody(&h, a.ConfigSchema)
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// actionExampleTypeName returns the Terraform action type name to use in the
// generated example. It prefers ActionIR.TypeName and falls back to a
// snake_cased action name when TypeName is empty, consistent with the other
// example type-name helpers.
func actionExampleTypeName(a ir.ActionIR) string {
	if t := actionTypeName(a); t != "" {
		return t
	}
	return typeNameFallback(a.Name)
}

// writeHCLBody renders configurable attributes and blocks for an object schema.
// Computed-only attributes are omitted because they are set by the provider.
func writeHCLBody(h *hclBuilder, obj ir.ObjectSchemaIR) {
	for _, attr := range obj.Attributes {
		if !includeInExample(attr) {
			continue
		}
		writeHCLAttribute(h, attr)
	}
	for _, block := range obj.Blocks {
		writeHCLBlock(h, block)
	}
}

// includeInExample reports whether an attribute should appear in an HCL example.
// Configurable attributes (required or optional) are included; computed-only
// attributes are omitted because they are set by the provider.
func includeInExample(attr ir.AttributeIR) bool {
	return attr.Required || attr.Optional
}

// writeHCLAttribute renders a single configurable attribute in HCL.
//
// Object-like attributes map to SingleNestedAttribute in the generated schema,
// so they must use assignment syntax (`name = { ... }`), not block syntax
// (`name { ... }`). Block syntax is reserved for real nested blocks (ir.BlockIR),
// which are rendered by writeHCLBlock.
func writeHCLAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema

	if s.Collection != nil {
		writeHCLCollectionAttribute(h, attr)
		return
	}

	if schema.IsObjectLike(s) {
		// Single nested attribute: assignment syntax (SingleNestedAttribute).
		h.writeLinef("%s = {", attr.Name)
		h.indent++
		writeHCLBody(h, ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks})
		h.indent--
		h.writeLinef("}")
		return
	}

	h.writeLinef("%s = %s", attr.Name, primitiveExampleValue(s.Type))
}

// writeHCLCollectionAttribute renders a list, set, or map attribute in HCL.
//
// List/Set-of-object and Map-of-object attributes map to ListNestedAttribute,
// SetNestedAttribute, and MapNestedAttribute respectively in the generated
// schema, so they must use assignment syntax with object/map literals — not
// repeated block syntax. Only real nested blocks (ir.BlockIR) use block syntax.
func writeHCLCollectionAttribute(h *hclBuilder, attr ir.AttributeIR) {
	s := attr.Schema
	elem := s.Collection.ElementType

	switch s.Collection.Kind {
	case ir.List, ir.Set:
		if schema.IsPrimitiveSchema(elem) {
			// Primitive lists/sets render as compact single-line literals; keeping
			// primitives on one line makes simple examples easier to scan.
			h.writeLinef("%s = [ %s ]", attr.Name, primitiveExampleValue(elem.Type))
			return
		}
		if schema.IsObjectLike(elem) {
			// Object collections (ListNestedAttribute/SetNestedAttribute) use a
			// list-of-objects assignment literal.
			h.writeLinef("%s = [{", attr.Name)
			h.indent++
			writeHCLBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks})
			h.indent--
			h.writeLinef("}]")
			return
		}
	case ir.Map:
		// Use a domain-relevant placeholder key derived from the attribute name
		// rather than the generic "key" string.
		key := naming.SnakeCase(attr.Name)
		if schema.IsPrimitiveSchema(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef("%q = %s", key, primitiveExampleValue(elem.Type))
			h.indent--
			h.writeLinef("}")
			return
		}
		if schema.IsObjectLike(elem) {
			h.writeLinef("%s = {", attr.Name)
			h.indent++
			h.writeLinef("%q = {", key)
			h.indent++
			writeHCLBody(h, ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks})
			h.indent--
			h.writeLinef("}")
			h.indent--
			h.writeLinef("}")
			return
		}
	}

	// Fallback for unsupported collection element types (for example, an
	// OpenAPI array whose items are a oneOf/anyOf union, which maps to a
	// DynamicAttribute element). Rather than panicking and aborting the whole
	// CLI run, emit an empty literal so example generation degrades gracefully.
	// This mirrors the acceptance-test HCL writer, which emits `name = []`.
	switch s.Collection.Kind {
	case ir.Map:
		h.writeLinef("%s = {}", attr.Name)
	default:
		h.writeLinef("%s = []", attr.Name)
	}
}

// writeHCLBlock renders a nested block in HCL.
func writeHCLBlock(h *hclBuilder, block ir.BlockIR) {
	h.writeLinef("%s {", block.Name)
	h.indent++
	writeHCLBody(h, block.Schema)
	h.indent--
	h.writeLinef("}")
}

// primitiveExampleValue returns a placeholder HCL literal for a primitive type.
// Unrecognized PrimitiveType constants fall back to a string placeholder so that
// example generation does not fail silently; when adding a new PrimitiveType,
// update this switch to return a type-appropriate value.
func primitiveExampleValue(t ir.PrimitiveType) string {
	switch t {
	case ir.TypeString:
		return `"example"`
	case ir.TypeInt:
		return "1"
	case ir.TypeFloat:
		return "1.0"
	case ir.TypeBool:
		return "true"
	case ir.TypeDynamic:
		return "null"
	default:
		return `"example"`
	}
}
