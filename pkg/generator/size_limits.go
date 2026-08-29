package generator

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// Terraform platform size limits (G39). The Terraform CLI caps a
// GetProviderSchema gRPC response at 64 MiB; the estimate below targets a
// smaller error threshold so a provider that passes generation also passes
// init with headroom for the serialization overhead the estimate cannot see
// (framework attribute metadata, validators, msgpack maps).
const (
	// SchemaSizeFailDefault is the default hard cap on the estimated
	// serialized provider schema size (bytes).
	SchemaSizeFailDefault = 60 << 20
	// DocsFileLimitDefault is the default per-docs-file byte cap. The Terraform
	// Registry truncates any document over 500KB with only a viewer-facing
	// note, silently losing the tail of the schema documentation.
	DocsFileLimitDefault = 500000
)

// attributeOverheadBytes is the fixed per-attribute serialization overhead the
// name/flag/type estimation cannot see (attribute type marker, flags, nesting
// envelope). Tuned so the estimate errs high rather than low.
const attributeOverheadBytes = 128

// perPrimitiveOverheadBytes is the fixed per-node overhead for a standalone
// schema node (type discriminator plus constraint fields).
const perPrimitiveOverheadBytes = 32

// EffectiveSchemaLimits resolves the configured limits against the defaults:
// a nil limits uses the built-in default thresholds; a negative MaxSchemaBytes
// disables the schema check; a zero MaxSchemaBytes restores the default.
func EffectiveSchemaLimits(limits *config.LimitsConfig) (warn, fail int) {
	if limits != nil && limits.MaxSchemaBytes < 0 {
		return 0, 0
	}
	fail = SchemaSizeFailDefault
	if limits != nil && limits.MaxSchemaBytes > 0 {
		fail = limits.MaxSchemaBytes
	}
	warn = fail * 4 / 5
	if limits != nil && limits.WarnSchemaBytes > 0 {
		warn = limits.WarnSchemaBytes
	}
	return warn, fail
}

// EffectiveDocsFileLimit resolves the per-docs-file byte cap. Zero (or a nil
// config) uses the Registry's 500KB default; negative disables the check.
func EffectiveDocsFileLimit(limits *config.LimitsConfig) int {
	if limits != nil {
		if limits.MaxDocsFileBytes < 0 {
			return 0
		}
		if limits.MaxDocsFileBytes > 0 {
			return limits.MaxDocsFileBytes
		}
	}
	return DocsFileLimitDefault
}

// EffectiveDescriptionLimit resolves the description truncation length in
// bytes. Zero and negative both mean "do not truncate" (truncation is
// opt-in, since descriptions carry real spec content).
func EffectiveDescriptionLimit(limits *config.LimitsConfig) int {
	if limits == nil || limits.MaxDescriptionBytes <= 0 {
		return 0
	}
	return limits.MaxDescriptionBytes
}

// CheckProviderSchemaSize estimates the serialized size of the provider
// schema (every construct's schema plus the provider's own configuration
// schema) and reports diagnostics against the Terraform platform limit: a
// Warning once the estimate crosses the warning threshold, an Error once it
// crosses the hard cap (generation must then be reined in — see skip_operations
// and limits.max_description_bytes). The estimate is deliberately
// conservative (it over-counts) so a passing estimate is trustworthy.
func CheckProviderSchemaSize(p *ir.ProviderIR, limits *config.LimitsConfig) diagnostics.Diagnostics {
	warn, fail := EffectiveSchemaLimits(limits)
	if fail <= 0 {
		return nil
	}
	estimate := EstimateProviderSchemaBytes(p)
	var diags diagnostics.Diagnostics
	if estimate >= int64(fail) {
		diags = append(diags, diagnostics.Diagnostic{
			Severity: diagnostics.Error,
			Summary:  "Provider schema exceeds the Terraform size limit",
			Detail: fmt.Sprintf("Estimated serialized provider schema size is %s, at or above the %s cap. "+
				"The Terraform CLI refuses GetProviderSchema responses over 64 MiB, so this provider would fail at `terraform init`. "+
				"Cut keys or resources (skip_operations, generation.skip_*, resource_overrides with skip: true), or set limits.max_description_bytes to trim descriptions.",
				humanBytes(estimate), humanBytes(int64(fail))),
		})
		return diags
	}
	if warn > 0 && estimate >= int64(warn) {
		diags = append(diags, diagnostics.Diagnostic{
			Severity: diagnostics.Warning,
			Summary:  "Provider schema is approaching the Terraform size limit",
			Detail: fmt.Sprintf("Estimated serialized provider schema size is %s (warning threshold %s, hard cap %s). "+
				"The largest constructs: %s. Trim now or generation will fail once the estimate crosses the cap.",
				humanBytes(estimate), humanBytes(int64(warn)), humanBytes(int64(fail)),
				strings.Join(largestConstructs(p, 5), ", ")),
		})
	}
	return diags
}

// EstimateProviderSchemaBytes estimates the size of the provider's serialized
// schema (msgpack, as served by GetProviderSchema). It walks every construct
// the provider registers and sums a conservative per-attribute overhead plus
// the byte length of every string the framework carries (names, descriptions,
// enum values, patterns). It is an estimate, not an exact serialization: the
// goal is to catch providers that cannot pass `terraform init` before they are
// generated, erring high rather than low.
func EstimateProviderSchemaBytes(p *ir.ProviderIR) int64 {
	if p == nil {
		return 0
	}
	total := estimateObjectSchema(p.ConfigSchema)
	for i := range p.Resources {
		r := &p.Resources[i]
		total += estimateObjectSchema(r.Schema) + int64(len(r.TypeName))
		if r.IdentitySchema != nil {
			total += estimateObjectSchema(*r.IdentitySchema)
		}
	}
	for i := range p.DataSources {
		total += estimateObjectSchema(p.DataSources[i].Schema) + int64(len(p.DataSources[i].TypeName))
	}
	for i := range p.Actions {
		total += estimateObjectSchema(p.Actions[i].ConfigSchema) + int64(len(p.Actions[i].TypeName))
	}
	for i := range p.EphemeralResources {
		e := &p.EphemeralResources[i]
		total += estimateObjectSchema(e.ConfigSchema) + estimateObjectSchema(e.ResultSchema) + int64(len(e.TypeName))
	}
	for i := range p.ListResources {
		l := &p.ListResources[i]
		total += estimateObjectSchema(l.ConfigSchema) + estimateObjectSchema(l.IdentitySchema)
		if l.ResourceSchema != nil {
			total += estimateObjectSchema(*l.ResourceSchema)
		}
		total += int64(len(l.TypeName))
	}
	for i := range p.Functions {
		f := &p.Functions[i]
		for _, arg := range f.Arguments {
			total += estimateAttribute(arg)
		}
		total += estimateSchema(f.ReturnType) + int64(len(f.Name))
	}
	return total
}

func estimateObjectSchema(o ir.ObjectSchemaIR) int64 {
	total := int64(0)
	for _, a := range o.Attributes {
		total += estimateAttribute(a)
	}
	for _, b := range o.Blocks {
		total += int64(len(b.Name)) + int64(len(b.Description)) + attributeOverheadBytes + estimateObjectSchema(b.Schema)
	}
	return total
}

func estimateAttribute(a ir.AttributeIR) int64 {
	total := int64(len(a.Name)) + int64(len(a.WireName)) + int64(len(a.Description)) +
		int64(len(a.MarkdownDescription)) + int64(len(a.DeprecationMessage)) + attributeOverheadBytes
	total += estimateSchema(a.Schema)
	return total
}

func estimateSchema(s ir.SchemaIR) int64 {
	total := int64(len(s.Name)) + int64(len(s.Description)) + int64(len(s.Type)) +
		int64(len(s.Format)) + int64(len(s.Pattern)) + int64(len(s.DeprecationMessage)) + perPrimitiveOverheadBytes
	for _, v := range s.EnumValues {
		total += int64(len(fmt.Sprint(v))) + 8
	}
	if s.Default != nil {
		total += int64(len(fmt.Sprint(*s.Default))) + 8
	}
	if s.Collection != nil {
		total += estimateSchema(s.Collection.ElementType) + perPrimitiveOverheadBytes
	}
	if s.Union != nil {
		for _, v := range s.Union.Variants {
			total += estimateSchema(v)
		}
	}
	for _, a := range s.Attributes {
		total += estimateAttribute(a)
	}
	for _, b := range s.Blocks {
		total += int64(len(b.Name)) + int64(len(b.Description)) + attributeOverheadBytes + estimateObjectSchema(b.Schema)
	}
	for _, sub := range []*ir.SchemaIR{s.Not, s.IfSchema, s.ThenSchema, s.ElseSchema, s.PropertyNames, s.UnevaluatedProperties} {
		if sub != nil {
			total += estimateSchema(*sub)
		}
	}
	for _, sub := range s.DependentSchemas {
		if sub != nil {
			total += estimateSchema(*sub)
		}
	}
	for _, sub := range s.PatternProperties {
		if sub != nil {
			total += estimateSchema(*sub)
		}
	}
	return total
}

// largestConstructs names the constructs with the biggest estimated schemas,
// largest first, capped at n entries, so a size warning points at what to cut.
func largestConstructs(p *ir.ProviderIR, n int) []string {
	type sized struct {
		name  string
		bytes int64
	}
	all := make([]sized, 0, len(p.Resources)+len(p.DataSources))
	for i := range p.Resources {
		all = append(all, sized{p.Resources[i].TypeName, estimateObjectSchema(p.Resources[i].Schema)})
	}
	for i := range p.DataSources {
		all = append(all, sized{p.DataSources[i].TypeName, estimateObjectSchema(p.DataSources[i].Schema)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].bytes > all[j].bytes })
	out := make([]string, 0, n)
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, fmt.Sprintf("%s (%s)", all[i].name, humanBytes(all[i].bytes)))
	}
	return out
}

// ApplyDescriptionLimit truncates every description carried by the provider IR
// (attributes, blocks, and standalone schema nodes, at every nesting level)
// to at most max bytes, on a UTF-8 rune boundary, appending an ellipsis when
// it cuts. It mutates the provider in place and returns the number of
// descriptions truncated. max <= 0 is a no-op (truncation is opt-in).
//
// Long descriptions are the dominant driver of both schema size and docs
// size, so this is the primary lever for fitting a large spec under the
// Terraform platform limits (G39).
func ApplyDescriptionLimit(p *ir.ProviderIR, maxBytes int) int {
	if p == nil || maxBytes <= 0 {
		return 0
	}
	truncated := 0
	trunc := func(s *string) {
		if truncateDescription(s, maxBytes) {
			truncated++
		}
	}
	for i := range p.Resources {
		r := &p.Resources[i]
		traverseObjectSchema(&r.Schema, trunc)
		if r.IdentitySchema != nil {
			traverseObjectSchema(r.IdentitySchema, trunc)
		}
	}
	for i := range p.DataSources {
		traverseObjectSchema(&p.DataSources[i].Schema, trunc)
	}
	for i := range p.Actions {
		traverseObjectSchema(&p.Actions[i].ConfigSchema, trunc)
	}
	for i := range p.EphemeralResources {
		traverseObjectSchema(&p.EphemeralResources[i].ConfigSchema, trunc)
		traverseObjectSchema(&p.EphemeralResources[i].ResultSchema, trunc)
	}
	for i := range p.ListResources {
		traverseObjectSchema(&p.ListResources[i].ConfigSchema, trunc)
		traverseObjectSchema(&p.ListResources[i].IdentitySchema, trunc)
		if p.ListResources[i].ResourceSchema != nil {
			traverseObjectSchema(p.ListResources[i].ResourceSchema, trunc)
		}
	}
	for i := range p.Functions {
		for j := range p.Functions[i].Arguments {
			a := &p.Functions[i].Arguments[j]
			trunc(&a.Description)
			traverseSchema(&a.Schema, trunc)
		}
		traverseSchema(&p.Functions[i].ReturnType, trunc)
	}
	traverseObjectSchema(&p.ConfigSchema, trunc)
	return truncated
}

// traverseObjectSchema walks every description reachable from an object schema
// and applies fn to a pointer to each (including nested attributes, blocks,
// and standalone schema nodes).
func traverseObjectSchema(o *ir.ObjectSchemaIR, fn func(*string)) {
	if o == nil {
		return
	}
	for i := range o.Attributes {
		a := &o.Attributes[i]
		fn(&a.Description)
		traverseSchema(&a.Schema, fn)
	}
	for i := range o.Blocks {
		b := &o.Blocks[i]
		fn(&b.Description)
		traverseObjectSchema(&b.Schema, fn)
	}
}

func traverseSchema(s *ir.SchemaIR, fn func(*string)) {
	if s == nil {
		return
	}
	fn(&s.Description)
	if s.Collection != nil {
		traverseSchema(&s.Collection.ElementType, fn)
	}
	if s.Union != nil {
		for i := range s.Union.Variants {
			traverseSchema(&s.Union.Variants[i], fn)
		}
	}
	for i := range s.Attributes {
		a := &s.Attributes[i]
		fn(&a.Description)
		traverseSchema(&a.Schema, fn)
	}
	for i := range s.Blocks {
		b := &s.Blocks[i]
		fn(&b.Description)
		traverseObjectSchema(&b.Schema, fn)
	}
	for _, sub := range []*ir.SchemaIR{s.Not, s.IfSchema, s.ThenSchema, s.ElseSchema, s.PropertyNames, s.UnevaluatedProperties} {
		if sub != nil {
			traverseSchema(sub, fn)
		}
	}
	for _, sub := range s.DependentSchemas {
		if sub != nil {
			traverseSchema(sub, fn)
		}
	}
	for _, sub := range s.PatternProperties {
		if sub != nil {
			traverseSchema(sub, fn)
		}
	}
}

// truncateDescription truncates s in place to at most max bytes on a UTF-8
// rune boundary, appending an ellipsis when a cut happened. It reports whether
// the string was truncated.
func truncateDescription(s *string, maxBytes int) bool {
	if maxBytes <= 0 || len(*s) <= maxBytes {
		return false
	}
	const ellipsis = "…"
	if maxBytes <= len(ellipsis) {
		// Too small to fit an ellipsis; hard-cut instead of exceeding the cap.
		*s = truncateRuneBoundary(*s, maxBytes)
		return true
	}
	*s = truncateRuneBoundary(*s, maxBytes-len(ellipsis)) + ellipsis
	return true
}

// truncateRuneBoundary cuts s to at most maxBytes without splitting a rune.
func truncateRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		// Back up to the previous rune boundary: slicing mid-rune produces an
		// invalid string, and a multi-byte rune straddling the limit is dropped.
		cut--
	}
	return s[:cut]
}

// humanBytes renders a byte count for diagnostics (e.g. "12.3 MiB").
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// isDocsMarkdownPath reports whether a cleaned relative output path is a
// Registry docs markdown file (docs/**.md), the family subject to the
// Registry's per-document 500KB truncation.
func isDocsMarkdownPath(clean string) bool {
	return strings.HasPrefix(clean, "docs/") && strings.HasSuffix(clean, ".md")
}
