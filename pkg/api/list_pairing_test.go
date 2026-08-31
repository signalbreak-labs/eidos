package api

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// identityList returns a list resource named name with a single id identity
// attribute, as a promoted collection GET with a paired instance path would
// produce.
func identityList(name, collectionPath string) ir.ListResourceIR {
	return ir.ListResourceIR{
		Name:           name,
		FullName:       "test_" + name,
		TypeName:       "test_" + name,
		CollectionPath: collectionPath,
		ListMapping:    ir.OperationMappingIR{Method: "GET", PathTemplate: collectionPath},
		IdentitySchema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
			{Name: "id", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		}},
	}
}

// crudResource returns a managed resource named name whose CRUD group was
// inferred from the given collection path.
func crudResource(name, collectionPath string) ir.ResourceIR {
	return ir.ResourceIR{
		Name:           name,
		FullName:       "test_" + name,
		TypeName:       "test_" + name,
		CollectionPath: collectionPath,
	}
}

func hasDiagnostic(diags diagnostics.Diagnostics, severity diagnostics.Severity, substr string) bool {
	for _, d := range diags {
		if d.Severity == severity && strings.Contains(d.Summary, substr) {
			return true
		}
	}
	return false
}

// TestPairListResourceRegistrations_RenamesListToManagedResource locks in that
// a list resource whose collection path belongs to a surviving managed-resource
// CRUD group is renamed to that resource so the provider can register it, and
// that the rename surfaces an Info diagnostic rather than happening silently.
func TestPairListResourceRegistrations_RenamesListToManagedResource(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources:     []ir.ResourceIR{crudResource("widget", "/widgets")},
		ListResources: []ir.ListResourceIR{identityList("get_all_widgets", "/widgets")},
	}
	var diags diagnostics.Diagnostics
	pairListResourceRegistrations(provider, &diags)

	lr := provider.ListResources[0]
	if lr.Name != "widget" || lr.TypeName != "test_widget" || lr.FullName != "test_widget" {
		t.Errorf("list not renamed to the managed resource: got Name=%q TypeName=%q", lr.Name, lr.TypeName)
	}
	if !lr.Registerable {
		t.Error("renamed list must be Registerable")
	}
	if !hasDiagnostic(diags, diagnostics.Info, "paired with managed resource") {
		t.Errorf("expected an Info diagnostic for the rename, got %+v", diags)
	}
}

// TestPairListResourceRegistrations_UnpairableListWarns locks in that a list
// resource with no paired managed resource is never registered silently: it
// keeps its inferred name, stays unregisterable, and gets a fail-loud Warning.
func TestPairListResourceRegistrations_UnpairableListWarns(t *testing.T) {
	provider := &ir.ProviderIR{
		ListResources: []ir.ListResourceIR{identityList("get_all_things", "/things")},
	}
	var diags diagnostics.Diagnostics
	pairListResourceRegistrations(provider, &diags)

	lr := provider.ListResources[0]
	if lr.Name != "get_all_things" || lr.Registerable {
		t.Errorf("unpairable list must keep its name and stay unregistered: %+v", lr)
	}
	if !hasDiagnostic(diags, diagnostics.Warning, "cannot be registered") {
		t.Errorf("expected a Warning for the unregistered list, got %+v", diags)
	}
}

// TestPairListResourceRegistrations_ExistingNameMatchKept locks in that a list
// whose type name already matches a managed resource (e.g. a generator.yaml
// list_resource_override with resource: user) is marked registerable without a
// rename, and that a second list for the same collection path cannot claim the
// already-paired resource.
func TestPairListResourceRegistrations_ExistingNameMatchKept(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{crudResource("user", "/users")},
		ListResources: []ir.ListResourceIR{
			identityList("user", "/users"),
			identityList("get_all_users", "/users"),
		},
	}
	var diags diagnostics.Diagnostics
	pairListResourceRegistrations(provider, &diags)

	if !provider.ListResources[0].Registerable {
		t.Error("name-matched list must be Registerable")
	}
	if provider.ListResources[1].Registerable {
		t.Error("a second list must not claim a resource already paired with another list")
	}
	if provider.ListResources[1].Name != "get_all_users" {
		t.Errorf("unclaimed list must keep its name, got %q", provider.ListResources[1].Name)
	}
	if !hasDiagnostic(diags, diagnostics.Warning, "cannot be registered") {
		t.Errorf("expected a Warning for the second, unclaimable list, got %+v", diags)
	}
}

// TestPairListResourceRegistrations_EmptyIdentityNotRenamed locks in that a
// list with no identity attributes is never renamed onto a managed resource:
// registering it would give terraform query an identity-less list typed against
// a resource with no IdentitySchema method.
func TestPairListResourceRegistrations_EmptyIdentityNotRenamed(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources: []ir.ResourceIR{crudResource("user", "/users")},
		ListResources: []ir.ListResourceIR{{
			Name:           "get_all_users",
			FullName:       "test_get_all_users",
			TypeName:       "test_get_all_users",
			CollectionPath: "/users",
		}},
	}
	var diags diagnostics.Diagnostics
	pairListResourceRegistrations(provider, &diags)

	lr := provider.ListResources[0]
	if lr.Registerable || lr.Name != "get_all_users" {
		t.Errorf("identity-less list must stay unregistered under its inferred name: %+v", lr)
	}
	if !hasDiagnostic(diags, diagnostics.Warning, "cannot be registered") {
		t.Errorf("expected a Warning for the identity-less list, got %+v", diags)
	}
}

// TestPairListResourceRegistrations_SharesIdentityWithResource locks in that a
// renamed list's identity schema is copied onto the managed resource so the
// generator emits the ResourceWithIdentity method terraform query requires.
func TestPairListResourceRegistrations_SharesIdentityWithResource(t *testing.T) {
	provider := &ir.ProviderIR{
		Resources:     []ir.ResourceIR{crudResource("user", "/users")},
		ListResources: []ir.ListResourceIR{identityList("get_all_users", "/users")},
	}
	var diags diagnostics.Diagnostics
	pairListResourceRegistrations(provider, &diags)
	pairListResourceIdentities(provider)

	if provider.Resources[0].IdentitySchema == nil {
		t.Fatal("managed resource has no identity schema after pairing")
	}
	if len(provider.Resources[0].IdentitySchema.Attributes) != 1 ||
		provider.Resources[0].IdentitySchema.Attributes[0].Name != "id" {
		t.Errorf("identity schema not shared from the renamed list: %+v", provider.Resources[0].IdentitySchema)
	}
}
