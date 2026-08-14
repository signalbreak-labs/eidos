package api

import (
	"fmt"
	"sort"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// namedConstruct pairs a generated construct's name with a human-readable
// description of the OpenAPI operation it came from, for duplicate diagnostics.
type namedConstruct struct {
	kind   string
	name   string
	source string
}

// checkDuplicateConstructNames fails loud when two constructs of the same kind
// share a name. Such a collision would make the generator emit two files at the
// same path (e.g. docs/actions/<name>.md), which previously surfaced as a
// confusing "duplicate output path" error from the generator. Here it is
// reported as an actionable diagnostic naming both source operations so the
// user can rename an operationId or add a generator.yaml override.
func checkDuplicateConstructNames(preview *ir.ProviderIR) diagnostics.Diagnostics {
	constructs := make([]namedConstruct, 0,
		len(preview.Resources)+len(preview.DataSources)+len(preview.Actions)+
			len(preview.EphemeralResources)+len(preview.ListResources)+len(preview.Functions))
	for _, r := range preview.Resources {
		constructs = append(constructs, namedConstruct{"resource", r.Name, describeSource(r.SourceOperation, r.CRUDMapping.Create)})
	}
	for _, d := range preview.DataSources {
		constructs = append(constructs, namedConstruct{"data source", d.Name, describeSource(d.SourceOperation, d.ReadMapping)})
	}
	for _, a := range preview.Actions {
		constructs = append(constructs, namedConstruct{"action", a.Name, describeSource(a.SourceOperation, a.InvokeMapping)})
	}
	for _, e := range preview.EphemeralResources {
		constructs = append(constructs, namedConstruct{"ephemeral resource", e.Name, describeSource(e.SourceOperation, e.OpenMapping)})
	}
	for _, l := range preview.ListResources {
		constructs = append(constructs, namedConstruct{"list resource", l.Name, describeSource(l.SourceOperation, l.ListMapping)})
	}
	for _, f := range preview.Functions {
		constructs = append(constructs, namedConstruct{"function", f.Name, describeSource(f.SourceOperation, ir.OperationMappingIR{})})
	}

	// Group by (kind, name) deterministically so the diagnostic order is stable.
	sort.Slice(constructs, func(i, j int) bool {
		if constructs[i].kind != constructs[j].kind {
			return constructs[i].kind < constructs[j].kind
		}
		return constructs[i].name < constructs[j].name
	})

	var diags diagnostics.Diagnostics
	for i := 0; i < len(constructs); {
		j := i
		for j < len(constructs) && constructs[j].kind == constructs[i].kind && constructs[j].name == constructs[i].name {
			j++
		}
		if j-i > 1 {
			group := constructs[i:j]
			diags = append(diags, diagnostics.Diagnostic{
				Severity: diagnostics.Error,
				Summary:  fmt.Sprintf("duplicate %s name %q", group[0].kind, group[0].name),
				Detail: fmt.Sprintf(
					"The following operations all produce the %s name %q: %s. "+
						"Rename the colliding operationIds in the spec, or use a generator.yaml override that matches the "+
						"operation's METHOD /path (e.g. action_overrides with operation: \"PUT /snmp/throttle\") to disambiguate.",
					group[0].kind, group[0].name, joinSources(group)),
			})
		}
		i = j
	}
	return diags
}

// describeSource renders a construct's source operation for a diagnostic, e.g.
// "PUT /snmp/throttle (operationId redefineSnmpThrottleConfig)".
func describeSource(operationID string, m ir.OperationMappingIR) string {
	loc := ""
	if m.Method != "" || m.PathTemplate != "" {
		loc = fmt.Sprintf("%s %s", m.Method, m.PathTemplate)
	}
	switch {
	case loc != "" && operationID != "":
		return fmt.Sprintf("%s (operationId %s)", loc, operationID)
	case loc != "":
		return loc
	case operationID != "":
		return "operationId " + operationID
	default:
		return "an operation"
	}
}

func joinSources(group []namedConstruct) string {
	parts := make([]string, 0, len(group))
	for _, c := range group {
		parts = append(parts, c.source)
	}
	out := ""
	for i, p := range parts {
		switch {
		case i == len(parts)-1:
			out += "and " + p
		case i == len(parts)-2:
			out += p + " "
		default:
			out += p + ", "
		}
	}
	return out
}
