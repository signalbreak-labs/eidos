package generator_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/api"
	"github.com/signalbreak-labs/eidos/pkg/generator"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestMyCloudPetsGroupedResourceWired is the §3 regression guard: the mycloud-pets spec
// (POST /pets, GET /pets/{petId}, DELETE /pets/{petId}, GET /pets) must group into
// a single managed `pet` resource whose Create/Read/Delete are wired to the
// generated API client, with a schema reconciled against the NewPet request body
// (id Computed, name Required, tag Optional). Before §3, mycloud-pets yielded two
// separate partial resources (create_pets, delete_pet) and no wiring fired.
func TestMyCloudPetsGroupedResourceWired(t *testing.T) {
	data, err := os.ReadFile("../../test/specs/mycloud-pets.yaml")
	if err != nil {
		t.Fatalf("read mycloud-pets spec: %v", err)
	}
	resp := api.Validate(data)
	if !resp.Valid || resp.IRPreview == nil {
		t.Fatalf("mycloud-pets spec produced invalid diagnostics or no IR preview")
	}

	// The grouped `pet` resource exists with complete CRUD.
	var pet *ir.ResourceIR
	for i := range resp.IRPreview.Resources {
		if resp.IRPreview.Resources[i].Name == "pet" {
			pet = &resp.IRPreview.Resources[i]
			break
		}
	}
	if pet == nil {
		t.Fatalf("no grouped `pet` resource in IR; resources: %+v", resp.IRPreview.Resources)
	}
	if pet.CRUDMapping.Create.Method != http.MethodPost || pet.CRUDMapping.Read.Method != http.MethodGet || pet.CRUDMapping.Delete.Method != http.MethodDelete {
		t.Errorf("pet CRUD mapping incomplete: create=%+v read=%+v delete=%+v",
			pet.CRUDMapping.Create, pet.CRUDMapping.Read, pet.CRUDMapping.Delete)
	}
	if pet.CRUDMapping.Update != nil {
		t.Errorf("mycloud-pets has no update operation; got Update=%+v", pet.CRUDMapping.Update)
	}

	// Schema reconciliation: id Computed (server-assigned int), name Required,
	// tag Optional.
	find := func(name string) *ir.AttributeIR {
		for i := range pet.Schema.Attributes {
			if pet.Schema.Attributes[i].Name == name {
				return &pet.Schema.Attributes[i]
			}
		}
		return nil
	}
	id := find("id")
	if id == nil {
		t.Fatalf("no id attribute in pet schema: %+v", pet.Schema.Attributes)
	}
	if id.Schema.Type != ir.TypeInt || !id.Computed || id.Required || id.Optional {
		t.Errorf("id attr = type %q Required=%v Optional=%v Computed=%v; want int/Computed",
			id.Schema.Type, id.Required, id.Optional, id.Computed)
	}
	name := find("name")
	if name == nil || !name.Required || name.Computed {
		t.Errorf("name attr = %+v; want Required (in NewPet required)", name)
	}
	tag := find("tag")
	if tag == nil || !tag.Optional || tag.Required || !tag.Computed {
		t.Errorf("tag attr = %+v; want Optional+Computed (optional request input the response also returns)", tag)
	}

	// The partial resources (create_pets, delete_pet) must not survive grouping.
	for _, r := range resp.IRPreview.Resources {
		if r.Name == "create_pets" || r.Name == "delete_pet" {
			t.Errorf("partial resource %q survived grouping; should be merged into pet", r.Name)
		}
	}
	// The instance GET (showPetById) is consumed into the resource Read; the
	// collection GET (listPets) remains a data source.
	if !hasDataSourceNamed(resp.IRPreview.DataSources, "list_pets") {
		t.Errorf("list_pets data source should remain for the collection GET")
	}
	if hasDataSourceNamed(resp.IRPreview.DataSources, "show_pet_by_id") {
		t.Errorf("show_pet_by_id data source should be consumed into pet.Read")
	}

	// Generate and confirm the wired bodies make real HTTP calls.
	tmp := t.TempDir()
	if _, err := generator.Run(resp.IRPreview, generator.Options{
		Mode:           generator.ModeWrite,
		OutputDir:      tmp,
		CollectOptions: generator.DefaultCollectOptions(),
	}); err != nil {
		t.Fatalf("generator.Run: %v", err)
	}
	body, err := os.ReadFile(tmp + "/internal/provider/resource_pet.go")
	if err != nil {
		t.Fatalf("read resource_pet.go: %v", err)
	}
	// Three wired operations (Create/Read/Delete) each issue a request.
	if got := strings.Count(string(body), "r.client.NewRequest"); got != 3 {
		t.Errorf("resource_pet.go has %d r.client.NewRequest calls; want 3 (Create/Read/Delete)", got)
	}
	// Update is honestly scaffolded (mycloud-pets has no PUT/PATCH).
	if !strings.Contains(string(body), "Update is not wired to a remote API endpoint.") {
		t.Errorf("resource_pet.go Update should carry the honest not-wired scaffold marker")
	}
}

func hasDataSourceNamed(sources []ir.DataSourceIR, name string) bool {
	for _, ds := range sources {
		if ds.Name == name {
			return true
		}
	}
	return false
}
