package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// JSONConvertFile returns the generated internal/provider/json_convert.go file
// containing generic conversions between the Terraform Plugin Framework model
// structs and JSON-ready maps. Resource CRUD bodies that are wired to the
// generated API client use these helpers to build request bodies and to apply
// response payloads back to Terraform state, so no per-attribute mapping code
// has to be generated. The file carries a per-provider wire-name map so nested
// object attributes round-trip under the API's original property names (G18).
// The XML serialization helpers (mapToXML and friends) and their imports are
// emitted only when a resource wired CRUD body serializes application/xml
// (N-37); JSON-only providers carry no dead XML code.
func JSONConvertFile(provider *ir.ProviderIR) File {
	return Template("internal/provider/json_convert.go", schema.JSONConvertTemplate, map[string]any{
		"WireNamesBody": renderWireNames(provider),
		"IncludeXML":    AnyResourceXMLBody(provider.Resources),
	})
}

// renderWireNames builds the body of the generated wireNames map, keyed by
// model struct name. Entries are sorted by model name and attribute path so
// generation stays deterministic.
func renderWireNames(provider *ir.ProviderIR) string {
	byModel := map[string]map[string]string{}
	for _, r := range provider.Resources {
		wireNamesForModel(byModel, resourceModelName(r), r.Schema)
	}
	for _, ds := range provider.DataSources {
		wireNamesForModel(byModel, dataSourceAPIModelName(ds), ds.Schema)
	}
	for _, a := range provider.Actions {
		wireNamesForModel(byModel, actionModelName(a), a.ConfigSchema)
	}
	for _, er := range provider.EphemeralResources {
		// The ephemeral model combines config and result attributes.
		wireNamesForModel(byModel, ephemeralResourceModelName(er), er.ConfigSchema)
		wireNamesForModel(byModel, ephemeralResourceModelName(er), er.ResultSchema)
	}

	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)

	var b strings.Builder
	for _, m := range models {
		paths := make([]string, 0, len(byModel[m]))
		for p := range byModel[m] {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		fmt.Fprintf(&b, "\t%q: {\n", m)
		for _, p := range paths {
			fmt.Fprintf(&b, "\t\t%q: %q,\n", p, byModel[m][p])
		}
		fmt.Fprintf(&b, "\t},\n")
	}
	return b.String()
}

// wireNamesForModel records the wire names of a model's schema into byModel.
// Models with no wire-name differences are omitted, and repeated calls for the
// same model (ephemeral config + result) merge their entries.
func wireNamesForModel(byModel map[string]map[string]string, model string, s ir.ObjectSchemaIR) {
	m := map[string]string{}
	collectObjectWireNames(s, "", m)
	if len(m) == 0 {
		return
	}
	existing := byModel[model]
	if existing == nil {
		byModel[model] = m
		return
	}
	for k, v := range m {
		existing[k] = v
	}
}

// collectObjectWireNames records wire names for attributes whose wire name
// differs from the tfsdk name. Paths are dot-joined tfsdk attribute names;
// collection elements are denoted with a "*" segment (e.g. "modules.*.symbol").
func collectObjectWireNames(s ir.ObjectSchemaIR, prefix string, out map[string]string) {
	for _, attr := range s.Attributes {
		path := prefix + attr.Name
		if attr.WireName != "" && attr.WireName != attr.Name {
			out[path] = attr.WireName
		}
		collectSchemaWireNames(attr.Schema, path+".", out)
	}
	for _, block := range s.Blocks {
		collectObjectWireNames(block.Schema, prefix+block.Name+".", out)
	}
}

// collectSchemaWireNames recurses into a schema node, threading the "*" path
// segment through collection elements so nested object elements resolve their
// wire names at the same path the generated conversion helpers use.
func collectSchemaWireNames(s ir.SchemaIR, prefix string, out map[string]string) {
	if s.Collection != nil {
		collectSchemaWireNames(s.Collection.ElementType, prefix+"*.", out)
		return
	}
	if s.Union != nil {
		for _, variant := range s.Union.Variants {
			collectSchemaWireNames(variant, prefix, out)
		}
		return
	}
	collectObjectWireNames(ir.ObjectSchemaIR{Attributes: s.Attributes, Blocks: s.Blocks}, prefix, out)
}
