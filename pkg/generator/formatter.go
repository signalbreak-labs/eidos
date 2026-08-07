package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// Summary is the structured dry-run report produced by the generator. It is
// formatted to either human-readable text or JSON by FormatText and FormatJSON.
type Summary struct {
	// ProviderName is the short name of the generated provider.
	ProviderName string `json:"provider_name"`
	// Spec is the path to the source OpenAPI specification file.
	Spec string `json:"spec"`
	// SpecVersion is the detected OpenAPI version (e.g. "3.0.3").
	SpecVersion string `json:"spec_version"`
	// ConfigPath is the path to the generator.yaml overrides file, if any.
	ConfigPath string `json:"config_path,omitempty"`
	// Counts reports how many constructs would be generated.
	Counts Counts `json:"counts"`
	// Files is the deterministic list of files that would be written (dry-run)
	// or were written (full generation).
	Files []FileEntry `json:"files"`
	// Written is true when the files have actually been written to disk (full
	// generation). It is false for a dry-run summary. It controls the wording of
	// the human-readable text report so a post-generation summary does not claim
	// files would "would be written" (L-1).
	Written bool `json:"written"`
	// Diagnostics contains informational, warning, and error messages from the
	// generation pipeline.
	Diagnostics []SummaryDiagnostic `json:"diagnostics"`
}

// Counts holds per-construct totals for the dry-run summary.
type Counts struct {
	Resources           int `json:"resources"`
	DataSources         int `json:"data_sources"`
	Actions             int `json:"actions"`
	EphemeralResources  int `json:"ephemeral_resources"`
	ListResources       int `json:"list_resources"`
	Functions           int `json:"functions"`
	SecuritySchemes     int `json:"security_schemes"`
	WriteOnlyAttributes int `json:"write_only_attributes"`
}

// SummaryDiagnostic is a single diagnostic rendered in the dry-run summary.
// It exposes severity as a lower-case string, a human-readable summary message,
// and an optional detail field. The text renderer concatenates Summary and
// Detail; JSON consumers receive them as separate fields for richer rendering.
type SummaryDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
}

// NewSummary builds a dry-run Summary from the generation inputs. It computes
// construct counts from provider and attaches the supplied file list and
// diagnostics. A nil provider is treated as an empty provider.
func NewSummary(provider *ir.ProviderIR, spec, configPath string, files []FileEntry, diags diagnostics.Diagnostics) Summary {
	if files == nil {
		files = []FileEntry{}
	}
	return Summary{
		ProviderName: providerName(provider),
		Spec:         spec,
		SpecVersion:  specVersion(provider),
		ConfigPath:   configPath,
		Counts:       CountsFromProviderIR(provider),
		Files:        files,
		Diagnostics:  summaryDiagnosticsFrom(diags),
	}
}

// CountsFromProviderIR computes the construct counts for a provider. A nil
// provider returns a zero Counts value.
func CountsFromProviderIR(provider *ir.ProviderIR) Counts {
	if provider == nil {
		return Counts{}
	}

	return Counts{
		Resources:           len(provider.Resources),
		DataSources:         len(provider.DataSources),
		Actions:             len(provider.Actions),
		EphemeralResources:  len(provider.EphemeralResources),
		ListResources:       len(provider.ListResources),
		Functions:           len(provider.Functions),
		SecuritySchemes:     len(provider.SecurityIR.Schemes),
		WriteOnlyAttributes: countWriteOnlyAttributes(provider),
	}
}

// FormatText renders a Summary as the documented human-readable dry-run report.
func FormatText(s Summary) string {
	var b strings.Builder

	title := "Eidos dry-run summary"
	filesLine := "Files that would be written (%d):\n"
	if s.Written {
		title = "Eidos generation summary"
		filesLine = "Files written (%d):\n"
	}
	if s.ProviderName != "" {
		fmt.Fprintf(&b, "%s for provider %q\n", title, s.ProviderName)
	} else {
		fmt.Fprintln(&b, title)
	}

	if s.Spec != "" {
		if s.SpecVersion != "" {
			fmt.Fprintf(&b, "Spec: %s (OpenAPI %s)\n", s.Spec, s.SpecVersion)
		} else {
			fmt.Fprintf(&b, "Spec: %s\n", s.Spec)
		}
	}

	if s.ConfigPath != "" {
		fmt.Fprintf(&b, "Config: %s\n", s.ConfigPath)
	}

	if s.Spec != "" || s.ConfigPath != "" {
		b.WriteByte('\n')
	}

	fmt.Fprintln(&b, "Generated constructs:")
	writeCounts(&b, s.Counts)

	b.WriteByte('\n')
	fmt.Fprintf(&b, filesLine, len(s.Files))
	for _, f := range s.Files {
		fmt.Fprintf(&b, "  %s\n", f.Path)
	}

	b.WriteByte('\n')
	fmt.Fprintln(&b, "Diagnostics:")
	if len(s.Diagnostics) == 0 {
		fmt.Fprintln(&b, "  none")
	} else {
		for _, d := range s.Diagnostics {
			msg := d.Message
			if d.Detail != "" {
				if msg != "" {
					msg = msg + ": " + d.Detail
				} else {
					msg = d.Detail
				}
			}
			fmt.Fprintf(&b, "  [%s] %s\n", d.Severity, msg)
		}
	}

	return b.String()
}

// FormatJSON renders a Summary as the documented JSON dry-run report.
//
// The returned bytes always end with a trailing newline. Callers that embed
// the output inside another JSON envelope must trim that newline first.
func FormatJSON(s Summary) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dry-run summary: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

func providerName(provider *ir.ProviderIR) string {
	if provider == nil {
		return ""
	}
	return provider.Name
}

func specVersion(provider *ir.ProviderIR) string {
	if provider == nil {
		return ""
	}
	return provider.SourceSpecVersion
}

func summaryDiagnosticsFrom(diags diagnostics.Diagnostics) []SummaryDiagnostic {
	out := make([]SummaryDiagnostic, 0, len(diags))
	for _, d := range diags {
		out = append(out, SummaryDiagnostic{
			Severity: d.Severity.String(),
			Message:  d.Summary,
			Detail:   d.Detail,
		})
	}
	return out
}

func writeCounts(b *strings.Builder, c Counts) {
	type countLine struct {
		label string
		value int
	}
	labels := []countLine{
		{"Resources", c.Resources},
		{"Data sources", c.DataSources},
		{"Actions", c.Actions},
		{"Ephemeral resources", c.EphemeralResources},
		{"List resources", c.ListResources},
		{"Functions", c.Functions},
		{"Security schemes", c.SecuritySchemes},
		{"Write-only attributes", c.WriteOnlyAttributes},
	}

	// L-37: every count line is always shown, so there is no hideWhenZero
	// filter; if a future line should be omitted at zero, add an explicit
	// branch here rather than reviving a never-true flag.

	maxWidth := 0
	for _, l := range labels {
		if w := len(l.label) + 1; w > maxWidth {
			maxWidth = w
		}
	}

	for _, l := range labels {
		fmt.Fprintf(b, "  %-*s%d\n", maxWidth, l.label+":", l.value)
	}
}

func countWriteOnlyAttributes(provider *ir.ProviderIR) int {
	if provider == nil {
		return 0
	}

	total := 0
	for _, res := range provider.Resources {
		total += countWriteOnlyInObjectSchema(&res.Schema)
	}
	for _, ds := range provider.DataSources {
		total += countWriteOnlyInObjectSchema(&ds.Schema)
	}
	for _, act := range provider.Actions {
		total += countWriteOnlyInObjectSchema(&act.ConfigSchema)
	}
	for _, er := range provider.EphemeralResources {
		total += countWriteOnlyInObjectSchema(&er.ConfigSchema)
		total += countWriteOnlyInObjectSchema(&er.ResultSchema)
	}
	for _, lr := range provider.ListResources {
		total += countWriteOnlyInObjectSchema(&lr.ConfigSchema)
		total += countWriteOnlyInObjectSchema(&lr.IdentitySchema)
		if lr.ResourceSchema != nil {
			total += countWriteOnlyInObjectSchema(lr.ResourceSchema)
		}
	}
	for _, fn := range provider.Functions {
		for _, arg := range fn.Arguments {
			total += countWriteOnlyInAttribute(arg)
		}
		total += countWriteOnlyInSchema(&fn.ReturnType)
	}

	return total
}

// countWriteOnlyInObjectSchema counts explicitly write-only attributes inside
// an object schema, recursing into nested blocks. It takes a pointer to match
// the pointer pattern used by countWriteOnlyInSchema.
func countWriteOnlyInObjectSchema(obj *ir.ObjectSchemaIR) int {
	if obj == nil {
		return 0
	}
	total := 0
	for _, attr := range obj.Attributes {
		total += countWriteOnlyInAttribute(attr)
	}
	for _, block := range obj.Blocks {
		total += countWriteOnlyInObjectSchema(&block.Schema)
	}
	return total
}

// countWriteOnlyInAttribute counts a single attribute. Attribute-level WriteOnly
// is terminal: a write-only attribute is counted as one and its schema is not
// recursed, because any nested fields are implicitly write-only via their
// parent. For non-write-only attributes, counting continues into the attribute
// schema.
func countWriteOnlyInAttribute(attr ir.AttributeIR) int {
	if attr.WriteOnly {
		return 1
	}
	return countWriteOnlyInSchema(&attr.Schema)
}

// countWriteOnlyInSchema counts write-only flags inside a schema node and
// recurses into attributes, blocks, collections, union variants, conditional
// branches, dependent schemas, pattern properties, negations, unevaluated
// properties, and property-name schemas. Schema-level WriteOnly contributes
// one to the total and the recursion continues, so a write-only object schema
// counts itself plus any explicitly write-only descendants.
func countWriteOnlyInSchema(schema *ir.SchemaIR) int {
	if schema == nil {
		return 0
	}

	total := 0
	if schema.WriteOnly {
		total++
	}

	for _, attr := range schema.Attributes {
		total += countWriteOnlyInAttribute(attr)
	}
	for _, block := range schema.Blocks {
		total += countWriteOnlyInObjectSchema(&block.Schema)
	}
	if schema.Collection != nil {
		total += countWriteOnlyInSchema(&schema.Collection.ElementType)
	}
	if schema.Union != nil {
		for _, v := range schema.Union.Variants {
			total += countWriteOnlyInSchema(&v)
		}
	}
	if schema.IfSchema != nil {
		total += countWriteOnlyInSchema(schema.IfSchema)
	}
	if schema.ThenSchema != nil {
		total += countWriteOnlyInSchema(schema.ThenSchema)
	}
	if schema.ElseSchema != nil {
		total += countWriteOnlyInSchema(schema.ElseSchema)
	}
	for _, dep := range schema.DependentSchemas {
		total += countWriteOnlyInSchema(dep)
	}
	for _, pp := range schema.PatternProperties {
		total += countWriteOnlyInSchema(pp)
	}
	if schema.Not != nil {
		total += countWriteOnlyInSchema(schema.Not)
	}
	if schema.UnevaluatedProperties != nil {
		total += countWriteOnlyInSchema(schema.UnevaluatedProperties)
	}
	if schema.PropertyNames != nil {
		total += countWriteOnlyInSchema(schema.PropertyNames)
	}

	return total
}
