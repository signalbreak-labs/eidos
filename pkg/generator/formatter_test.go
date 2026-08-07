package generator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestNewSummary_ComputesCountsFromProviderIR(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:              "mycloud",
		SourceSpecVersion: "3.0.3",
		Resources:         []ir.ResourceIR{{Name: "pet"}, {Name: "server"}},
		DataSources:       []ir.DataSourceIR{{Name: "pet"}},
		Actions:           []ir.ActionIR{{Name: "reboot"}},
		EphemeralResources: []ir.EphemeralResourceIR{
			{Name: "temporaryCredential"},
		},
		ListResources: []ir.ListResourceIR{{Name: "pets"}},
		Functions:     []ir.FunctionIR{{Name: "ipLookup"}},
		SecurityIR: ir.SecurityIR{
			Schemes: []ir.SecuritySchemeIR{{Name: "apiKey", Type: ir.SecuritySchemeAPIKey}},
		},
	}

	summary := NewSummary(provider, "./api.yaml", "./generator.yaml", nil, nil)

	if summary.ProviderName != "mycloud" {
		t.Errorf("provider_name = %q, want mycloud", summary.ProviderName)
	}
	if summary.Spec != "./api.yaml" {
		t.Errorf("spec = %q, want ./api.yaml", summary.Spec)
	}
	if summary.SpecVersion != "3.0.3" {
		t.Errorf("spec_version = %q, want 3.0.3", summary.SpecVersion)
	}
	if summary.ConfigPath != "./generator.yaml" {
		t.Errorf("config_path = %q, want ./generator.yaml", summary.ConfigPath)
	}

	c := summary.Counts
	if c.Resources != 2 {
		t.Errorf("resources count = %d, want 2", c.Resources)
	}
	if c.DataSources != 1 {
		t.Errorf("data_sources count = %d, want 1", c.DataSources)
	}
	if c.Actions != 1 {
		t.Errorf("actions count = %d, want 1", c.Actions)
	}
	if c.EphemeralResources != 1 {
		t.Errorf("ephemeral_resources count = %d, want 1", c.EphemeralResources)
	}
	if c.ListResources != 1 {
		t.Errorf("list_resources count = %d, want 1", c.ListResources)
	}
	if c.Functions != 1 {
		t.Errorf("functions count = %d, want 1", c.Functions)
	}
	if c.SecuritySchemes != 1 {
		t.Errorf("security_schemes count = %d, want 1", c.SecuritySchemes)
	}
}

func TestNewSummary_NilProvider(t *testing.T) {
	summary := NewSummary(nil, "./api.yaml", "", nil, nil)

	if summary.ProviderName != "" {
		t.Errorf("provider_name = %q, want empty", summary.ProviderName)
	}
	if summary.SpecVersion != "" {
		t.Errorf("spec_version = %q, want empty", summary.SpecVersion)
	}
	if summary.Counts.Resources != 0 {
		t.Errorf("resources count = %d, want 0", summary.Counts.Resources)
	}
}

func TestFormatText_IncludesAllSections(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:              "mycloud",
		SourceSpecVersion: "3.0.3",
		Resources:         []ir.ResourceIR{{Name: "pet"}},
	}
	files := []FileEntry{
		{Path: "internal/provider/provider.go", Reason: "provider schema and registration"},
		{Path: "internal/provider/resource_pet.go", Reason: "resource Pet"},
	}
	diags := diagnostics.Diagnostics{
		{Severity: diagnostics.Warning, Summary: "Pet.oneOf: split_resources strategy selected for Pet"},
		{Severity: diagnostics.Info, Summary: "test files disabled", Detail: "pass --generate-terraform-tests to include them"},
	}

	summary := NewSummary(provider, "./api.yaml", "./generator.yaml", files, diags)
	text := FormatText(summary)

	want := []string{
		`Eidos dry-run summary for provider "mycloud"`,
		"Spec: ./api.yaml (OpenAPI 3.0.3)",
		"Config: ./generator.yaml",
		"Generated constructs:",
		"Resources:",
		"Data sources:",
		"Actions:",
		"Files that would be written (2):",
		"  internal/provider/provider.go",
		"  internal/provider/resource_pet.go",
		"Diagnostics:",
		"[warning] Pet.oneOf: split_resources strategy selected for Pet",
		"[info] test files disabled: pass --generate-terraform-tests to include them",
	}

	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("text summary missing expected content %q:\n%s", w, text)
		}
	}
}

func TestFormatText_EmptyDiagnostics(t *testing.T) {
	summary := NewSummary(&ir.ProviderIR{Name: "empty"}, "./api.yaml", "", nil, nil)
	text := FormatText(summary)

	if !strings.Contains(text, "Diagnostics:\n  none\n") {
		t.Errorf("expected 'none' diagnostics line, got:\n%s", text)
	}
}

func TestFormatJSON_MatchesDocumentedShape(t *testing.T) {
	provider := &ir.ProviderIR{
		Name:              "mycloud",
		SourceSpecVersion: "3.0.3",
		Resources:         []ir.ResourceIR{{Name: "pet"}},
	}
	files := []FileEntry{
		{Path: "internal/provider/provider.go", Reason: "provider schema and registration"},
		{Path: "internal/provider/resource_pet.go", Reason: "resource Pet"},
	}
	diags := diagnostics.Diagnostics{
		{Severity: diagnostics.Warning, Summary: "Pet.oneOf: split_resources strategy selected for Pet"},
	}

	summary := NewSummary(provider, "./api.yaml", "./generator.yaml", files, diags)
	data, err := FormatJSON(summary)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\n%s", err, string(data))
	}

	if got["provider_name"] != "mycloud" {
		t.Errorf("provider_name = %v, want mycloud", got["provider_name"])
	}
	if got["spec_version"] != "3.0.3" {
		t.Errorf("spec_version = %v, want 3.0.3", got["spec_version"])
	}
	if got["config_path"] != "./generator.yaml" {
		t.Errorf("config_path = %v, want ./generator.yaml", got["config_path"])
	}

	counts, ok := got["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts not an object: %v", got["counts"])
	}
	if counts["resources"] != float64(1) {
		t.Errorf("counts.resources = %v, want 1", counts["resources"])
	}

	fileList, ok := got["files"].([]any)
	if !ok || len(fileList) != 2 {
		t.Fatalf("files = %v, want 2 entries", got["files"])
	}
	first := fileList[0].(map[string]any)
	if first["path"] != "internal/provider/provider.go" {
		t.Errorf("first file path = %v", first["path"])
	}
	if first["reason"] != "provider schema and registration" {
		t.Errorf("first file reason = %v", first["reason"])
	}

	diagList, ok := got["diagnostics"].([]any)
	if !ok || len(diagList) != 1 {
		t.Fatalf("diagnostics = %v, want 1 entry", got["diagnostics"])
	}
	d := diagList[0].(map[string]any)
	if d["severity"] != "warning" {
		t.Errorf("diagnostic severity = %v, want warning", d["severity"])
	}
	if d["message"] != "Pet.oneOf: split_resources strategy selected for Pet" {
		t.Errorf("diagnostic message = %v", d["message"])
	}

	if !strings.HasSuffix(string(data), "\n") {
		t.Error("JSON output should end with a newline")
	}
}

func TestFormatJSON_EmptyFilesEmitsEmptyArray(t *testing.T) {
	summary := NewSummary(nil, "./api.yaml", "", nil, nil)
	data, err := FormatJSON(summary)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\n%s", err, string(data))
	}

	files, ok := got["files"].([]any)
	if !ok {
		t.Fatalf("files = %v, want []", got["files"])
	}
	if len(files) != 0 {
		t.Errorf("files length = %d, want 0", len(files))
	}
}

func TestFormatJSON_DiagnosticsPreserveDetail(t *testing.T) {
	diags := diagnostics.Diagnostics{{
		Severity: diagnostics.Info,
		Summary:  "test files disabled",
		Detail:   "pass --generate-terraform-tests to include them",
	}}
	summary := NewSummary(nil, "./api.yaml", "", nil, diags)
	data, err := FormatJSON(summary)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\n%s", err, string(data))
	}

	diagList, ok := got["diagnostics"].([]any)
	if !ok || len(diagList) != 1 {
		t.Fatalf("diagnostics = %v, want 1 entry", got["diagnostics"])
	}
	d := diagList[0].(map[string]any)
	if d["severity"] != "info" {
		t.Errorf("severity = %v, want info", d["severity"])
	}
	if d["message"] != "test files disabled" {
		t.Errorf("message = %v, want 'test files disabled'", d["message"])
	}
	if d["detail"] != "pass --generate-terraform-tests to include them" {
		t.Errorf("detail = %v, want 'pass --generate-terraform-tests to include them'", d["detail"])
	}
}

func TestCountsFromProviderIR_PolymorphismSplitsRemoved(t *testing.T) {
	provider := &ir.ProviderIR{Name: "empty"}
	summary := NewSummary(provider, "./api.yaml", "", nil, nil)

	text := FormatText(summary)
	if strings.Contains(text, "Polymorphism splits") {
		t.Errorf("text output should not mention polymorphism splits after field removal:\n%s", text)
	}

	data, err := FormatJSON(summary)
	if err != nil {
		t.Fatalf("FormatJSON failed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\n%s", err, string(data))
	}
	counts, ok := got["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts = %v, want object", got["counts"])
	}
	if _, exists := counts["polymorphism_splits"]; exists {
		t.Errorf("JSON counts should not contain polymorphism_splits after field removal: %v", counts)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributes(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
					{Name: "secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInNestedBlocks(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Blocks: []ir.BlockIR{{
					Name: "settings",
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "password", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInCollections(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name: "secrets",
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.List,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "value", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInUnions(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name: "credential",
					Schema: ir.SchemaIR{
						Union: &ir.UnionType{
							Variants: []ir.SchemaIR{
								{Attributes: []ir.AttributeIR{{Name: "token", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}}},
								{Attributes: []ir.AttributeIR{{Name: "plain", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
							},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInDependentSchemas(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name: "config",
					Schema: ir.SchemaIR{
						DependentSchemas: map[string]*ir.SchemaIR{
							"credit_card": {
								Attributes: []ir.AttributeIR{
									{Name: "cvv", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInIfThenElse(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name: "conditional",
					Schema: ir.SchemaIR{
						IfSchema: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "if_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
						ThenSchema: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "then_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
						ElseSchema: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "else_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 3 {
		t.Errorf("write_only_attributes = %d, want 3", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_WriteOnlyAttributeEarlyReturn(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name:      "secret",
					WriteOnly: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "nested_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 1 {
		t.Errorf("write_only_attributes = %d, want 1 (parent write-only should be terminal)", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInAdvancedSchemaBranches(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{{
					Name: "advanced",
					Schema: ir.SchemaIR{
						PatternProperties: map[string]*ir.SchemaIR{
							"^sec$": {
								Attributes: []ir.AttributeIR{{Name: "password", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
							},
						},
						Not: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "not_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
						UnevaluatedProperties: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "uneval_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
						PropertyNames: &ir.SchemaIR{
							Attributes: []ir.AttributeIR{{Name: "prop_name_secret", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
						},
					},
				}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 4 {
		t.Errorf("write_only_attributes = %d, want 4", counts.WriteOnlyAttributes)
	}
}

func TestCountsFromProviderIR_CountsWriteOnlyAttributesInFunctions(t *testing.T) {
	provider := &ir.ProviderIR{
		Functions: []ir.FunctionIR{{
			Name: "rotate",
			Arguments: []ir.FunctionParamIR{
				{Name: "old", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
			ReturnType: ir.SchemaIR{
				Attributes: []ir.AttributeIR{{Name: "new", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
			},
		}},
	}

	counts := CountsFromProviderIR(provider)
	if counts.WriteOnlyAttributes != 2 {
		t.Errorf("write_only_attributes = %d, want 2", counts.WriteOnlyAttributes)
	}
}
