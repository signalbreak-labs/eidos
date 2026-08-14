package generator

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestRenderWireNames verifies that renderWireNames emits a deterministic,
// sorted wire-name map covering nested objects, lists of objects, and maps of
// objects, keyed by the generated model struct name.
func TestRenderWireNames(t *testing.T) {
	p := ir.ProviderIR{
		DataSources: []ir.DataSourceIR{
			{
				Name: "get_ship",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name:     "ship_symbol",
							WireName: "shipSymbol",
							Schema:   ir.SchemaIR{Type: ir.TypeString},
						},
						{
							Name: "nav",
							Schema: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "system_symbol", WireName: "systemSymbol", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "flight_mode", WireName: "flightMode", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "status", Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
						{
							Name: "modules",
							Schema: ir.SchemaIR{
								Collection: &ir.CollectionType{
									Kind: ir.List,
									ElementType: ir.SchemaIR{
										Attributes: []ir.AttributeIR{
											{Name: "capacity", Schema: ir.SchemaIR{Type: ir.TypeInt}},
											{Name: "module_symbol", WireName: "moduleSymbol", Schema: ir.SchemaIR{Type: ir.TypeString}},
										},
									},
								},
							},
						},
						{
							Name: "labels",
							Schema: ir.SchemaIR{
								Collection: &ir.CollectionType{
									Kind: ir.Map,
									ElementType: ir.SchemaIR{
										Attributes: []ir.AttributeIR{
											{Name: "display_name", WireName: "displayName", Schema: ir.SchemaIR{Type: ir.TypeString}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body := renderWireNames(&p)

	// The model key is present and sorted.
	if !strings.Contains(body, `"GetShipDataSourceModel": {`) {
		t.Fatalf("renderWireNames missing model key:\n%s", body)
	}

	// Nested object attributes use their wire names.
	for _, want := range []string{
		`"nav.flight_mode": "flightMode"`,
		`"nav.system_symbol": "systemSymbol"`,
		`"ship_symbol": "shipSymbol"`,
		`"modules.*.module_symbol": "moduleSymbol"`,
		`"labels.*.display_name": "displayName"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("renderWireNames missing %q:\n%s", want, body)
		}
	}

	// Attributes whose wire name matches the tfsdk name are omitted.
	for _, notWant := range []string{
		`"nav.status"`,
		`"modules.*.capacity"`,
	} {
		if strings.Contains(body, notWant) {
			t.Errorf("renderWireNames should omit %q:\n%s", notWant, body)
		}
	}

	// Paths are sorted within a model.
	navIdx := strings.Index(body, `"nav.flight_mode"`)
	sysIdx := strings.Index(body, `"nav.system_symbol"`)
	if navIdx == -1 || sysIdx == -1 || navIdx > sysIdx {
		t.Errorf("renderWireNames paths not sorted (flight_mode before system_symbol):\n%s", body)
	}
}

// TestRenderWireNamesEmpty verifies that models with no wire-name differences
// are omitted entirely.
func TestRenderWireNamesEmpty(t *testing.T) {
	p := ir.ProviderIR{
		Actions: []ir.ActionIR{
			{
				Name: "dock_ship",
				ConfigSchema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "ship_symbol", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
		},
	}
	body := renderWireNames(&p)
	if strings.Contains(body, "DockShipActionModel") {
		t.Errorf("renderWireNames should omit models with no wire-name differences:\n%s", body)
	}
}
