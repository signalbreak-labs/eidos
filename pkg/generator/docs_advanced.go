package generator

import (
	"fmt"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ActionDocsFile returns the generated docs/actions/<name>.md file for a single
// Terraform action.
func ActionDocsFile(a ir.ActionIR) File {
	path := fmt.Sprintf("docs/actions/%s.md", naming.SnakeCase(a.Name))
	arguments, _, blocks, nested := renderDocsSections(a.ConfigSchema.Attributes, a.ConfigSchema.Attributes, a.ConfigSchema.Blocks)
	data := map[string]any{
		"ActionName":      actionDocsTypeName(a),
		"ProviderName":    providerDocsTypeNameFromAction(a),
		"Description":     escapeDescription(a.Description),
		"DescriptionBody": bodyDescription(a.Description),
		"ExampleHCL":      generateActionExampleHCL(a),
		"Arguments":       arguments,
		"Blocks":          blocks,
		"NestedSchemas":   nested,
	}
	return Template(path, actionTemplate, data)
}

// ActionDocsFiles returns the generated action documentation files for all
// supplied ActionIR values, preserving order.
func ActionDocsFiles(actions []ir.ActionIR) []File {
	files := make([]File, 0, len(actions))
	for _, a := range actions {
		files = append(files, ActionDocsFile(a))
	}
	return files
}

// EphemeralResourceDocsFile returns the generated
// docs/ephemeral-resources/<name>.md file for a single Terraform ephemeral
// resource.
func EphemeralResourceDocsFile(er ir.EphemeralResourceIR) File {
	path := fmt.Sprintf("docs/ephemeral-resources/%s.md", naming.SnakeCase(er.Name))
	merged := ephemeralMergedAttributes(er)
	mergedBlocks := ephemeralMergedBlocks(er)
	arguments, attributes, blocks, nested := renderDocsSections(merged, merged, mergedBlocks)
	data := map[string]any{
		"EphemeralResourceName": ephemeralResourceDocsTypeName(er),
		"ProviderName":          providerDocsTypeNameFromEphemeralResource(er),
		"Description":           escapeDescription(er.Description),
		"DescriptionBody":       bodyDescription(er.Description),
		"ExampleHCL":            generateEphemeralResourceExampleHCL(er),
		"Arguments":             arguments,
		"Attributes":            attributes,
		"Blocks":                blocks,
		"NestedSchemas":         nested,
	}
	return Template(path, ephemeralResourceTemplate, data)
}

// EphemeralResourceDocsFiles returns the generated ephemeral resource
// documentation files for all supplied EphemeralResourceIR values, preserving
// order.
func EphemeralResourceDocsFiles(ers []ir.EphemeralResourceIR) []File {
	files := make([]File, 0, len(ers))
	for _, er := range ers {
		files = append(files, EphemeralResourceDocsFile(er))
	}
	return files
}

// ListResourceDocsFile returns the generated docs/list-resources/<name>.md file
// for a single Terraform list resource.
func ListResourceDocsFile(lr ir.ListResourceIR) File {
	path := fmt.Sprintf("docs/list-resources/%s.md", naming.SnakeCase(lr.Name))
	arguments, attributes, blocks, nested := renderDocsSections(lr.ConfigSchema.Attributes, lr.IdentitySchema.Attributes, lr.ConfigSchema.Blocks)
	data := map[string]any{
		"ListResourceName": listResourceDocsTypeName(lr),
		"ProviderName":     providerDocsTypeNameFromListResource(lr),
		"Description":      escapeDescription(lr.Description),
		"DescriptionBody":  bodyDescription(lr.Description),
		"ExampleHCL":       generateListResourceExampleHCL(lr),
		"Arguments":        arguments,
		"Attributes":       attributes,
		"Blocks":           blocks,
		"NestedSchemas":    nested,
	}
	return Template(path, listResourceTemplate, data)
}

// ListResourceDocsFiles returns the generated list resource documentation files
// for all supplied ListResourceIR values, preserving order. List resources the
// provider cannot register (no paired managed resource, so terraform query never
// exposes them) get no docs: a documented construct the provider cannot serve
// is actively misleading.
func ListResourceDocsFiles(lrs []ir.ListResourceIR) []File {
	files := make([]File, 0, len(lrs))
	for _, lr := range lrs {
		if !lr.Registerable {
			continue
		}
		files = append(files, ListResourceDocsFile(lr))
	}
	return files
}

// FunctionDocsFile returns the generated docs/functions/<name>.md file for a
// single provider-defined function. providerName is the provider type (e.g.
// "mycloud") and is used for the example invocation because function type
// names are not required to include a provider prefix.
func FunctionDocsFile(fn ir.FunctionIR, providerName string) File {
	path := fmt.Sprintf("docs/functions/%s.md", naming.SnakeCase(fn.Name))
	data := map[string]any{
		"FunctionName":    functionDocsTypeName(fn),
		"ProviderName":    strings.TrimSpace(providerName),
		"Description":     escapeDescription(fn.Description),
		"DescriptionBody": bodyDescription(fn.Description),
		"ExampleHCL":      generateFunctionExampleHCL(fn, providerName),
		"SignatureArgs":   renderFunctionSignatureArgs(fn),
		"ArgumentDetails": renderFunctionParameters(fn),
		"ReturnType":      schemaTypeName(fn.ReturnType),
		"Variadic":        fn.Variadic,
	}
	return Template(path, functionTemplate, data)
}

// FunctionDocsFiles returns the generated function documentation files for all
// supplied FunctionIR values, preserving order.
func FunctionDocsFiles(functions []ir.FunctionIR, providerName string) []File {
	files := make([]File, 0, len(functions))
	for _, fn := range functions {
		files = append(files, FunctionDocsFile(fn, providerName))
	}
	return files
}

// docsActionRef, docsEphemeralResourceRef, docsListResourceRef, and
// docsFunctionRef are precomputed link references used by the index template.
type docsActionRef struct {
	TypeName string
	FileName string
}

type docsEphemeralResourceRef struct {
	TypeName string
	FileName string
}

type docsListResourceRef struct {
	TypeName string
	FileName string
}

type docsFunctionRef struct {
	TypeName string
	FileName string
}

// actionDocsTypeName returns the Terraform action type name for docs.
func actionDocsTypeName(a ir.ActionIR) string {
	return docsTypeNameOrFallback(a.TypeName, a.Name, naming.SnakeCase)
}

// providerDocsTypeNameFromAction returns the provider portion of an action type
// name. It assumes the type name follows the <provider>_<resource> convention.
func providerDocsTypeNameFromAction(a ir.ActionIR) string {
	return providerDocsPrefix(actionDocsTypeName(a))
}

// ephemeralResourceDocsTypeName returns the Terraform ephemeral resource type
// name for docs.
func ephemeralResourceDocsTypeName(er ir.EphemeralResourceIR) string {
	return docsTypeNameOrFallback(er.TypeName, er.Name, naming.SnakeCase)
}

// providerDocsTypeNameFromEphemeralResource returns the provider portion of an
// ephemeral resource type name. It assumes the type name follows the
// <provider>_<resource> convention.
func providerDocsTypeNameFromEphemeralResource(er ir.EphemeralResourceIR) string {
	return providerDocsPrefix(ephemeralResourceDocsTypeName(er))
}

// listResourceDocsTypeName returns the Terraform list resource type name for
// docs.
func listResourceDocsTypeName(lr ir.ListResourceIR) string {
	return docsTypeNameOrFallback(lr.TypeName, lr.Name, naming.SnakeCase)
}

// providerDocsTypeNameFromListResource returns the provider portion of a list
// resource type name. It assumes the type name follows the
// <provider>_<resource> convention.
func providerDocsTypeNameFromListResource(lr ir.ListResourceIR) string {
	return providerDocsPrefix(listResourceDocsTypeName(lr))
}

// functionDocsTypeName returns the Terraform function name for docs.
func functionDocsTypeName(fn ir.FunctionIR) string {
	return docsTypeNameOrFallback(fn.TypeName, fn.Name, naming.SnakeCase)
}

// actionTemplate is the Terraform Registry-compatible frontmatter and body for
// a single docs/actions/<name>.md file.
const actionTemplate = `---
page_title: "{{.ActionName}} Action - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.ActionName}} Action

{{.DescriptionBody}}

## Example Usage

` + "```terraform" + `
{{.ExampleHCL}}
` + "```" + `
{{- if or .Arguments .Blocks}}
## Schema
{{- if .Arguments}}

### Arguments

The following arguments are supported:

{{.Arguments}}
{{- end}}
{{- if .Blocks}}

### Nested Blocks

{{.Blocks}}
{{- end}}
{{.NestedSchemas}}
{{- end}}
`

// ephemeralResourceTemplate is the Terraform Registry-compatible frontmatter
// and body for a single docs/ephemeral-resources/<name>.md file.
const ephemeralResourceTemplate = `---
page_title: "{{.EphemeralResourceName}} Ephemeral Resource - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.EphemeralResourceName}} Ephemeral Resource

{{.DescriptionBody}}

~> **Note:** Ephemeral resources are only available within the context of a single Terraform operation and are never persisted to state or plan files.

## Example Usage

` + "```terraform" + `
{{.ExampleHCL}}
` + "```" + `
{{- if or .Arguments .Attributes .Blocks}}
## Schema
{{- if .Arguments}}

### Arguments

The following arguments are supported:

{{.Arguments}}
{{- end}}
{{- if .Attributes}}

### Attributes

In addition to all arguments above, the following computed attributes are exported:

{{.Attributes}}
{{- end}}
{{- if .Blocks}}

### Nested Blocks

{{.Blocks}}
{{- end}}
{{.NestedSchemas}}
{{- end}}
`

// listResourceTemplate is the Terraform Registry-compatible frontmatter and
// body for a single docs/list-resources/<name>.md file.
const listResourceTemplate = `---
page_title: "{{.ListResourceName}} List Resource - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.ListResourceName}} List Resource

{{.DescriptionBody}}

## Example Usage

` + "```terraform" + `
{{.ExampleHCL}}
` + "```" + `
{{- if or .Arguments .Attributes .Blocks}}
## Schema
{{- if .Arguments}}

### Arguments

The following arguments are supported:

{{.Arguments}}
{{- end}}
{{- if .Attributes}}

### Identity Attributes

The following identity attributes are exported for each matching result:

{{.Attributes}}
{{- end}}
{{- if .Blocks}}

### Nested Blocks

{{.Blocks}}
{{- end}}
{{.NestedSchemas}}
{{- end}}
`

// functionTemplate is the Terraform Registry-compatible frontmatter and body
// for a single docs/functions/<name>.md file.
const functionTemplate = `---
page_title: "{{.FunctionName}} Function - {{.ProviderName}}"
subcategory: ""
description: |-
  {{.Description}}
---

# {{.FunctionName}} Function

{{.DescriptionBody}}

## Example Usage

` + "```terraform" + `
{{.ExampleHCL}}
` + "```" + `

## Signature

` + "```text" + `
{{.FunctionName}}({{.SignatureArgs}}) -> {{.ReturnType}}
` + "```" + `

## Arguments

The following arguments are supported:

{{.ArgumentDetails}}

## Return

The function returns a value of type ` + "`{{.ReturnType}}`" + `.
`

// generateListResourceExampleHCL builds a placeholder HCL example for a list
// resource. List resources are queried via `list` blocks in .tfquery.hcl files
// (see the Terraform `list` block reference), not a top-level `query` block;
// the previous `query "<type>" "example" { ... }` form was invalid HCL that
// Terraform would reject (M-17). The provider-specific config arguments live
// in a nested `config` block.
func generateListResourceExampleHCL(lr ir.ListResourceIR) string {
	var h hclBuilder
	providerName := providerDocsTypeNameFromListResource(lr)
	h.writeLinef(`list "%s" "example" {`, listResourceDocsTypeName(lr))
	h.indent++
	// provider and limit are consecutive single-line assignments; emit them with
	// aligned `=` signs so the example is terraform-fmt-clean.
	rows := &hclRows{h: &h}
	rows.add("provider", providerName)
	rows.add("limit", "100")
	rows.flush()
	if len(lr.ConfigSchema.Attributes) > 0 || len(lr.ConfigSchema.Blocks) > 0 {
		h.writeLinef("config {")
		h.indent++
		writeHCLBody(&h, lr.ConfigSchema)
		h.indent--
		h.writeLinef("}")
	}
	h.indent--
	h.writeLinef("}")
	return h.b.String()
}

// generateFunctionExampleHCL builds a placeholder HCL example for a provider-
// defined function invocation.
func generateFunctionExampleHCL(fn ir.FunctionIR, providerName string) string {
	var b strings.Builder
	args := functionExampleArgs(fn)
	fmt.Fprintf(&b, "# Example: provider::%s::%s(%s)\n", providerName, functionDocsTypeName(fn), args)
	b.WriteString("output \"example\" {\n")
	fmt.Fprintf(&b, "  value = provider::%s::%s(%s)\n", providerName, functionDocsTypeName(fn), args)
	b.WriteString("}")
	return b.String()
}

// functionExampleArgs returns a comma-separated placeholder argument list for a
// function example. Top-level string arguments are rendered as angle-bracket
// placeholders (e.g. "<separator>") so operators can see they must be
// substituted. Object-like and collection arguments are expanded to show their
// structure rather than being collapsed to `null`.
func functionExampleArgs(fn ir.FunctionIR) string {
	parts := make([]string, 0, len(fn.Arguments))
	for _, arg := range fn.Arguments {
		parts = append(parts, functionArgumentPlaceholder(arg.Schema, arg.Name))
	}
	return strings.Join(parts, ", ")
}

// functionArgumentPlaceholder returns a placeholder literal for a single
// function argument schema. The label is used for string placeholders so the
// example shows which value to substitute; for nested attributes, the attribute
// name is used as the label. Object-like schemas are expanded into HCL object
// literals so List/Set(Map) examples are syntactically valid.
func functionArgumentPlaceholder(s ir.SchemaIR, label string) string {
	if s.Collection != nil {
		return collectionArgumentPlaceholder(s.Collection, label)
	}
	if schema.IsObjectLike(s) {
		return objectArgumentPlaceholder(s, label)
	}
	if s.Type == ir.TypeDynamic {
		return `"example"`
	}
	if s.Type == ir.TypeString {
		return fmt.Sprintf(`"<%s>"`, label)
	}
	return primitiveExampleValue(s.Type)
}

// collectionArgumentPlaceholder returns a placeholder literal for a collection
// schema. Map elements always use a "key" entry; object-like elements are
// expanded rather than collapsed to null.
func collectionArgumentPlaceholder(c *ir.CollectionType, label string) string {
	elem := c.ElementType
	switch c.Kind {
	case ir.Map:
		return `{ "key" = ` + functionArgumentPlaceholder(elem, label) + ` }`
	default:
		return `[ ` + functionArgumentPlaceholder(elem, label) + ` ]`
	}
}

// objectArgumentPlaceholder returns an HCL object literal placeholder for an
// object-like schema, with one field per attribute and block. Labels are passed
// down so string fields are rendered as angle-bracket placeholders.
func objectArgumentPlaceholder(s ir.SchemaIR, label string) string {
	if !schema.IsObjectLike(s) {
		return "null"
	}
	var b strings.Builder
	b.WriteString("{ ")
	first := true
	for _, attr := range s.Attributes {
		if !first {
			b.WriteString(", ")
		}
		first = false
		attrLabel := attr.Name
		if attrLabel == "" {
			attrLabel = label
		}
		fmt.Fprintf(&b, "%s = %s", attr.Name, functionArgumentPlaceholder(attr.Schema, attrLabel))
	}
	for _, block := range s.Blocks {
		if !first {
			b.WriteString(", ")
		}
		first = false
		blockSchema := ir.SchemaIR{
			Attributes: block.Schema.Attributes,
			Blocks:     block.Schema.Blocks,
		}
		fmt.Fprintf(&b, "%s = %s", block.Name, objectArgumentPlaceholder(blockSchema, label))
	}
	b.WriteString(" }")
	return b.String()
}

// renderFunctionSignatureArgs renders a comma-separated argument list suitable
// for the function signature block. Each argument is shown as `name: Type`; the
// last argument is annotated with `...` when the function is variadic.
func renderFunctionSignatureArgs(fn ir.FunctionIR) string {
	parts := make([]string, 0, len(fn.Arguments))
	for i, arg := range fn.Arguments {
		name := arg.Name
		if fn.Variadic && i == len(fn.Arguments)-1 {
			name += "..."
		}
		parts = append(parts, fmt.Sprintf("%s: %s", name, schemaTypeName(arg.Schema)))
	}
	return strings.Join(parts, ", ")
}

// renderFunctionParameters renders the function signature and argument detail
// section for a function. Each parameter is listed with its name, type, and
// optional description. Variadic parameters are annotated with `...`.
func renderFunctionParameters(fn ir.FunctionIR) string {
	var b strings.Builder
	for i, arg := range fn.Arguments {
		name := arg.Name
		if fn.Variadic && i == len(fn.Arguments)-1 {
			name += "..."
		}
		fmt.Fprintf(&b, "* `%s` (%s)", name, schemaTypeName(arg.Schema))
		// Prefer MarkdownDescription over Description, matching the precedence
		// used everywhere else in the generator (e.g. ephemeralAttributeValues).
		// The prior order preferred plain Description (L-34).
		if arg.MarkdownDescription != "" {
			fmt.Fprintf(&b, " - %s", escapeDescription(arg.MarkdownDescription))
		} else if arg.Description != "" {
			fmt.Fprintf(&b, " - %s", escapeDescription(arg.Description))
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}
