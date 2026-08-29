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
		"DescriptionBody":    bodyDescription(pir.Description),
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
	arguments, attributes, blocks, nestedSchemas := renderDocsSchema(r.Schema)
	data := map[string]any{
		"ResourceName":    resourceDocsTypeName(r),
		"ProviderName":    providerDocsTypeNameFromResource(r),
		"Description":     escapeDescription(r.Description),
		"DescriptionBody": bodyDescription(r.Description),
		"ExampleArgs":     renderExampleArguments(r.Schema.Attributes),
		"Arguments":       arguments,
		"Attributes":      attributes,
		"Blocks":          blocks,
		"NestedSchemas":   nestedSchemas,
		"Importable":      r.Importable,
		"ImportFormat":    docsImportFormat(r.ImportIDFormat),
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
	arguments, attributes, blocks, nestedSchemas := renderDocsSchema(ds.Schema)
	data := map[string]any{
		"DataSourceName":  dataSourceDocsTypeName(ds),
		"ProviderName":    providerDocsTypeNameFromDataSource(ds),
		"Description":     escapeDescription(ds.Description),
		"DescriptionBody": bodyDescription(ds.Description),
		"ExampleArgs":     renderExampleArguments(ds.Schema.Attributes),
		"Arguments":       arguments,
		"Attributes":      attributes,
		"Blocks":          blocks,
		"NestedSchemas":   nestedSchemas,
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

{{.DescriptionBody}}

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

{{.DescriptionBody}}

## Example Usage

` + "```terraform" + `
resource "{{.ResourceName}}" "example" {
{{.ExampleArgs}}}
` + "```" + `
{{if or .Arguments .Attributes .Blocks}}
## Schema

{{if .Arguments -}}
### Arguments

The following arguments are supported:

{{.Arguments}}
{{end -}}
{{if .Attributes -}}
### Attributes

In addition to all arguments above, the following computed attributes are exported:

{{.Attributes}}
{{end -}}
{{if .Blocks -}}
### Nested Blocks

{{.Blocks}}
{{end -}}
{{.NestedSchemas}}
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

{{.DescriptionBody}}

## Example Usage

` + "```terraform" + `
data "{{.DataSourceName}}" "example" {
{{.ExampleArgs}}}
` + "```" + `
{{if or .Arguments .Attributes .Blocks}}
## Schema

{{if .Arguments -}}
### Arguments

The following arguments are supported:

{{.Arguments}}
{{end -}}
{{if .Attributes -}}
### Attributes

In addition to all arguments above, the following attributes are exported:

{{.Attributes}}
{{end -}}
{{if .Blocks -}}
### Nested Blocks

{{.Blocks}}
{{end -}}
{{.NestedSchemas}}
{{end -}}
`

// renderExampleArguments renders required and optional attributes as HCL-style
// example arguments, indented by two spaces. It delegates to writeHCLBody so the
// docs example matches the generated examples/resources/*.tf files: primitives
// get type-appropriate placeholders ("example", 0, true) and nested objects and
// collections are expanded into populated literals rather than null/empty
// placeholders. Assignment `=` signs are aligned the way `terraform fmt` aligns
// them, so the emitted example is fmt-clean.
func renderExampleArguments(attrs []ir.AttributeIR) string {
	var h hclBuilder
	h.indent = 1
	writeHCLBody(&h, ir.ObjectSchemaIR{Attributes: attrs})
	return h.b.String()
}

// renderDocsSchema renders the schema reference for a resource or data source
// object as tfplugindocs does: flat Arguments, Attributes, and Nested Blocks
// bullet lists (with nested-schema links) plus the collected
// "### Nested Schema for `path`" sections. A nested object attribute or block is
// listed once with a link to its own section instead of an inline indented
// expansion, so the registry's nested-schema anchors work and double-nested
// attributes are reachable by reference rather than indentation.
func renderDocsSchema(obj ir.ObjectSchemaIR) (arguments, attributes, blocks, nested string) {
	return renderDocsSections(obj.Attributes, obj.Attributes, obj.Blocks)
}

// renderDocsSections renders the schema bullet lists for an arbitrary
// Arguments/Attributes/Blocks combination (list resources draw Arguments and
// Attributes from different attribute sets) plus the collected nested-schema
// sections.
func renderDocsSections(argsAttrs, attrsAttrs []ir.AttributeIR, blocks []ir.BlockIR) (arguments, attributes, blockSection, nested string) {
	var argsB, attrsB, blocksB, nestedB strings.Builder
	nestedTypes := make([]docsNestedType, 0, len(argsAttrs)+len(attrsAttrs)+len(blocks))
	nestedTypes = append(nestedTypes, writeDocsAttributeRows(&argsB, argsAttrs, true, nil)...)
	nestedTypes = append(nestedTypes, writeDocsAttributeRows(&attrsB, attrsAttrs, false, nil)...)
	nestedTypes = append(nestedTypes, writeDocsBlockRows(&blocksB, blocks, nil)...)
	writeDocsNestedSections(&nestedB, nestedTypes)
	return argsB.String(), attrsB.String(), blocksB.String(), nestedB.String()
}

// docsNestedType is a nested schema (object attribute or block) collected while
// rendering a parent schema and emitted later as a tfplugindocs-style
// "### Nested Schema for `path`" section.
type docsNestedType struct {
	anchorID  string // "nestedatt--parent--child" or "nestedblock--name"
	pathTitle string // dot-joined Terraform attribute path, e.g. "owner.ships"
	path      []string
	object    *ir.ObjectSchemaIR
}

// writeDocsAttributeRows renders the schema bullet rows for a set of attributes
// filtered by section (arguments=true → Required/Optional; otherwise Computed)
// and returns the nested types to emit as nested-schema sections.
func writeDocsAttributeRows(b *strings.Builder, attrs []ir.AttributeIR, arguments bool, path []string) []docsNestedType {
	var nested []docsNestedType
	for _, attr := range attrs {
		qualifier := ""
		if arguments {
			switch {
			case attr.Required:
				qualifier = "required"
			case attr.Optional:
				qualifier = "optional"
			default:
				continue
			}
		} else {
			if !attr.Computed {
				continue
			}
			qualifier = "computed"
		}
		if nt, ok := writeDocsAttributeRow(b, attr, qualifier, path); ok {
			nested = append(nested, nt)
		}
	}
	return nested
}

// writeDocsAttributeRow writes a single schema bullet for an attribute. The
// qualifier is always shown because the Arguments/Attributes sections mix
// required, optional, and computed rows. Nested objects and collections of
// objects link to a nested-schema section (returned for rendering later) rather
// than being expanded inline.
func writeDocsAttributeRow(b *strings.Builder, attr ir.AttributeIR, qualifier string, path []string) (docsNestedType, bool) {
	fmt.Fprintf(b, "* `%s` (%s, %s)", attr.Name, docsTypeLabel(attr.Schema), qualifier)
	if attr.Description != "" {
		fmt.Fprintf(b, " - %s", escapeDescription(attr.Description))
	}
	if obj, ok := docsNestedObject(attr.Schema); ok {
		childPath := appendDocsPath(path, attr.Name)
		fmt.Fprintf(b, " (see [below for nested schema](#%s))", nestedDocsAnchor("nestedatt", childPath))
		fmt.Fprintln(b)
		return docsNestedType{
			anchorID:  nestedDocsAnchor("nestedatt", childPath),
			pathTitle: strings.Join(childPath, "."),
			path:      childPath,
			object:    &obj,
		}, true
	}
	fmt.Fprintln(b)
	return docsNestedType{}, false
}

// writeDocsBlockRows renders the schema bullets for a set of nested blocks and
// returns the nested types collected for section rendering.
func writeDocsBlockRows(b *strings.Builder, blocks []ir.BlockIR, path []string) []docsNestedType {
	nested := make([]docsNestedType, 0, len(blocks))
	for _, block := range blocks {
		fmt.Fprintf(b, "* `%s` (Block %s, %s)", block.Name, blockNestingLabel(block), blockQualifier(block))
		if block.Description != "" {
			fmt.Fprintf(b, " - %s", escapeDescription(block.Description))
		}
		childPath := appendDocsPath(path, block.Name)
		fmt.Fprintf(b, " (see [below for nested schema](#%s))", nestedDocsAnchor("nestedblock", childPath))
		fmt.Fprintln(b)
		bs := block.Schema
		nested = append(nested, docsNestedType{
			anchorID:  nestedDocsAnchor("nestedblock", childPath),
			pathTitle: strings.Join(childPath, "."),
			path:      childPath,
			object:    &bs,
		})
	}
	return nested
}

// writeDocsNestedSections renders the nested-schema sections for a set of
// nested types in document order, skipping types already emitted (an attribute
// that is both optional and computed appears in both the Arguments and
// Attributes sections but must produce only one section).
func writeDocsNestedSections(b *strings.Builder, types []docsNestedType) {
	seen := map[string]bool{}
	for _, nt := range types {
		if seen[nt.anchorID] {
			continue
		}
		seen[nt.anchorID] = true
		writeDocsNestedSection(b, nt)
	}
}

// writeDocsNestedSection renders one "### Nested Schema for `path`" section:
// children grouped under Required:/Optional:/Read-Only: subtitles in the
// tfplugindocs style, with any deeper nested schemas emitted as their own
// sections after. Deeper sections are deduplicated by writeDocsNestedSections,
// so this function does not need to track the visited set.
func writeDocsNestedSection(b *strings.Builder, nt docsNestedType) {
	fmt.Fprintf(b, "<a id=\"%s\"></a>\n", nt.anchorID)
	fmt.Fprintf(b, "### Nested Schema for `%s`\n\n", nt.pathTitle)

	var deeper []docsNestedType
	obj := nt.object
	for _, group := range []string{"Required", "Optional", "Read-Only"} {
		var attrs []ir.AttributeIR
		var blocks []ir.BlockIR
		for _, attr := range obj.Attributes {
			if docsChildQualifier(attr) == group {
				attrs = append(attrs, attr)
			}
		}
		for _, block := range obj.Blocks {
			if docsBlockGroup(block) == group {
				blocks = append(blocks, block)
			}
		}
		if len(attrs) == 0 && len(blocks) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s:\n\n", group)
		for _, attr := range attrs {
			childPath := appendDocsPath(nt.path, attr.Name)
			child, ok := writeDocsNestedChildAttr(b, attr, childPath)
			if ok {
				deeper = append(deeper, child)
			}
		}
		for _, block := range blocks {
			childPath := appendDocsPath(nt.path, block.Name)
			fmt.Fprintf(b, "* `%s` (Block %s)", block.Name, blockNestingLabel(block))
			if block.Description != "" {
				fmt.Fprintf(b, " - %s", escapeDescription(block.Description))
			}
			fmt.Fprintf(b, " (see [below for nested schema](#%s))", nestedDocsAnchor("nestedblock", childPath))
			fmt.Fprintln(b)
			bs := block.Schema
			deeper = append(deeper, docsNestedType{
				anchorID:  nestedDocsAnchor("nestedblock", childPath),
				pathTitle: strings.Join(childPath, "."),
				path:      childPath,
				object:    &bs,
			})
		}
	}
	writeDocsNestedSections(b, deeper)
}

// writeDocsNestedChildAttr writes a nested-schema child attribute bullet. The
// qualifier is implied by the Required:/Optional:/Read-Only: group header, so it
// is not repeated inline; the description and any deeper nested-schema link are.
func writeDocsNestedChildAttr(b *strings.Builder, attr ir.AttributeIR, childPath []string) (docsNestedType, bool) {
	fmt.Fprintf(b, "* `%s` (%s)", attr.Name, docsTypeLabel(attr.Schema))
	if attr.Description != "" {
		fmt.Fprintf(b, " - %s", escapeDescription(attr.Description))
	}
	if obj, ok := docsNestedObject(attr.Schema); ok {
		fmt.Fprintf(b, " (see [below for nested schema](#%s))", nestedDocsAnchor("nestedatt", childPath))
		fmt.Fprintln(b)
		return docsNestedType{
			anchorID:  nestedDocsAnchor("nestedatt", childPath),
			pathTitle: strings.Join(childPath, "."),
			path:      childPath,
			object:    &obj,
		}, true
	}
	fmt.Fprintln(b)
	return docsNestedType{}, false
}

// docsChildQualifier returns the tfplugindocs child-group name for an attribute
// (Required/Optional/Read-Only), by Required > Optional > Computed priority.
func docsChildQualifier(attr ir.AttributeIR) string {
	if attr.Required {
		return "Required"
	}
	if attr.Optional {
		return "Optional"
	}
	return "Read-Only"
}

// docsBlockGroup returns the tfplugindocs child-group name for a block.
func docsBlockGroup(block ir.BlockIR) string {
	if blockQualifier(block) == "required" {
		return "Required"
	}
	return "Optional"
}

// docsNestedObject returns the object schema a nested object attribute or
// collection-of-objects attribute describes, or whether the schema nests at
// all. A discriminated union nests through its merged attribute set.
func docsNestedObject(s ir.SchemaIR) (ir.ObjectSchemaIR, bool) {
	if s.Collection != nil {
		elem := s.Collection.ElementType
		if schema.IsObjectLike(elem) {
			return ir.ObjectSchemaIR{Attributes: elem.Attributes, Blocks: elem.Blocks}, true
		}
		return ir.ObjectSchemaIR{}, false
	}
	if schema.IsObjectLike(s) {
		return ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, true
	}
	if merged := schema.MergedDiscriminatedUnion(s); merged != nil {
		return ir.ObjectSchemaIR{Attributes: merged.Attributes, Blocks: merged.Blocks}, true
	}
	return ir.ObjectSchemaIR{}, false
}

// docsTypeLabel returns the tfplugindocs-style type label for a schema: a
// primitive ("String", "Number", "Boolean", "Dynamic"), a collection of
// primitives ("List of String"), or a nested-object form ("Attributes",
// "Attributes List", "Attributes Set", "Attributes Map").
func docsTypeLabel(s ir.SchemaIR) string {
	if s.Collection != nil {
		if schema.IsObjectLike(s.Collection.ElementType) {
			return "Attributes " + collectionKindLabel(s.Collection.Kind)
		}
		return schemaTypeName(s)
	}
	if schema.IsObjectLike(s) {
		return "Attributes"
	}
	if s.Union != nil {
		if schema.MergedDiscriminatedUnion(s) != nil {
			return "Attributes"
		}
		return "Dynamic"
	}
	return schemaTypeName(s)
}

// appendDocsPath returns path with name appended, leaving path unchanged.
func appendDocsPath(path []string, name string) []string {
	out := make([]string, len(path), len(path)+1)
	copy(out, path)
	return append(out, name)
}

// nestedDocsAnchor builds a tfplugindocs anchor id from a path of attribute
// names, e.g. nestedDocsAnchor("nestedatt", ["owner","ships"]) →
// "nestedatt--owner--ships".
func nestedDocsAnchor(kind string, path []string) string {
	return kind + "--" + strings.Join(path, "--")
}

// schemaTypeName returns a human-readable Terraform type name for an IR schema
// in the tfplugindocs vocabulary: "String", "Number", "Boolean", "Dynamic",
// "List of String", "Object". Unrepresentable shapes render as "Dynamic",
// matching the DynamicAttribute the generator degrades to rather than the
// opaque "Unknown".
func schemaTypeName(s ir.SchemaIR) string {
	if s.Collection != nil {
		kind := collectionKindLabel(s.Collection.Kind)
		return fmt.Sprintf("%s of %s", kind, schemaTypeName(s.Collection.ElementType))
	}

	switch s.Type {
	case ir.TypeString:
		return "String"
	case ir.TypeInt, ir.TypeFloat:
		return "Number"
	case ir.TypeBool:
		return "Boolean"
	case ir.TypeDynamic:
		return "Dynamic"
	}

	if schema.IsObjectLike(s) {
		return "Object"
	}

	return "Dynamic"
}

// bodyDescription returns a description rendered as Markdown body text: line
// breaks and markdown syntax are preserved so multi-paragraph or markdown-rich
// provider/resource descriptions (headings, links, code fences) render as
// intended on the Registry. It is used for the docs body, while
// escapeDescription (single-line, escaped) remains for the YAML frontmatter and
// inline attribute bullets.
func bodyDescription(s string) string {
	s = strings.TrimSpace(s)
	return strings.ReplaceAll(s, "\r\n", "\n")
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
