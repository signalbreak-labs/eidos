package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestHasFullCRUD verifies HasFullCRUD reports true only for operations that
// belong to a complete create/read/delete CRUD group.
func TestHasFullCRUD(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets": {
			MethodPost: op(MethodPost, "/pets"),
			MethodGet:  op(MethodGet, "/pets"),
		},
		"/pets/{petId}": {
			MethodGet:    op(MethodGet, "/pets/{petId}"),
			MethodDelete: op(MethodDelete, "/pets/{petId}"),
		},
	}

	if !HasFullCRUD("/pets", MethodPost, pathOps) {
		t.Error("expected POST /pets to be part of a full CRUD group")
	}
	if !HasFullCRUD("/pets/{petId}", MethodGet, pathOps) {
		t.Error("expected GET /pets/{petId} to be part of a full CRUD group")
	}
	if HasFullCRUD("/pets", MethodGet, pathOps) {
		t.Error("collection GET is a list, not part of the managed-resource CRUD group")
	}
}

// TestHasFullCRUD_IncompleteGroup verifies a group missing Delete is not
// reported as full CRUD.
func TestHasFullCRUD_IncompleteGroup(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets": {
			MethodPost: op(MethodPost, "/pets"),
		},
		"/pets/{petId}": {
			MethodGet: op(MethodGet, "/pets/{petId}"),
		},
	}
	if HasFullCRUD("/pets", MethodPost, pathOps) {
		t.Error("expected no full CRUD without a delete operation")
	}
}

// TestResourceOverrideMatches verifies ResourceOverrideMatches matches a
// resource by name or operation identity.
func TestResourceOverrideMatches(t *testing.T) {
	r := ir.ResourceIR{
		Name:            "pet",
		FullName:        "mycloud_pet",
		SourceOperation: "createPet",
	}

	if !ResourceOverrideMatches(r, config.ResourceOverride{Schema: "pet"}) {
		t.Error("expected override schema pet to match resource pet")
	}
	if !ResourceOverrideMatches(r, config.ResourceOverride{Schema: "mycloud_pet"}) {
		t.Error("expected override schema mycloud_pet to match")
	}
	if !ResourceOverrideMatches(r, config.ResourceOverride{Operation: "createPet"}) {
		t.Error("expected override operation createPet to match SourceOperation")
	}
	if ResourceOverrideMatches(r, config.ResourceOverride{Schema: "dog"}) {
		t.Error("expected override schema dog not to match resource pet")
	}
}

// TestOverrideMatchesEntity verifies OverrideMatchesEntity matches an entity
// by its source operation or name.
func TestOverrideMatchesEntity(t *testing.T) {
	m := ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"}

	if !OverrideMatchesEntity("createPet", m, "createPet", "pet") {
		t.Error("expected override operation createPet to match source operation")
	}
	if !OverrideMatchesEntity("createPet", m, "pet", "pet") {
		t.Error("expected override name pet to match entity name")
	}
	if OverrideMatchesEntity("createPet", m, "deletePet", "dog") {
		t.Error("expected unrelated override not to match")
	}
}
