package transformer

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func ptrDuration(d time.Duration) *time.Duration {
	return &d
}

func ptrConfigDuration(d time.Duration) *config.Duration {
	cd := config.Duration(d)
	return &cd
}

func boolPtr(b bool) *bool {
	return &b
}

func TestApplyOverrides_NilInputs(t *testing.T) {
	if err := ApplyOverrides(nil, &config.Config{}); err != nil {
		t.Errorf("ApplyOverrides(nil, cfg) = %v, want nil", err)
	}
	provider := &ir.ProviderIR{}
	if err := ApplyOverrides(provider, nil); err != nil {
		t.Errorf("ApplyOverrides(provider, nil) = %v, want nil", err)
	}
	if err := ApplyOverrides(nil, nil); err != nil {
		t.Errorf("ApplyOverrides(nil, nil) = %v, want nil", err)
	}
}

func TestApplyOverrides_ResourceName(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			FullName:        "Pet",
			SourceOperation: "createPet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:       "pet",
			ResourceName: "custom_pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Resources[0].Name; got != "custom_pet" {
		t.Errorf("resource Name = %q, want %q", got, "custom_pet")
	}
	// The inferred TypeName carries no provider prefix in this standalone IR, so
	// the rename preserves the empty prefix. In a real pipeline the TypeName is
	// "<provider>_<name>" and the prefix is preserved across the rename (see
	// TestApplyOverrides_ResourceName_PreservesProviderPrefix).
	if got := provider.Resources[0].TypeName; got != "custom_pet" {
		t.Errorf("resource TypeName = %q, want %q", got, "custom_pet")
	}
	if got := provider.Resources[0].FullName; got != "Custom Pet" {
		t.Errorf("resource FullName = %q, want %q", got, "Custom Pet")
	}
}

// TestApplyOverrides_ResourceDescription verifies that a resource_override
// description replaces the auto-inferred description, and that an omitted
// description does not erase the spec-supplied text. Mirrors the action and
// ephemeral override description handling.
func TestApplyOverrides_ResourceDescription(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			SourceOperation: "createPet",
			Description:     "Auto-inferred pet description.",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:      "pet",
			Description: "Custom pet resource description.",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Resources[0].Description; got != "Custom pet resource description." {
		t.Errorf("resource Description = %q, want %q", got, "Custom pet resource description.")
	}

	// An override without a description must not erase the existing one.
	provider2 := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			SourceOperation: "createPet",
			Description:     "Keep me.",
		}},
	}
	cfg2 := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "pet",
		}},
	}
	if err := ApplyOverrides(provider2, cfg2); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}
	if got := provider2.Resources[0].Description; got != "Keep me." {
		t.Errorf("resource Description = %q, want %q (omitted override must not erase)", got, "Keep me.")
	}
}

// resource_name override keeps the provider prefix on the Terraform type name
// (the shape produced by the real pipeline: TypeName is always
// "<provider>_<name>"). Terraform resolves a resource type as
// "<provider>_<resource>", so stripping the prefix — e.g. "space-traders-api_"
// from "space-traders-api_purchase_ship" — would leave an unresolvable
// "purchase_ship" type (previously emitted, breaking terraform plan).
func TestApplyOverrides_ResourceName_PreservesProviderPrefix(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "purchase_ship",
			TypeName:        "space-traders-api_purchase_ship",
			FullName:        "Purchase Ship",
			SourceOperation: "purchase-ship",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "space-traders-api", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Operation:    "purchase-ship",
			ResourceName: "buy_ship",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Resources[0].Name; got != "buy_ship" {
		t.Errorf("resource Name = %q, want %q", got, "buy_ship")
	}
	if got := provider.Resources[0].TypeName; got != "space-traders-api_buy_ship" {
		t.Errorf("resource TypeName = %q, want %q", got, "space-traders-api_buy_ship")
	}
	if got := provider.Resources[0].FullName; got != "Buy Ship" {
		t.Errorf("resource FullName = %q, want %q", got, "Buy Ship")
	}
}

// TestApplyOverrides_ActionName_PreservesProviderPrefix is the action
// counterpart of TestApplyOverrides_ResourceName_PreservesProviderPrefix: the
// action name override must keep the provider prefix on the Terraform action
// type name so the generated action is resolvable.
func TestApplyOverrides_ActionName_PreservesProviderPrefix(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:            "navigate_ship",
			TypeName:        "space-traders-api_navigate_ship",
			FullName:        "Navigate Ship",
			SourceOperation: "navigate-ship",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "space-traders-api", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "navigate-ship",
			Name:      "set_ship_flight_mode",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Actions[0].TypeName; got != "space-traders-api_set_ship_flight_mode" {
		t.Errorf("action TypeName = %q, want %q", got, "space-traders-api_set_ship_flight_mode")
	}
}

func TestApplyOverrides_ResourceByOperation(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			SourceOperation: "createPet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Operation:    "createPet",
			ResourceName: "renamed_pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Resources[0].Name != "renamed_pet" {
		t.Errorf("resource Name = %q, want %q", provider.Resources[0].Name, "renamed_pet")
	}
}

func TestApplyOverrides_IDAttributeAndImportFormat(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema:   ir.ObjectSchemaIR{},
			CRUDMapping: ir.CRUDMappingIR{
				Read: ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{petId}"},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:       "pet",
			IDAttribute:  "petId",
			ImportFormat: "%s:%s",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.IDAttribute != "petId" {
		t.Errorf("IDAttribute = %q, want %q", r.IDAttribute, "petId")
	}
	if r.ImportIDFormat != "%s:%s" {
		t.Errorf("ImportIDFormat = %q, want %q", r.ImportIDFormat, "%s:%s")
	}
	if !r.Importable {
		t.Errorf("Importable = false, want true")
	}
}

func TestApplyOverrides_ImportFormatWithoutRead(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:        "pet",
			TypeName:    "pet",
			Schema:      ir.ObjectSchemaIR{},
			CRUDMapping: ir.CRUDMappingIR{},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:       "pet",
			ImportFormat: "%s:%s",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.ImportIDFormat != "%s:%s" {
		t.Errorf("ImportIDFormat = %q, want %q", r.ImportIDFormat, "%s:%s")
	}
	if r.Importable {
		t.Errorf("Importable = true, want false (no Read operation)")
	}
}

func TestApplyOverrides_Timeouts(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "pet", Schema: ir.ObjectSchemaIR{}},
			{Name: "server", TypeName: "server", Schema: ir.ObjectSchemaIR{}},
			{Name: "partial", TypeName: "partial", Schema: ir.ObjectSchemaIR{}},
		},
	}
	// partial already has only Create set before overrides are applied.
	provider.Resources[2].Timeouts = &ir.TimeoutConfigIR{
		Create: ptrDuration(45 * time.Minute),
	}

	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "pet",
			Timeouts: &config.TimeoutConfig{
				Create: ptrConfigDuration(30 * time.Minute),
				Read:   ptrConfigDuration(10 * time.Minute),
				Update: ptrConfigDuration(30 * time.Minute),
				Delete: ptrConfigDuration(10 * time.Minute),
			},
		}},
		GlobalTimeouts: &config.TimeoutConfig{
			Create: ptrConfigDuration(20 * time.Minute),
			Read:   ptrConfigDuration(5 * time.Minute),
			Update: ptrConfigDuration(20 * time.Minute),
			Delete: ptrConfigDuration(5 * time.Minute),
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	wantPet := &ir.TimeoutConfigIR{
		Create: ptrDuration(30 * time.Minute),
		Read:   ptrDuration(10 * time.Minute),
		Update: ptrDuration(30 * time.Minute),
		Delete: ptrDuration(10 * time.Minute),
	}
	if !reflect.DeepEqual(provider.Resources[0].Timeouts, wantPet) {
		t.Errorf("pet timeouts = %+v, want %+v", provider.Resources[0].Timeouts, wantPet)
	}

	wantServer := &ir.TimeoutConfigIR{
		Create: ptrDuration(20 * time.Minute),
		Read:   ptrDuration(5 * time.Minute),
		Update: ptrDuration(20 * time.Minute),
		Delete: ptrDuration(5 * time.Minute),
	}
	if !reflect.DeepEqual(provider.Resources[1].Timeouts, wantServer) {
		t.Errorf("server timeouts = %+v, want %+v", provider.Resources[1].Timeouts, wantServer)
	}

	wantPartial := &ir.TimeoutConfigIR{
		Create: ptrDuration(45 * time.Minute),
		Read:   ptrDuration(5 * time.Minute),
		Update: ptrDuration(20 * time.Minute),
		Delete: ptrDuration(5 * time.Minute),
	}
	if !reflect.DeepEqual(provider.Resources[2].Timeouts, wantPartial) {
		t.Errorf("partial timeouts = %+v, want %+v", provider.Resources[2].Timeouts, wantPartial)
	}
}

func TestApplyOverrides_StateUpgrades(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "pet", TypeName: "pet", Schema: ir.ObjectSchemaIR{}},
			{Name: "server", TypeName: "server", Schema: ir.ObjectSchemaIR{}},
		},
	}

	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:        "pet",
			SchemaVersion: 2,
			StateUpgrades: []config.StateUpgradeConfig{
				{From: 0, Renames: map[string]string{"old_name": "new_name"}},
				{From: 1, Renames: map[string]string{"kind": "type"}},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Resources[0].SchemaVersion; got != 2 {
		t.Errorf("pet schema_version = %d, want 2", got)
	}
	wantUpgrades := []ir.StateUpgradeIR{
		{FromVersion: 0, Renames: map[string]string{"old_name": "new_name"}},
		{FromVersion: 1, Renames: map[string]string{"kind": "type"}},
	}
	if !reflect.DeepEqual(provider.Resources[0].StateUpgrades, wantUpgrades) {
		t.Errorf("pet state_upgrades = %+v, want %+v", provider.Resources[0].StateUpgrades, wantUpgrades)
	}

	// A resource without a state-upgrade override is left untouched.
	if provider.Resources[1].SchemaVersion != 0 || provider.Resources[1].StateUpgrades != nil {
		t.Errorf("server should have no state upgrades, got schema_version=%d upgrades=%+v",
			provider.Resources[1].SchemaVersion, provider.Resources[1].StateUpgrades)
	}
}

// TestApplyOverrides_StateUpgradesPreservedWhenAbsent verifies that an override
// matching a resource but declaring no state_upgrades does not clobber upgrades
// applied by an earlier matching override (last-declared-wins, but absence is
// not a reset).
func TestApplyOverrides_StateUpgradesPreservedWhenAbsent(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{Name: "pet", TypeName: "pet", Schema: ir.ObjectSchemaIR{}}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{
			{Schema: "pet", SchemaVersion: 1, StateUpgrades: []config.StateUpgradeConfig{
				{From: 0, Renames: map[string]string{"a": "b"}},
			}},
			// Second matching override does not declare state_upgrades; it must not
			// wipe the upgrades applied by the first.
			{Schema: "pet", ResourceName: "renamed_pet"},
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Resources[0].Name != "renamed_pet" {
		t.Errorf("resource name = %q, want renamed_pet", provider.Resources[0].Name)
	}
	if len(provider.Resources[0].StateUpgrades) != 1 {
		t.Fatalf("state_upgrades len = %d, want 1 (absence must not reset)", len(provider.Resources[0].StateUpgrades))
	}
	if got := provider.Resources[0].StateUpgrades[0].Renames["a"]; got != "b" {
		t.Errorf("state_upgrades[0].renames[a] = %q, want b", got)
	}
}

func TestApplyOverrides_AttributeFlags(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
					{Name: "created_at", Schema: ir.SchemaIR{Type: ir.TypeString}},
					{Name: "owner_secret", Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
				Blocks: []ir.BlockIR{{
					Name: "tags",
					Schema: ir.ObjectSchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "createdAt", Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				}},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:              "pet",
			ForceNew:            []string{"name"},
			ComputedAttributes:  []string{"createdAt"},
			SensitiveAttributes: []string{"owner_secret"},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.Resources[0].Schema.Attributes
	if !attrs[0].ForceNew {
		t.Errorf("name ForceNew = false, want true")
	}
	if !attrs[1].Computed {
		t.Errorf("created_at Computed = false, want true")
	}
	if !attrs[2].Sensitive {
		t.Errorf("owner_secret Sensitive = false, want true")
	}

	blockAttrs := provider.Resources[0].Schema.Blocks[0].Schema.Attributes
	if !blockAttrs[0].Computed {
		t.Errorf("nested createdAt Computed = false, want true")
	}
}

func TestApplyOverrides_WriteOnlyAttributes(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "existing", Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "pet",
			WriteOnlyAttributes: []config.WriteOnlyAttribute{
				{Name: "password", Description: "Secret password", Sensitive: true},
				{Name: "existing", Description: "Replaced write-only attr", Sensitive: false},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.Resources[0].Schema.Attributes
	if len(attrs) != 2 {
		t.Fatalf("attribute count = %d, want 2", len(attrs))
	}

	pass := attrs[1]
	if pass.Name != "password" {
		t.Errorf("write-only attr Name = %q, want %q", pass.Name, "password")
	}
	if !pass.WriteOnly {
		t.Errorf("password WriteOnly = false, want true")
	}
	if !pass.Sensitive {
		t.Errorf("password Sensitive = false, want true")
	}
	if pass.Description != "Secret password" {
		t.Errorf("password Description = %q, want %q", pass.Description, "Secret password")
	}

	existing := attrs[0]
	if !existing.WriteOnly {
		t.Errorf("existing WriteOnly = false, want true")
	}
	if existing.Sensitive {
		t.Errorf("existing Sensitive = true, want false")
	}
}

func TestApplyOverrides_WriteOnlyPreservesExistingRequired(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "api_key", Schema: ir.SchemaIR{Type: ir.TypeString}, Required: true},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "pet",
			WriteOnlyAttributes: []config.WriteOnlyAttribute{
				{Name: "api_key", Description: "API key for authentication", Sensitive: true},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attr := provider.Resources[0].Schema.Attributes[0]
	if attr.Name != "api_key" {
		t.Errorf("attr Name = %q, want %q", attr.Name, "api_key")
	}
	if !attr.Required {
		t.Errorf("attr Required = false, want true")
	}
	if !attr.WriteOnly {
		t.Errorf("attr WriteOnly = false, want true")
	}
	if !attr.Sensitive {
		t.Errorf("attr Sensitive = false, want true")
	}
	if attr.Description != "API key for authentication" {
		t.Errorf("attr Description = %q, want %q", attr.Description, "API key for authentication")
	}
}

// TestApplyOverrides_WriteOnlyClearsComputed locks in the M-46 fix: a Computed
// attribute that is forced write-only via an override must have Computed cleared,
// because the framework forbids WriteOnly together with Computed.
func TestApplyOverrides_WriteOnlyClearsComputed(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "token", Schema: ir.SchemaIR{Type: ir.TypeString}, Computed: true},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "pet",
			WriteOnlyAttributes: []config.WriteOnlyAttribute{
				{Name: "token", Sensitive: true},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attr := provider.Resources[0].Schema.Attributes[0]
	if !attr.WriteOnly {
		t.Errorf("attr WriteOnly = false, want true")
	}
	if attr.Computed {
		t.Errorf("attr Computed = true, want false (WriteOnly+Computed is forbidden by the framework)")
	}
}

// TestApplyWriteOnlyAttributesClearsComputed locks in the M-46 fix for the
// inference path: an attribute inferred as both Computed and write-only must
// have Computed cleared, not emitted as Computed+WriteOnly.
func TestApplyWriteOnlyAttributesClearsComputed(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:      "password",
				Computed:  true,
				WriteOnly: true,
				Schema:    ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	wo := obj.Attributes[0]
	if !wo.WriteOnly {
		t.Errorf("WriteOnly = false, want true")
	}
	if wo.Computed {
		t.Errorf("Computed = true, want false (WriteOnly+Computed is forbidden by the framework)")
	}
}

func TestApplyOverrides_SkipResource(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "keep", TypeName: "keep", Schema: ir.ObjectSchemaIR{}},
			{Name: "skip", TypeName: "skip", Schema: ir.ObjectSchemaIR{}},
		},
	}
	skip := true
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "skip",
			Skip:   &skip,
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if len(provider.Resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(provider.Resources))
	}
	if provider.Resources[0].Name != "keep" {
		t.Errorf("remaining resource Name = %q, want %q", provider.Resources[0].Name, "keep")
	}
}

func TestApplyOverrides_SkipWinsOverPriorOverrides(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			SourceOperation: "createPet",
			Schema:          ir.ObjectSchemaIR{},
		}},
	}
	skip := true
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{
			{
				Operation:    "createPet",
				ResourceName: "renamed_pet",
			},
			{
				Operation: "createPet",
				Skip:      &skip,
			},
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if len(provider.Resources) != 0 {
		t.Fatalf("resource count = %d, want 0", len(provider.Resources))
	}
}

func TestApplyOverrides_MultipleOverridesOnSameResource(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:            "pet",
			TypeName:        "pet",
			SourceOperation: "createPet",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{
			{
				Operation:    "createPet",
				ResourceName: "custom_pet",
			},
			{
				Operation:          "createPet",
				ComputedAttributes: []string{"name"},
			},
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.Name != "custom_pet" {
		t.Errorf("resource Name = %q, want %q", r.Name, "custom_pet")
	}
	if !r.Schema.Attributes[0].Computed {
		t.Errorf("name Computed = false, want true")
	}
}

// TestApplyOverrides_ComputedAttributesClearsRequired locks in the fix for the
// Required+Computed invalid combination: a computed_attributes override that
// claims a previously-Required attribute forces it Computed and clears Required
// (the plugin framework forbids Computed together with Required). Optional is
// preserved so an Optional attribute forced Computed becomes Optional+Computed,
// which is valid.
func TestApplyOverrides_ComputedAttributesClearsRequired(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "profile",
			TypeName: "profile",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "alias", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					{Name: "labels", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:             "profile",
			ComputedAttributes: []string{"alias", "labels"},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.Resources[0].Schema.Attributes
	alias := attrs[0]
	if !alias.Computed {
		t.Errorf("alias Computed = false, want true (forced by computed_attributes)")
	}
	if alias.Required {
		t.Errorf("alias Required = true, want false (Computed+Required is invalid; computed_attributes clears Required)")
	}
	labels := attrs[1]
	if !labels.Computed {
		t.Errorf("labels Computed = false, want true (forced by computed_attributes)")
	}
	if !labels.Optional {
		t.Errorf("labels Optional = false, want true (Optional preserved for Optional+Computed)")
	}
}

// TestApplyOverrides_ComputedAttributesNested locks in the N-20 fix: a
// computed_attributes (or force_new/sensitive) override targeting an attribute
// nested under an object attribute, inside a list element, or under a union
// variant is applied instead of matching nothing silently. The nested walk
// mirrors the write-only recursion.
func TestApplyOverrides_ComputedAttributesNested(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "profile",
			TypeName: "profile",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{
						Name:     "contact",
						Optional: true,
						Schema: ir.SchemaIR{
							Attributes: []ir.AttributeIR{
								{Name: "nested_field", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
							},
						},
					},
					{
						Name:     "tags",
						Optional: true,
						Schema: ir.SchemaIR{
							Collection: &ir.CollectionType{
								Kind: ir.List,
								ElementType: ir.SchemaIR{
									Attributes: []ir.AttributeIR{
										{Name: "list_nested", Sensitive: false, Schema: ir.SchemaIR{Type: ir.TypeString}},
									},
								},
							},
						},
					},
					{
						Name:     "shape",
						Optional: true,
						Schema: ir.SchemaIR{
							Union: &ir.UnionType{
								Variants: []ir.SchemaIR{
									{
										Attributes: []ir.AttributeIR{
											{Name: "union_nested", Schema: ir.SchemaIR{Type: ir.TypeString}},
										},
									},
								},
							},
						},
					},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:              "profile",
			ComputedAttributes:  []string{"nested_field"},
			ForceNew:            []string{"list_nested"},
			SensitiveAttributes: []string{"union_nested"},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	root := provider.Resources[0].Schema

	// Object-attribute nested attribute.
	contact, ok := findAttr(root.Attributes, "contact")
	if !ok {
		t.Fatalf("attribute %q not found", "contact")
	}
	nested, ok := findAttr(contact.Schema.Attributes, "nested_field")
	if !ok {
		t.Fatalf("attribute %q not found", "nested_field")
	}
	if !nested.Computed {
		t.Errorf("nested_field Computed = false, want true (forced by computed_attributes)")
	}
	if nested.Required {
		t.Errorf("nested_field Required = true, want false (computed_attributes clears Required)")
	}

	// List-element nested attribute.
	tags, ok := findAttr(root.Attributes, "tags")
	if !ok {
		t.Fatalf("attribute %q not found", "tags")
	}
	listNested, ok := findAttr(tags.Schema.Collection.ElementType.Attributes, "list_nested")
	if !ok {
		t.Fatalf("attribute %q not found", "list_nested")
	}
	if !listNested.ForceNew {
		t.Errorf("list_nested ForceNew = false, want true (forced by force_new)")
	}

	// Union-variant nested attribute.
	shape, ok := findAttr(root.Attributes, "shape")
	if !ok {
		t.Fatalf("attribute %q not found", "shape")
	}
	unionNested, ok := findAttr(shape.Schema.Union.Variants[0].Attributes, "union_nested")
	if !ok {
		t.Fatalf("attribute %q not found", "union_nested")
	}
	if !unionNested.Sensitive {
		t.Errorf("union_nested Sensitive = false, want true (forced by sensitive_attributes)")
	}
}

func TestApplyOverrides_DatasourceName(t *testing.T) {
	provider := &ir.ProviderIR{
		DataSources: []ir.DataSourceIR{{
			Name:            "getPetById",
			TypeName:        "getPetById",
			SourceOperation: "getPetById",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		DatasourceOverrides: []config.DatasourceOverride{{
			Operation:      "getPetById",
			DatasourceName: "pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.DataSources[0].Name != "pet" {
		t.Errorf("datasource Name = %q, want %q", provider.DataSources[0].Name, "pet")
	}
	if provider.DataSources[0].TypeName != "pet" {
		t.Errorf("datasource TypeName = %q, want %q", provider.DataSources[0].TypeName, "pet")
	}
	if provider.DataSources[0].FullName != "Pet" {
		t.Errorf("datasource FullName = %q, want %q", provider.DataSources[0].FullName, "Pet")
	}
}

func TestApplyOverrides_DatasourceByNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		DataSources: []ir.DataSourceIR{{
			Name:     "getPetById",
			TypeName: "getPetById",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		DatasourceOverrides: []config.DatasourceOverride{{
			Name:           "getPetById",
			DatasourceName: "pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.DataSources[0].Name != "pet" {
		t.Errorf("datasource Name = %q, want %q", provider.DataSources[0].Name, "pet")
	}
	if provider.DataSources[0].TypeName != "pet" {
		t.Errorf("datasource TypeName = %q, want %q", provider.DataSources[0].TypeName, "pet")
	}
}

func TestApplyOverrides_ActionOverride(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:            "reboot_pet",
			TypeName:        "reboot_pet",
			Description:     "rebootPet",
			SourceOperation: "rebootPet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation:        "rebootPet",
			Name:             "reboot_server",
			Description:      "Reboots the specified server",
			ProgressMessages: true,
			ModifyPlan:       true,
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	a := provider.Actions[0]
	if a.Name != "reboot_server" {
		t.Errorf("action Name = %q, want %q", a.Name, "reboot_server")
	}
	if a.Description != "Reboots the specified server" {
		t.Errorf("action Description = %q, want %q", a.Description, "Reboots the specified server")
	}
	if !a.ProgressMessages {
		t.Errorf("action ProgressMessages = false, want true")
	}
	if !a.ModifyPlan {
		t.Errorf("action ModifyPlan = false, want true")
	}
}

func TestApplyOverrides_ActionOverrideCaseAndWhitespace(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:            "reboot_pet",
			TypeName:        "reboot_pet",
			SourceOperation: "rebootPet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "  REBOOTPET  ",
			Name:      "reboot_server",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Actions[0].Name != "reboot_server" {
		t.Errorf("action Name = %q, want %q", provider.Actions[0].Name, "reboot_server")
	}
}

func TestApplyOverrides_ActionOverrideNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:     "reboot_pet",
			TypeName: "reboot_pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "reboot_pet",
			Name:      "reboot_server",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Actions[0].Name != "reboot_server" {
		t.Errorf("action Name = %q, want %q", provider.Actions[0].Name, "reboot_server")
	}
}

// TestApplyOverrides_ActionOverrideByMethodPath verifies that an override
// operation in "METHOD /path" form matches only the action whose invoke mapping
// has that method and path, disambiguating two operations that share an
// operationId (a duplicate operationId in the spec).
func TestApplyOverrides_ActionOverrideByMethodPath(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{
			{
				Name:            "redefine_snmp_throttle_config",
				TypeName:        "redefine_snmp_throttle_config",
				SourceOperation: "redefineSnmpThrottleConfig",
				InvokeMapping:   ir.OperationMappingIR{Method: "PUT", PathTemplate: "/snmp/throttle"},
			},
			{
				Name:            "redefine_snmp_throttle_config",
				TypeName:        "redefine_snmp_throttle_config",
				SourceOperation: "redefineSnmpThrottleConfig",
				InvokeMapping:   ir.OperationMappingIR{Method: "PUT", PathTemplate: "/snmp/throttle/{id}"},
			},
		},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "PUT /snmp/throttle",
			Name:      "redefine_snmp_throttle",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Actions[0].Name; got != "redefine_snmp_throttle" {
		t.Errorf("action[0] Name = %q, want %q (matched by method+path)", got, "redefine_snmp_throttle")
	}
	if got := provider.Actions[1].Name; got != "redefine_snmp_throttle_config" {
		t.Errorf("action[1] Name = %q, want %q (must not match a different path)", got, "redefine_snmp_throttle_config")
	}
}

// TestApplyOverrides_ActionOverrideMethodPathCaseInsensitive verifies the
// method+path form tolerates case and whitespace differences, matching the
// existing operationId matching semantics.
func TestApplyOverrides_ActionOverrideMethodPathCaseInsensitive(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:            "upload_certificate",
			TypeName:        "upload_certificate",
			SourceOperation: "uploadCertificate",
			InvokeMapping:   ir.OperationMappingIR{Method: "POST", PathTemplate: "/certificates/upload"},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "  post /certificates/upload  ",
			Name:      "upload_cert",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if got := provider.Actions[0].Name; got != "upload_cert" {
		t.Errorf("action Name = %q, want %q", got, "upload_cert")
	}
}

func TestApplyOverrides_EphemeralOverride(t *testing.T) {
	provider := &ir.ProviderIR{
		EphemeralResources: []ir.EphemeralResourceIR{{
			Name:            "generateTemporaryCredentials",
			TypeName:        "generate_temporary_credentials",
			SourceOperation: "generateTemporaryCredentials",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		EphemeralOverrides: []config.EphemeralOverride{{
			Operation:   "generateTemporaryCredentials",
			Name:        "temporary_credential",
			Description: "Generates short-lived credentials",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	e := provider.EphemeralResources[0]
	if e.Name != "temporary_credential" {
		t.Errorf("ephemeral Name = %q, want %q", e.Name, "temporary_credential")
	}
	if e.Description != "Generates short-lived credentials" {
		t.Errorf("ephemeral Description = %q, want %q", e.Description, "Generates short-lived credentials")
	}
}

func TestApplyOverrides_EphemeralOverrideNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		EphemeralResources: []ir.EphemeralResourceIR{{
			Name:     "generate_temporary_credentials",
			TypeName: "generate_temporary_credentials",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		EphemeralOverrides: []config.EphemeralOverride{{
			Operation: "generate_temporary_credentials",
			Name:      "temporary_credential",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.EphemeralResources[0].Name != "temporary_credential" {
		t.Errorf("ephemeral Name = %q, want %q", provider.EphemeralResources[0].Name, "temporary_credential")
	}
}

func TestApplyOverrides_ListResourceOverride(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:            "pets",
			TypeName:        "pets",
			SourceOperation: "listPets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			Pagination: &config.PaginationConfig{
				Style: "offset",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "offset" {
		t.Errorf("list resource PaginationStyle = %q, want %q", provider.ListResources[0].PaginationStyle, "offset")
	}
}

func TestApplyOverrides_ListResourceConfigSchema(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:            "pets",
			TypeName:        "pets",
			SourceOperation: "listPets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			ConfigSchema: []config.ListConfigSchema{
				{Name: "status", Type: "string", Description: "Filter by status"},
				{Name: "limit", Type: "integer", Optional: boolPtr(true)},
				{Name: "enabled", Type: "boolean"},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.ListResources[0].ConfigSchema.Attributes
	if len(attrs) != 3 {
		t.Fatalf("attribute count = %d, want 3", len(attrs))
	}

	cases := []struct {
		idx      int
		name     string
		typ      ir.PrimitiveType
		required bool
		desc     string
	}{
		{0, "status", ir.TypeString, true, "Filter by status"},
		{1, "limit", ir.TypeInt, false, ""},
		{2, "enabled", ir.TypeBool, true, ""},
	}
	for _, tc := range cases {
		a := attrs[tc.idx]
		if a.Name != tc.name {
			t.Errorf("attribute[%d].Name = %q, want %q", tc.idx, a.Name, tc.name)
		}
		if a.Schema.Type != tc.typ {
			t.Errorf("attribute[%d].Schema.Type = %v, want %v", tc.idx, a.Schema.Type, tc.typ)
		}
		if a.Required != tc.required {
			t.Errorf("attribute[%d].Required = %v, want %v", tc.idx, a.Required, tc.required)
		}
		if a.Description != tc.desc {
			t.Errorf("attribute[%d].Description = %q, want %q", tc.idx, a.Description, tc.desc)
		}
	}
}

func TestApplyOverrides_ListResourceConfigSchemaUpdatesExisting(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "pets",
			TypeName: "pets",
			ConfigSchema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "status", Schema: ir.SchemaIR{Type: ir.TypeString}, Required: true, Description: "old"},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			ConfigSchema: []config.ListConfigSchema{
				{Name: "status", Type: "string", Optional: boolPtr(true), Description: "Filter by status"},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.ListResources[0].ConfigSchema.Attributes
	if len(attrs) != 1 {
		t.Fatalf("attribute count = %d, want 1", len(attrs))
	}
	if !attrs[0].Optional {
		t.Errorf("existing attribute Optional = false, want true")
	}
	if attrs[0].Description != "Filter by status" {
		t.Errorf("existing attribute Description = %q, want %q", attrs[0].Description, "Filter by status")
	}
}

// TestApplyOverrides_ListResourceConfigSchemaDescriptionOnlyPreservesOptional
// locks in the M-7 fix: a config_schema entry that sets only `description` (no
// `optional:` key) must not flip an existing spec-optional filter to Required.
// Before the fix `required := !override.Optional` on a bare bool treated the
// omitted key as `optional: false` and unconditionally overwrote
// Required/Optional on the in-place update.
func TestApplyOverrides_ListResourceConfigSchemaDescriptionOnlyPreservesOptional(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "pets",
			TypeName: "pets",
			ConfigSchema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					// Spec-optional filter, as inferred from the OpenAPI query
					// parameter.
					{Name: "status", Schema: ir.SchemaIR{Type: ir.TypeString}, Optional: true, Description: "spec text"},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			ConfigSchema: []config.ListConfigSchema{
				// Description-only override: `optional` omitted.
				{Name: "status", Type: "string", Description: "Filter by status"},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.ListResources[0].ConfigSchema.Attributes
	if len(attrs) != 1 {
		t.Fatalf("attribute count = %d, want 1", len(attrs))
	}
	if attrs[0].Required {
		t.Errorf("description-only override flipped existing optional filter to Required (M-7)")
	}
	if !attrs[0].Optional {
		t.Errorf("existing attribute Optional = false, want true (M-7)")
	}
	if attrs[0].Description != "Filter by status" {
		t.Errorf("existing attribute Description = %q, want %q", attrs[0].Description, "Filter by status")
	}
}

// TestApplyOverrides_ListResourceConfigSchemaExplicitOptionalFalse locks in the
// M-7 counterpart: an explicit `optional: false` still flips the existing
// attribute to Required — the pointer distinguishes "omitted" from "false".
func TestApplyOverrides_ListResourceConfigSchemaExplicitOptionalFalse(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "pets",
			TypeName: "pets",
			ConfigSchema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{Name: "status", Schema: ir.SchemaIR{Type: ir.TypeString}, Optional: true, Description: "spec text"},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			ConfigSchema: []config.ListConfigSchema{
				{Name: "status", Type: "string", Optional: boolPtr(false)},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.ListResources[0].ConfigSchema.Attributes
	if len(attrs) != 1 {
		t.Fatalf("attribute count = %d, want 1", len(attrs))
	}
	if !attrs[0].Required {
		t.Errorf("explicit optional: false must flip existing attribute to Required")
	}
	if attrs[0].Optional {
		t.Errorf("explicit optional: false must clear Optional")
	}
}

func TestApplyOverrides_ListResourceByOperation(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{
			{Name: "pets", TypeName: "pets", SourceOperation: "listPets"},
			{Name: "dogs", TypeName: "dogs", SourceOperation: "listDogs"},
		},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Operation: "listPets",
			Pagination: &config.PaginationConfig{
				Style: "cursor",
			},
			ConfigSchema: []config.ListConfigSchema{
				{Name: "query", Type: "string", Optional: boolPtr(true)},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "cursor" {
		t.Errorf("matched list resource PaginationStyle = %q, want cursor", provider.ListResources[0].PaginationStyle)
	}
	if len(provider.ListResources[0].ConfigSchema.Attributes) != 1 {
		t.Errorf("matched list resource config attribute count = %d, want 1", len(provider.ListResources[0].ConfigSchema.Attributes))
	}
	if provider.ListResources[1].PaginationStyle != "" {
		t.Errorf("unmatched list resource PaginationStyle = %q, want empty", provider.ListResources[1].PaginationStyle)
	}
	if len(provider.ListResources[1].ConfigSchema.Attributes) != 0 {
		t.Errorf("unmatched list resource config attribute count = %d, want 0", len(provider.ListResources[1].ConfigSchema.Attributes))
	}
}

func TestApplyOverrides_ListResourceOperationNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "pets",
			TypeName: "pets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Operation: "pets",
			Pagination: &config.PaginationConfig{
				Style: "offset",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "offset" {
		t.Errorf("list resource PaginationStyle = %q, want offset", provider.ListResources[0].PaginationStyle)
	}
}

func TestApplyOverrides_ListResourceOperationPrecedence(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{
			{Name: "pets", TypeName: "pets", SourceOperation: "listPets"},
			{Name: "dogs", TypeName: "dogs", SourceOperation: "listDogs"},
		},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource:  "dogs",
			Operation: "listPets",
			Pagination: &config.PaginationConfig{
				Style: "link_header",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "link_header" {
		t.Errorf("pets PaginationStyle = %q, want link_header", provider.ListResources[0].PaginationStyle)
	}
	if provider.ListResources[1].PaginationStyle != "" {
		t.Errorf("dogs PaginationStyle = %q, want empty", provider.ListResources[1].PaginationStyle)
	}
}

func TestApplyOverrides_ListResourceOverrideByFullNameWhenResourceMatches(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "other",
			TypeName: "other",
			FullName: "list_pets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "listpets",
			Pagination: &config.PaginationConfig{
				Style: "offset",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "offset" {
		t.Errorf("list resource PaginationStyle = %q, want %q", provider.ListResources[0].PaginationStyle, "offset")
	}
}

func TestApplyOverrides_ListResourceOverrideByFullNameOnly(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "",
			TypeName: "",
			FullName: "list_pets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "listpets",
			Pagination: &config.PaginationConfig{
				Style: "offset",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "offset" {
		t.Errorf("list resource PaginationStyle = %q, want %q", provider.ListResources[0].PaginationStyle, "offset")
	}
}

func TestApplyOverrides_ListResourceByOperationFullNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "other",
			TypeName: "other",
			FullName: "list_pets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Operation: "list_pets",
			Pagination: &config.PaginationConfig{
				Style: "cursor",
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.ListResources[0].PaginationStyle != "cursor" {
		t.Errorf("list resource PaginationStyle = %q, want %q", provider.ListResources[0].PaginationStyle, "cursor")
	}
}

func TestApplyOverrides_ListResourceConfigSchemaPascalCase(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{{
			Name:     "pets",
			TypeName: "pets",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ListResourceOverrides: []config.ListResourceOverride{{
			Resource: "pets",
			ConfigSchema: []config.ListConfigSchema{
				{Name: "StatusFilter", Type: "string"},
			},
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	attrs := provider.ListResources[0].ConfigSchema.Attributes
	if len(attrs) != 1 {
		t.Fatalf("attribute count = %d, want 1", len(attrs))
	}
	if attrs[0].Name != "status_filter" {
		t.Errorf("attribute Name = %q, want %q", attrs[0].Name, "status_filter")
	}
}

func TestPrimitiveTypeFromConfig(t *testing.T) {
	cases := []struct {
		input string
		want  ir.PrimitiveType
	}{
		{"string", ir.TypeString},
		{"integer", ir.TypeInt},
		{"int", ir.TypeInt},
		{"number", ir.TypeFloat},
		{"float", ir.TypeFloat},
		{"boolean", ir.TypeBool},
		{"bool", ir.TypeBool},
		{"null", ir.TypeNull},
		{"", ir.TypeDynamic},
		{"unknown", ir.TypeDynamic},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := primitiveTypeFromConfig(tc.input)
			if got != tc.want {
				t.Errorf("primitiveTypeFromConfig(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestApplyOverrides_FunctionOverride(t *testing.T) {
	provider := &ir.ProviderIR{
		Functions: []ir.FunctionIR{{
			Name:            "ip_lookup",
			TypeName:        "ip_lookup",
			SourceOperation: "ipLookup",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		FunctionOverrides: []config.FunctionOverride{{
			Operation: "ipLookup",
			Name:      "lookup_ip",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Functions[0].Name != "lookup_ip" {
		t.Errorf("function Name = %q, want %q", provider.Functions[0].Name, "lookup_ip")
	}
}

func TestApplyOverrides_FunctionOverrideCaseInsensitive(t *testing.T) {
	provider := &ir.ProviderIR{
		Functions: []ir.FunctionIR{{
			Name:            "ip_lookup",
			TypeName:        "ip_lookup",
			SourceOperation: "ipLookup",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		FunctionOverrides: []config.FunctionOverride{{
			Operation: "IPLOOKUP",
			Name:      "lookup_ip",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Functions[0].Name != "lookup_ip" {
		t.Errorf("function Name = %q, want %q", provider.Functions[0].Name, "lookup_ip")
	}
}

func TestApplyOverrides_FunctionOverrideNameFallback(t *testing.T) {
	provider := &ir.ProviderIR{
		Functions: []ir.FunctionIR{{
			Name:     "ip_lookup",
			TypeName: "ip_lookup",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		FunctionOverrides: []config.FunctionOverride{{
			Operation: "ip_lookup",
			Name:      "lookup_ip",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Functions[0].Name != "lookup_ip" {
		t.Errorf("function Name = %q, want %q", provider.Functions[0].Name, "lookup_ip")
	}
}

func TestApplyOverrides_EmptyProvider(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Naming: &config.NamingConfig{
			ResourcePrefix:   "mycloud_",
			DatasourcePrefix: "mycloud_",
		},
		ResourceOverrides: []config.ResourceOverride{{
			Schema: "Pet",
			Skip:   boolPtr(true),
		}},
		GlobalTimeouts: &config.TimeoutConfig{
			Create: ptrConfigDuration(20 * time.Minute),
		},
	}

	t.Run("nil provider", func(t *testing.T) {
		if err := ApplyOverrides(nil, cfg); err != nil {
			t.Errorf("ApplyOverrides(nil, cfg) = %v, want nil", err)
		}
	})

	t.Run("empty slices", func(t *testing.T) {
		provider := &ir.ProviderIR{}
		if err := ApplyOverrides(provider, cfg); err != nil {
			t.Fatalf("ApplyOverrides() = %v, want nil", err)
		}
		if len(provider.Resources) != 0 {
			t.Errorf("Resources = %v, want empty", provider.Resources)
		}
		if len(provider.DataSources) != 0 {
			t.Errorf("DataSources = %v, want empty", provider.DataSources)
		}
	})
}

func TestApplyOverrides_NamingPrefixSuffix(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			FullName: "Pet",
		}},
		DataSources: []ir.DataSourceIR{{
			Name:     "pet",
			TypeName: "pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Naming: &config.NamingConfig{
			ResourcePrefix:   "mycloud_",
			DatasourcePrefix: "mycloud_",
			ResourceSuffix:   "_v1",
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.Name != "mycloud_pet_v1" {
		t.Errorf("resource Name = %q, want %q", r.Name, "mycloud_pet_v1")
	}
	if r.TypeName != "mycloud_pet_v1" {
		t.Errorf("resource TypeName = %q, want %q", r.TypeName, "mycloud_pet_v1")
	}

	ds := provider.DataSources[0]
	if ds.Name != "mycloud_pet" {
		t.Errorf("datasource Name = %q, want %q", ds.Name, "mycloud_pet")
	}
}

func TestApplyOverrides_PolymorphismVariantNames(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "Cat", TypeName: "cat"},
			{Name: "Dog", TypeName: "dog"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "Cat", TypeName: "cat"},
		},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Polymorphism: &config.PolymorphismConfig{
			Strategy: "split_resources",
			OneOf: []config.OneOfOverride{{
				Schema: "Pet",
				Variants: []config.Variant{
					{Schema: "Cat", ResourceName: "feline", DatasourceName: "felines"},
					{Schema: "Dog", ResourceName: "canine"},
				},
			}},
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Resources[0].Name != "feline" {
		t.Errorf("Cat resource Name = %q, want %q", provider.Resources[0].Name, "feline")
	}
	if provider.Resources[1].Name != "canine" {
		t.Errorf("Dog resource Name = %q, want %q", provider.Resources[1].Name, "canine")
	}
	if provider.DataSources[0].Name != "felines" {
		t.Errorf("Cat datasource Name = %q, want %q", provider.DataSources[0].Name, "felines")
	}
	// L-102: FullName must stay consistent with Name after variant rename.
	if provider.Resources[0].FullName != "Feline" {
		t.Errorf("Cat resource FullName = %q, want %q", provider.Resources[0].FullName, "Feline")
	}
	if provider.DataSources[0].FullName != "Felines" {
		t.Errorf("Cat datasource FullName = %q, want %q", provider.DataSources[0].FullName, "Felines")
	}
}

// TestApplyOverrides_PolymorphismVariantNamesNoSplitResources locks in the
// L-102 fix: when split_resources is NOT the selected strategy, variant name
// overrides must not rename entities that merely share a name with a variant
// schema.
func TestApplyOverrides_PolymorphismVariantNamesNoSplitResources(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{
			{Name: "Cat", TypeName: "cat", FullName: "Cat"},
			{Name: "Dog", TypeName: "dog", FullName: "Dog"},
		},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Polymorphism: &config.PolymorphismConfig{
			Strategy: "single_resource",
			OneOf: []config.OneOfOverride{{
				Schema: "Pet",
				Variants: []config.Variant{
					{Schema: "Cat", ResourceName: "feline"},
					{Schema: "Dog", ResourceName: "canine"},
				},
			}},
		},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Resources[0].Name != "Cat" {
		t.Errorf("Cat resource Name = %q, want %q (no rename without split_resources)", provider.Resources[0].Name, "Cat")
	}
	if provider.Resources[1].Name != "Dog" {
		t.Errorf("Dog resource Name = %q, want %q (no rename without split_resources)", provider.Resources[1].Name, "Dog")
	}
}

func TestApplyOverrides_PolymorphismStrategyOverride(t *testing.T) {
	schema := &ir.SchemaIR{
		Name: "Pet",
		Union: &ir.UnionType{
			Kind: ir.OneOf,
			Variants: []ir.SchemaIR{
				{Name: "Cat", Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
				{Name: "Dog", Attributes: []ir.AttributeIR{{Name: "id", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	// Global split_resources strategy should be selected for a top-level oneOf
	// with named, object-like variants.
	cfg := PolymorphismConfig{Strategy: "split_resources"}
	strategy, err := SelectStrategy(schema, ContextTopLevelResource, cfg)
	if err != nil {
		t.Fatalf("SelectStrategy() = %v, want nil", err)
	}
	if strategy != StrategySplitResources {
		t.Errorf("strategy = %q, want %q", strategy, StrategySplitResources)
	}

	// Per-schema dynamic_union override should beat global default.
	perSchemaCfg := PolymorphismConfig{
		Strategy: "split_resources",
		OneOf: []PolymorphismOneOfConfig{{
			Schema:   "Pet",
			Strategy: "dynamic_union",
		}},
	}
	strategy, err = SelectStrategy(schema, ContextTopLevelResource, perSchemaCfg)
	if err != nil {
		t.Fatalf("SelectStrategy() = %v, want nil", err)
	}
	if strategy != StrategyDynamicUnion {
		t.Errorf("strategy = %q, want %q", strategy, StrategyDynamicUnion)
	}
}

func TestToPolymorphismConfig(t *testing.T) {
	cfg := &config.PolymorphismConfig{
		Strategy: "split_resources",
		OneOf: []config.OneOfOverride{{
			Schema: "Pet",
			Variants: []config.Variant{
				{Schema: "Cat", ResourceName: "feline", DatasourceName: "felines"},
			},
		}},
	}

	poly := toPolymorphismConfig(cfg)
	if poly.Strategy != "split_resources" {
		t.Errorf("Strategy = %q, want %q", poly.Strategy, "split_resources")
	}
	if len(poly.OneOf) != 1 {
		t.Fatalf("OneOf count = %d, want 1", len(poly.OneOf))
	}
	if poly.OneOf[0].Schema != "Pet" {
		t.Errorf("OneOf schema = %q, want %q", poly.OneOf[0].Schema, "Pet")
	}
	if len(poly.OneOf[0].Variants) != 1 {
		t.Fatalf("variant count = %d, want 1", len(poly.OneOf[0].Variants))
	}
	v := poly.OneOf[0].Variants[0]
	if v.Schema != "Cat" || v.ResourceName != "feline" || v.DataSourceName != "felines" {
		t.Errorf("variant = %+v, want Cat/feline/felines", v)
	}
}

func TestApplyOverrides_ByOperationCaseAndWhitespace(t *testing.T) {
	cases := []struct {
		name       string
		provider   *ir.ProviderIR
		cfg        *config.Config
		entityName string
		wantName   string
	}{
		{
			name: "resource",
			provider: &ir.ProviderIR{Resources: []ir.ResourceIR{{
				Name: "pet", TypeName: "pet", SourceOperation: "createPet",
			}}},
			cfg: &config.Config{
				Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
				ResourceOverrides: []config.ResourceOverride{{
					Operation: "  CREATEPET  ", ResourceName: "renamed_pet",
				}},
			},
			entityName: "resource",
			wantName:   "renamed_pet",
		},
		{
			name: "datasource",
			provider: &ir.ProviderIR{DataSources: []ir.DataSourceIR{{
				Name: "getPetById", TypeName: "getPetById", SourceOperation: "getPetById",
			}}},
			cfg: &config.Config{
				Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
				DatasourceOverrides: []config.DatasourceOverride{{
					Operation: "  GETPETBYID  ", DatasourceName: "pet",
				}},
			},
			entityName: "datasource",
			wantName:   "pet",
		},
		{
			name: "action",
			provider: &ir.ProviderIR{Actions: []ir.ActionIR{{
				Name: "reboot_pet", TypeName: "reboot_pet", SourceOperation: "rebootPet",
			}}},
			cfg: &config.Config{
				Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
				ActionOverrides: []config.ActionOverride{{
					Operation: "  REBOOTPET  ", Name: "reboot_server",
				}},
			},
			entityName: "action",
			wantName:   "reboot_server",
		},
		{
			name: "ephemeral",
			provider: &ir.ProviderIR{EphemeralResources: []ir.EphemeralResourceIR{{
				Name: "generate_temporary_credentials", TypeName: "generate_temporary_credentials",
				SourceOperation: "generateTemporaryCredentials",
			}}},
			cfg: &config.Config{
				Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
				EphemeralOverrides: []config.EphemeralOverride{{
					Operation: "  GENERATETEMPORARYCREDENTIALS  ", Name: "temporary_credential",
				}},
			},
			entityName: "ephemeral",
			wantName:   "temporary_credential",
		},
		{
			name: "function",
			provider: &ir.ProviderIR{Functions: []ir.FunctionIR{{
				Name: "ip_lookup", TypeName: "ip_lookup", SourceOperation: "ipLookup",
			}}},
			cfg: &config.Config{
				Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
				FunctionOverrides: []config.FunctionOverride{{
					Operation: "  IPLOOKUP  ", Name: "lookup_ip",
				}},
			},
			entityName: "function",
			wantName:   "lookup_ip",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyOverrides(tc.provider, tc.cfg); err != nil {
				t.Fatalf("ApplyOverrides() = %v, want nil", err)
			}

			var got string
			switch tc.entityName {
			case "resource":
				got = tc.provider.Resources[0].Name
			case "datasource":
				got = tc.provider.DataSources[0].Name
			case "action":
				got = tc.provider.Actions[0].Name
			case "ephemeral":
				got = tc.provider.EphemeralResources[0].Name
			case "function":
				got = tc.provider.Functions[0].Name
			}
			if got != tc.wantName {
				t.Errorf("%s Name = %q, want %q", tc.entityName, got, tc.wantName)
			}
		})
	}
}

func TestApplyOverrides_DatasourceFullNameOverwrite(t *testing.T) {
	provider := &ir.ProviderIR{
		DataSources: []ir.DataSourceIR{{
			Name:     "getPetById",
			TypeName: "getPetById",
			FullName: "Old Pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		DatasourceOverrides: []config.DatasourceOverride{{
			Name:           "getPetById",
			DatasourceName: "pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.DataSources[0].FullName != "Pet" {
		t.Errorf("datasource FullName = %q, want %q", provider.DataSources[0].FullName, "Pet")
	}
}

func TestApplyOverrides_ActionFullNameOverwrite(t *testing.T) {
	provider := &ir.ProviderIR{
		Actions: []ir.ActionIR{{
			Name:     "reboot_pet",
			TypeName: "reboot_pet",
			FullName: "Old Pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ActionOverrides: []config.ActionOverride{{
			Operation: "reboot_pet",
			Name:      "reboot_server",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	if provider.Actions[0].FullName != "Reboot Server" {
		t.Errorf("action FullName = %q, want %q", provider.Actions[0].FullName, "Reboot Server")
	}
}

func TestApplyOverrides_NamingPrefixBlocksSchemaMatch(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			FullName: "Pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Naming: &config.NamingConfig{
			ResourcePrefix: "mycloud_",
		},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:       "Pet",
			ResourceName: "custom_pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.Name != "mycloud_pet" {
		t.Errorf("resource Name = %q, want %q", r.Name, "mycloud_pet")
	}
	if r.TypeName != "mycloud_pet" {
		t.Errorf("resource TypeName = %q, want %q", r.TypeName, "mycloud_pet")
	}
	if r.FullName != "mycloud_Pet" {
		t.Errorf("resource FullName = %q, want %q", r.FullName, "mycloud_Pet")
	}
}

func TestApplyOverrides_NamingPrefixAllowsSchemaMatch(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			FullName: "Pet",
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		Naming: &config.NamingConfig{
			ResourcePrefix: "mycloud_",
		},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:       "mycloud_pet",
			ResourceName: "custom_pet",
		}},
	}

	if err := ApplyOverrides(provider, cfg); err != nil {
		t.Fatalf("ApplyOverrides() = %v, want nil", err)
	}

	r := provider.Resources[0]
	if r.Name != "custom_pet" {
		t.Errorf("resource Name = %q, want %q", r.Name, "custom_pet")
	}
	if r.TypeName != "custom_pet" {
		t.Errorf("resource TypeName = %q, want %q", r.TypeName, "custom_pet")
	}
	if r.FullName != "Custom Pet" {
		t.Errorf("resource FullName = %q, want %q", r.FullName, "Custom Pet")
	}
}

func TestSetAttributeFlag_UnknownFlagReturnsError(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}},
	}

	err := setAttributeFlag(obj, []string{"name"}, "required")
	if err == nil {
		t.Fatalf("expected error for unknown flag, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown attribute flag") {
		t.Errorf("error message %q does not contain 'unknown attribute flag'", msg)
	}
	if !strings.Contains(msg, "name") {
		t.Errorf("error message %q does not contain attribute name 'name'", msg)
	}
}

// polymorphicPetResource returns a managed resource whose schema is a
// top-level discriminated oneOf (Pet = oneOf[Cat, Dog]), as synthesized by
// ManagedResourceSchema: a single Computed wrapper attribute carrying the
// union.
func polymorphicPetResource() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		FullName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "pet",
					Computed: true,
					Schema: ir.SchemaIR{
						Name:     "Pet",
						Computed: true,
						Union: &ir.UnionType{
							Kind: ir.OneOf,
							Variants: []ir.SchemaIR{
								// Variant attributes are snake_cased by the schema
								// conversion, as in the live pipeline; the raw
								// discriminator PropertyName stays camelCase.
								{Name: "Cat", Attributes: []ir.AttributeIR{
									{Name: "pet_type", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "lives_remaining", Schema: ir.SchemaIR{Type: ir.TypeInt}},
								}},
								{Name: "Dog", Attributes: []ir.AttributeIR{
									{Name: "pet_type", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "bark_volume", Schema: ir.SchemaIR{Type: ir.TypeInt}},
								}},
							},
							Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
						},
					},
				},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
		},
		SourceOperation: "getPet",
	}
}

// TestApplyPolymorphismOverrides_SplitResources_Synthesizes asserts the D3
// wiring: a top-level polymorphic resource is replaced by one resource per
// variant when the split_resources strategy is selected — each variant keeps
// the original CRUD mapping and provider-name affixes, and the discriminator
// attribute is removed from the variant schemas. The first case exercises the
// explicit global strategy, the second the named-object-variants heuristic
// (no strategy configured).
func TestApplyPolymorphismOverrides_SplitResources_Synthesizes(t *testing.T) {
	run := func(t *testing.T, cfg *config.Config) *ir.ProviderIR {
		t.Helper()
		provider := &ir.ProviderIR{Resources: []ir.ResourceIR{polymorphicPetResource()}}
		if err := ApplyOverrides(provider, cfg); err != nil {
			t.Fatalf("ApplyOverrides() error = %v", err)
		}
		return provider
	}
	assertSplit := func(t *testing.T, provider *ir.ProviderIR) {
		t.Helper()
		if len(provider.Resources) != 2 {
			t.Fatalf("resources = %d, want 2 (one per variant)", len(provider.Resources))
		}
		got := map[string]ir.ResourceIR{}
		for _, r := range provider.Resources {
			got[r.Name] = r
		}
		for _, want := range []string{"cat", "dog"} {
			r, ok := got[want]
			if !ok {
				t.Fatalf("variant resource %q missing, got %v", want, got)
			}
			if r.TypeName != "mycloud_"+want {
				t.Errorf("variant %q TypeName = %q, want mycloud_%s", want, r.TypeName, want)
			}
			if r.CRUDMapping.Read.PathTemplate != "/pets/{id}" {
				t.Errorf("variant %q lost the original CRUD mapping: %+v", want, r.CRUDMapping)
			}
			if r.SourceOperation != "getPet" {
				t.Errorf("variant %q SourceOperation = %q, want getPet", want, r.SourceOperation)
			}
			for _, a := range r.Schema.Attributes {
				if a.Name == "petType" || a.Name == "pet_type" {
					t.Errorf("variant %q schema must drop the discriminator attribute, got %+v", want, r.Schema.Attributes)
				}
			}
		}
	}

	t.Run("explicit global strategy", func(t *testing.T) {
		assertSplit(t, run(t, &config.Config{Polymorphism: &config.PolymorphismConfig{
			Strategy: "split_resources",
		}}))
	})
	t.Run("heuristic default (named object variants)", func(t *testing.T) {
		assertSplit(t, run(t, &config.Config{Polymorphism: &config.PolymorphismConfig{}}))
	})

	t.Run("dynamic_union keeps the polymorphic resource", func(t *testing.T) {
		provider := run(t, &config.Config{Polymorphism: &config.PolymorphismConfig{
			Strategy: "dynamic_union",
		}})
		if len(provider.Resources) != 1 || provider.Resources[0].Name != "pet" {
			t.Fatalf("dynamic_union must keep the original resource, got %+v", provider.Resources)
		}
	})

	t.Run("non-polymorphic resources untouched", func(t *testing.T) {
		provider := &ir.ProviderIR{Resources: []ir.ResourceIR{{
			Name:     "store",
			TypeName: "mycloud_store",
			Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
				{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
			}},
		}}}
		if err := ApplyOverrides(provider, &config.Config{Polymorphism: &config.PolymorphismConfig{
			Strategy: "split_resources",
		}}); err != nil {
			t.Fatalf("ApplyOverrides() error = %v", err)
		}
		if len(provider.Resources) != 1 || provider.Resources[0].Name != "store" {
			t.Fatalf("non-polymorphic resource must be kept unchanged, got %+v", provider.Resources)
		}
	})
}

// TestOverrideDescriptionDoesNotEraseSpecText pins the guard that keeps a
// generator.yaml entry from blanking the description the OpenAPI spec supplied.
// `description` is omitempty in both override shapes, so an entry that sets only
// e.g. `sensitive: true` decodes with Description == "" and an unconditional
// assignment would silently drop the spec's text.
func TestOverrideDescriptionDoesNotEraseSpecText(t *testing.T) {
	const specText = "Display name of the pet."

	t.Run("write-only attribute keeps spec description", func(t *testing.T) {
		obj := &ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "name", Description: specText, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}}
		addWriteOnlyAttributes(obj, []config.WriteOnlyAttribute{{Name: "name", Sensitive: true}})
		attr, ok := findAttr(obj.Attributes, "name")
		if !ok {
			t.Fatal("name attribute not found")
		}
		if attr.Description != specText {
			t.Errorf("description = %q, want the spec text %q", attr.Description, specText)
		}
		if !attr.Sensitive {
			t.Error("override should still have applied Sensitive")
		}
	})

	t.Run("write-only attribute honors an explicit description", func(t *testing.T) {
		obj := &ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "name", Description: specText, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}}
		addWriteOnlyAttributes(obj, []config.WriteOnlyAttribute{{Name: "name", Description: "Override wins."}})
		attr, _ := findAttr(obj.Attributes, "name")
		if attr.Description != "Override wins." {
			t.Errorf("description = %q, want the override text", attr.Description)
		}
	})

	t.Run("list config schema keeps spec description", func(t *testing.T) {
		lr := &ir.ListResourceIR{ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "limit", Description: specText, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		}}}
		applyListResourceConfigSchema(lr, []config.ListConfigSchema{{Name: "limit", Type: "int64", Optional: boolPtr(true)}})
		attr, ok := findAttr(lr.ConfigSchema.Attributes, "limit")
		if !ok {
			t.Fatal("limit attribute not found")
		}
		if attr.Description != specText {
			t.Errorf("description = %q, want the spec text %q", attr.Description, specText)
		}
	})

	t.Run("list config schema honors an explicit description", func(t *testing.T) {
		lr := &ir.ListResourceIR{ConfigSchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "limit", Description: specText, Schema: ir.SchemaIR{Type: ir.TypeInt}},
		}}}
		applyListResourceConfigSchema(lr, []config.ListConfigSchema{{Name: "limit", Type: "int64", Optional: boolPtr(true), Description: "Override wins."}})
		attr, _ := findAttr(lr.ConfigSchema.Attributes, "limit")
		if attr.Description != "Override wins." {
			t.Errorf("description = %q, want the override text", attr.Description)
		}
	})
}

// TestApplyOverrides_ComputedRefusedForRequiredRequestInput locks in the G39
// guard: a computed_attributes entry naming an attribute the generated CRUD
// body sends with a required value (e.g. a required query parameter like
// clusterId, or a required create-body field) is refused for that attribute —
// applying it would leave the request sending a value the practitioner can
// never supply, breaking create and import. The attribute keeps its Required
// semantics and a Warning is surfaced instead of silently breaking the request.
func TestApplyOverrides_ComputedRefusedForRequiredRequestInput(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{{
			Name:     "adv_hash",
			TypeName: "adv_hash",
			Schema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					// cluster_id feeds a required query parameter the wired
					// read/create body sends from state.
					{Name: "cluster_id", Required: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					// slot_id is the path identifier, also a request input.
					{Name: "slot_id", Required: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					// fields is an optional request-body input: computed is
					// still allowed (Optional+Computed keeps it settable).
					{Name: "fields", Optional: true, RequestInput: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
					// cluster_name is response-only: computed applies freely.
					{Name: "cluster_name", Required: false, Optional: false, Schema: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		}},
	}
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "test", Version: "0.0.1"},
		ResourceOverrides: []config.ResourceOverride{{
			Schema:             "adv_hash",
			ComputedAttributes: []string{"cluster_id", "slot_id", "fields", "cluster_name"},
		}},
	}

	var diags diagnostics.Diagnostics
	if err := ApplyOverridesWithDiagnostics(provider, cfg, &diags); err != nil {
		t.Fatalf("ApplyOverridesWithDiagnostics() = %v, want nil", err)
	}

	attrs := provider.Resources[0].Schema.Attributes
	for _, a := range attrs {
		switch a.Name {
		case "cluster_id", "slot_id":
			if a.Computed {
				t.Errorf("%s Computed = true, want false (required request input must keep Required semantics)", a.Name)
			}
			if !a.Required {
				t.Errorf("%s Required = false, want true", a.Name)
			}
		case "fields":
			if !a.Computed || !a.Optional {
				t.Errorf("fields flags = (Computed=%v Optional=%v), want Optional+Computed", a.Computed, a.Optional)
			}
		case "cluster_name":
			if !a.Computed {
				t.Errorf("cluster_name Computed = false, want true (response-only attribute is freely computable)")
			}
		}
	}

	// Both refused attributes surface as fail-loud warnings.
	warnings := 0
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "computed_attributes override refused") {
			warnings++
		}
	}
	if warnings != 2 {
		t.Errorf("refusal warnings = %d, want 2 (cluster_id and slot_id); diags: %+v", warnings, diags)
	}
}
