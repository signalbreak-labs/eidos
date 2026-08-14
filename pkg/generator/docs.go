package generator

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ProviderDocsIndex returns the generated docs/index.md file for a provider.
func ProviderDocsIndex(pir ir.ProviderIR) File {
	name := providerDocsTypeName(pir)
	resources := make([]docsResourceRef, 0, len(pir.Resources))
	for _, r := range pir.Resources {
		resources = append(resources, docsResourceRef{
			TypeName: resourceDocsTypeName(r),
			FileName: naming.SnakeCase(r.Name) + ".md",
		})
	}
	dataSources := make([]docsDataSourceRef, 0, len(pir.DataSources))
	for _, ds := range pir.DataSources {
		dataSources = append(dataSources, docsDataSourceRef{
			TypeName: dataSourceDocsTypeName(ds),
			FileName: naming.SnakeCase(ds.Name) + ".md",
		})
	}
	actions := make([]docsActionRef, 0, len(pir.Actions))
	for _, a := range pir.Actions {
		actions = append(actions, docsActionRef{
			TypeName: actionDocsTypeName(a),
			FileName: naming.SnakeCase(a.Name) + ".md",
		})
	}
	ephemeralResources := make([]docsEphemeralResourceRef, 0, len(pir.EphemeralResources))
	for _, er := range pir.EphemeralResources {
		ephemeralResources = append(ephemeralResources, docsEphemeralResourceRef{
			TypeName: ephemeralResourceDocsTypeName(er),
			FileName: naming.SnakeCase(er.Name) + ".md",
		})
	}
	listResources := make([]docsListResourceRef, 0, len(pir.ListResources))
	for _, lr := range pir.ListResources {
		listResources = append(listResources, docsListResourceRef{
			TypeName: listResourceDocsTypeName(lr),
			FileName: naming.SnakeCase(lr.Name) + ".md",
		})
	}
	functions := make([]docsFunctionRef, 0, len(pir.Functions))
	for _, fn := range pir.Functions {
		functions = append(functions, docsFunctionRef{
			TypeName: functionDocsTypeName(fn),
			FileName: naming.SnakeCase(fn.Name) + ".md",
		})
	}

	return Template("docs/index.md", indexTemplate, map[string]any{
		"ProviderName":       name,
		"Description":        escapeDescription(pir.Description),
		"Resources":          resources,
		"DataSources":        dataSources,
		"Actions":            actions,
		"EphemeralResources": ephemeralResources,
		"ListResources":      listResources,
		"Functions":          functions,
	})
}

// ProviderDocsFiles returns all Markdown documentation files for a provider:
// docs/index.md plus docs for resources, data sources, actions, ephemeral
// resources, list resources, and functions when the provider defines them.
func ProviderDocsFiles(pir ir.ProviderIR) []File {
	files := make([]File, 0, 1+len(pir.Resources)+len(pir.DataSources)+
		len(pir.Actions)+len(pir.EphemeralResources)+len(pir.ListResources)+len(pir.Functions))
	files = append(files, ProviderDocsIndex(pir))
	files = append(files, ResourceDocsFiles(pir.Resources)...)
	files = append(files, DataSourceDocsFiles(pir.DataSources)...)
	files = append(files, ActionDocsFiles(pir.Actions)...)
	files = append(files, EphemeralResourceDocsFiles(pir.EphemeralResources)...)
	files = append(files, ListResourceDocsFiles(pir.ListResources)...)
	files = append(files, FunctionDocsFiles(pir.Functions, providerDocsTypeName(pir))...)
	return files
}

// ResourceDocsFile returns the generated docs/resources/<name>.md file for a
// single managed resource.
func ResourceDocsFile(r ir.ResourceIR) File {
	path := fmt.Sprintf("docs/resources/%s.md", naming.SnakeCase(r.Name))
	data := map[string]any{
		"ResourceName": resourceDocsTypeName(r),
		"ProviderName": providerDocsTypeNameFromResource(r),
		"Description":  escapeDescription(r.Description),
		"ExampleArgs":  renderExampleArguments(r.Schema.Attributes),
		"Arguments":    renderAttributeSection(r.Schema.Attributes, true),
		"Attributes":   renderAttributeSection(r.Schema.Attributes, false),
		"Blocks":       renderBlockSection(r.Schema.Blocks),
		"Importable":   r.Importable,
		"ImportFormat": docsImportFormat(r.ImportIDFormat),
	}
	return Template(path, resourceTemplate, data)
}

// ResourceDocsFiles returns the generated resource documentation files for all
// supplied ResourceIR values, preserving order.
func ResourceDocsFiles(resources []ir.ResourceIR) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		files = append(files, ResourceDocsFile(r))
	}
	return files
}

// DataSourceDocsFile returns the generated docs/data-sources/<name>.md file for
// a single data source.
func DataSourceDocsFile(ds ir.DataSourceIR) File {
	path := fmt.Sprintf("docs/data-sources/%s.md", naming.SnakeCase(ds.Name))
	data := map[string]any{
		"DataSourceName": dataSourceDocsTypeName(ds),
		"ProviderName":   providerDocsTypeNameFromDataSource(ds),
		"Description":    escapeDescription(ds.Description),
		"ExampleArgs":    renderExampleArguments(ds.Schema.Attributes),
		"Arguments":      renderAttributeSection(ds.Schema.Attributes, true),
		"Attributes":     renderAttributeSection(ds.Schema.Attributes, false),
		"Blocks":         renderBlockSection(ds.Schema.Blocks),
	}
	return Template(path, dataSourceTemplate, data)
}

// DataSourceDocsFiles returns the generated data source documentation files for
// all supplied DataSourceIR values, preserving order.
func DataSourceDocsFiles(dss []ir.DataSourceIR) []File {
	files := make([]File, 0, len(dss))
	for _, ds := range dss {
		files = append(files, DataSourceDocsFile(ds))
	}
	return files
}

// docsResourceRef is a precomputed link reference used by the index template.
type docsResourceRef struct {
	TypeName string
	FileName string
}

// docsDataSourceRef is a precomputed link reference used by the index template.
type docsDataSourceRef struct {
	TypeName string
	FileName string
}

// providerDocsTypeName returns the provider type name for documentation.
// It prefers ProviderIR.TypeName and falls back to ProviderIR.Name.
func providerDocsTypeName(pir ir.ProviderIR) string {
	if strings.TrimSpace(pir.TypeName) != "" {
		return strings.TrimSpace(pir.TypeName)
	}
	return strings.TrimSpace(pir.Name)
}

// providerDocsTypeNameFromResource returns the provider portion of a resource
// type name when the provider IR is not available. It extracts the prefix up to
// the first underscore, falling back to the resource name itself.
func providerDocsTypeNameFromResource(r ir.ResourceIR) string {
	return providerDocsPrefix(resourceDocsTypeName(r))
}

// providerDocsTypeNameFromDataSource returns the provider portion of a data
// source type name. It extracts the prefix up to the first underscore.
func providerDocsTypeNameFromDataSource(ds ir.DataSourceIR) string {
	return providerDocsPrefix(dataSourceDocsTypeName(ds))
}

// docsTypeNameOrFallback returns the configured type name if it is non-empty,
// otherwise applies transform to name. It is used by all docs type-name helpers
// so that fallback names are consistently normalized.
func docsTypeNameOrFallback(typeName, name string, transform func(string) string) string {
	if t := strings.TrimSpace(typeName); t != "" {
		return t
	}
	return transform(strings.TrimSpace(name))
}

// providerDocsPrefix returns the provider portion of a Terraform type name that
// follows the <provider>_<resource> naming convention. It splits on the first
// underscore and returns the prefix; if no underscore is present, the full name
// is returned as a non-empty fallback. Callers should ensure type names follow
// the convention for accurate results.
func providerDocsPrefix(typeName string) string {
	parts := strings.SplitN(typeName, "_", 2)
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		return parts[0]
	}
	return typeName
}

// resourceDocsTypeName returns the Terraform resource type name for docs. It
// prefers ResourceIR.TypeName and falls back to ResourceIR.Name.
func resourceDocsTypeName(r ir.ResourceIR) string {
	if strings.TrimSpace(r.TypeName) != "" {
		return strings.TrimSpace(r.TypeName)
	}
	return strings.TrimSpace(r.Name)
}

// dataSourceDocsTypeName returns the Terraform data source type name for docs.
// It prefers DataSourceIR.TypeName and falls back to DataSourceIR.Name.
func dataSourceDocsTypeName(ds ir.DataSourceIR) string {
	if strings.TrimSpace(ds.TypeName) != "" {
		return strings.TrimSpace(ds.TypeName)
	}
	return strings.TrimSpace(ds.Name)
}

// indexTemplate is the Terraform Registry-compatible frontmatter and body for
// docs/index.md.
const indexTemplate = `---
page_title: "{{.ProviderName}} Provider"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.ProviderName}} Provider

{{.Description}}

{{if .Resources -}}
## Resources

{{range .Resources}}- [{{.TypeName}}](resources/{{.FileName}})
{{end}}
{{end -}}
{{if .DataSources -}}
## Data Sources

{{range .DataSources}}- [{{.TypeName}}](data-sources/{{.FileName}})
{{end}}
{{end -}}
{{if .Actions -}}
## Actions

{{range .Actions}}- [{{.TypeName}}](actions/{{.FileName}})
{{end}}
{{end -}}
{{if .EphemeralResources -}}
## Ephemeral Resources

{{range .EphemeralResources}}- [{{.TypeName}}](ephemeral-resources/{{.FileName}})
{{end}}
{{end -}}
{{if .ListResources -}}
## List Resources

{{range .ListResources}}- [{{.TypeName}}](list-resources/{{.FileName}})
{{end}}
{{end -}}
{{if .Functions -}}
## Functions

{{range .Functions}}- [{{.TypeName}}](functions/{{.FileName}})
{{end}}
{{end -}}
`

// resourceTemplate is the Terraform Registry-compatible frontmatter and body
// for a single docs/resources/<name>.md file.
const resourceTemplate = `---
page_title: "{{.ResourceName}} Resource - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.ResourceName}} Resource

{{.Description}}

## Example Usage

` + "```terraform" + `
resource "{{.ResourceName}}" "example" {
{{.ExampleArgs}}}
` + "```" + `

## Schema

### Arguments

The following arguments are supported:

{{.Arguments}}
### Attributes

In addition to all arguments above, the following computed attributes are exported:

{{.Attributes}}
{{if .Blocks -}}
### Nested Blocks

{{.Blocks}}
{{end -}}
{{if .Importable -}}
## Import

Import is supported using the following syntax:

` + "```shell" + `
terraform import {{.ResourceName}}.example{{.ImportFormat}}
` + "```" + `
{{end -}}
`

// dataSourceTemplate is the Terraform Registry-compatible frontmatter and body
// for a single docs/data-sources/<name>.md file.
const dataSourceTemplate = `---
page_title: "{{.DataSourceName}} Data Source - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.DataSourceName}} Data Source

{{.Description}}

## Example Usage

` + "```terraform" + `
data "{{.DataSourceName}}" "example" {
{{.ExampleArgs}}}
` + "```" + `

## Schema

### Arguments

The following arguments are supported:

{{.Arguments}}
### Attributes

In addition to all arguments above, the following attributes are exported:

{{.Attributes}}
{{if .Blocks -}}
### Nested Blocks

{{.Blocks}}
{{end -}}
`

// renderExampleArguments renders required and optional attributes as HCL-style
// example arguments, indented by two spaces. Object attributes and collections
// are emitted as empty literals (`{}` or `[]`) so the example remains
// syntactically valid Terraform.
func renderExampleArguments(attrs []ir.AttributeIR) string {
	var b strings.Builder
	for _, attr := range attrs {
		if !attr.Required && !attr.Optional {
			continue
		}
		if attr.Schema.Collection != nil {
			switch attr.Schema.Collection.Kind {
			case ir.Map:
				fmt.Fprintf(&b, "  %s = {}\n", attr.Name)
			default:
				fmt.Fprintf(&b, "  %s = []\n", attr.Name)
			}
			continue
		}
		if schema.IsObjectLike(attr.Schema) {
			fmt.Fprintf(&b, "  %s = {}\n", attr.Name)
			continue
		}
		// Use null rather than "..." so the emitted example is syntactically
		// valid, copy-pasteable HCL (L-33: "..." is not a valid HCL expression).
		fmt.Fprintf(&b, "  %s = null\n", attr.Name)
	}
	return b.String()
}

// renderAttributeSection renders the schema reference rows for a set of
// attributes. When arguments is true, only Required/Optional attributes are
// rendered; otherwise only Computed attributes are rendered. Each row is
// indented by two spaces for the example usage block and by none for the
// reference list.
func renderAttributeSection(attrs []ir.AttributeIR, arguments bool) string {
	var b strings.Builder
	for _, attr := range attrs {
		include := false
		qualifier := ""
		if arguments {
			if attr.Required {
				include = true
				qualifier = "required"
			} else if attr.Optional {
				include = true
				qualifier = "optional"
			}
		} else {
			if attr.Computed {
				include = true
				qualifier = "computed"
			}
		}
		if !include {
			continue
		}
		writeAttributeRow(&b, attr, 0, qualifier)
	}
	return b.String()
}

// writeAttributeRow writes a single markdown bullet for an attribute with its
// type, qualifier, and optional description, then recurses into nested
// attributes and blocks.
func writeAttributeRow(b *strings.Builder, attr ir.AttributeIR, depth int, qualifier string) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s* `%s` (%s, %s)", indent, attr.Name, schemaTypeName(attr.Schema), qualifier)
	if attr.Description != "" {
		fmt.Fprintf(b, " - %s", escapeDescription(attr.Description))
	}
	fmt.Fprintln(b)
	if schema.IsObjectLike(attr.Schema) {
		writeObjectRows(b, ir.ObjectSchemaIR{Attributes: attr.Schema.Attributes, Blocks: attr.Schema.Blocks}, depth+1)
	}
}

// writeObjectRows renders markdown rows for all attributes and blocks inside
// an object schema at the given indentation depth.
func writeObjectRows(b *strings.Builder, obj ir.ObjectSchemaIR, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, attr := range obj.Attributes {
		qualifier := "computed"
		if attr.Required {
			qualifier = "required"
		} else if attr.Optional {
			qualifier = "optional"
		}
		fmt.Fprintf(b, "%s* `%s` (%s, %s)", indent, attr.Name, schemaTypeName(attr.Schema), qualifier)
		if attr.Description != "" {
			fmt.Fprintf(b, " - %s", escapeDescription(attr.Description))
		}
		fmt.Fprintln(b)
		if schema.IsObjectLike(attr.Schema) {
			writeObjectRows(b, ir.ObjectSchemaIR{Attributes: attr.Schema.Attributes, Blocks: attr.Schema.Blocks}, depth+1)
		}
	}
	for _, block := range obj.Blocks {
		fmt.Fprintf(b, "%s* `%s` (%s, %s)", indent, block.Name, blockNestingLabel(block), blockQualifier(block))
		if block.Description != "" {
			fmt.Fprintf(b, " - %s", escapeDescription(block.Description))
		}
		fmt.Fprintln(b)
		writeObjectRows(b, block.Schema, depth+1)
	}
}

// renderBlockSection renders the nested block reference section for a set of
// blocks, including their attributes.
func renderBlockSection(blocks []ir.BlockIR) string {
	var b strings.Builder
	for _, block := range blocks {
		fmt.Fprintf(&b, "* `%s` (%s, %s)", block.Name, blockNestingLabel(block), blockQualifier(block))
		if block.Description != "" {
			fmt.Fprintf(&b, " - %s", escapeDescription(block.Description))
		}
		fmt.Fprintln(&b)
		writeObjectRows(&b, block.Schema, 1)
	}
	return b.String()
}

// schemaTypeName returns a human-readable Terraform type name for an IR schema.
// Object-like schemas include the names of their top-level attributes and
// blocks so operators can see the expected structure.
func schemaTypeName(s ir.SchemaIR) string {
	if s.Collection != nil {
		kind := collectionKindLabel(s.Collection.Kind)
		elem := schemaTypeName(s.Collection.ElementType)
		return fmt.Sprintf("%s(%s)", kind, elem)
	}

	switch s.Type {
	case ir.TypeString:
		return "String"
	case ir.TypeInt:
		return "Number"
	case ir.TypeFloat:
		return "Number"
	case ir.TypeBool:
		return "Bool"
	case ir.TypeDynamic:
		return "Dynamic"
	}

	if schema.IsObjectLike(s) {
		return objectSchemaTypeName(s)
	}

	// An empty or otherwise unrepresentable primitive type renders as a
	// DynamicAttribute in the generated schema (resource.go falls back to
	// DynamicAttribute for shapes with no recognizable primitive/union/object
	// form), so the docs label matches that rather than the opaque "Unknown".
	return "Dynamic"
}

// objectSchemaTypeName returns a concise type label for an object-like schema,
// listing its attribute and block names. If the schema has no fields, it falls
// back to the bare "Object" label.
func objectSchemaTypeName(s ir.SchemaIR) string {
	if !schema.IsObjectLike(s) {
		return "Object"
	}
	names := make([]string, 0, len(s.Attributes)+len(s.Blocks))
	for _, attr := range s.Attributes {
		names = append(names, attr.Name)
	}
	for _, block := range s.Blocks {
		names = append(names, block.Name)
	}
	if len(names) == 0 {
		return "Object"
	}
	return fmt.Sprintf("Object({%s})", strings.Join(names, ", "))
}

// collectionKindLabel returns a capitalized label for a collection kind.
func collectionKindLabel(k ir.CollectionKind) string {
	switch k {
	case ir.List:
		return "List"
	case ir.Set:
		return "Set"
	case ir.Map:
		return "Map"
	default:
		return capitalize(string(k))
	}
}

// capitalize returns s with its first rune upper-cased and the rest unchanged.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// blockNestingLabel returns a human-readable nesting mode label for a block.
func blockNestingLabel(block ir.BlockIR) string {
	switch block.NestingMode {
	case ir.NestingList:
		return "List"
	case ir.NestingSet:
		return "Set"
	default:
		return "Single"
	}
}

// blockQualifier returns the requirement qualifier for a block.
func blockQualifier(block ir.BlockIR) string {
	if block.MinItems != nil && *block.MinItems > 0 {
		return "required"
	}
	return "optional"
}

// docsImportFormat returns an import ID format string for the import section.
// If no explicit format is configured, a default of ` <id>` is returned.
func docsImportFormat(format string) string {
	if strings.TrimSpace(format) == "" {
		return " <id>"
	}
	return " " + strings.TrimSpace(format)
}

// escapeDescription escapes characters that would break Terraform Registry
// frontmatter or markdown rendering. It trims whitespace, collapses newlines,
// and backslash-escapes markdown-special characters. The resulting value is
// safe to place inside the YAML `|-` frontmatter block used by the templates
// because it is a single line.
func escapeDescription(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	replacer := strings.NewReplacer(
		"#", "\\#",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"`", "\\`",
		"|", "\\|",
	)
	return replacer.Replace(s)
}
