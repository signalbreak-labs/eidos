package transformer

import (
	"reflect"
	"testing"
)

func op(m HTTPMethod, path string) Operation {
	return Operation{Method: m, Path: path}
}

func opPtr(m HTTPMethod, path string) *Operation {
	o := Operation{Method: m, Path: path}
	return &o
}

func TestInferResourceCRUD(t *testing.T) {
	tests := []struct {
		name  string
		paths map[string]map[HTTPMethod]Operation
		want  []ResourceCRUD
	}{
		{
			name: "simple full CRUD",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets": {
					MethodPost: op(MethodPost, "/pets"),
					MethodGet:  op(MethodGet, "/pets"),
				},
				"/pets/{petId}": {
					MethodGet:    op(MethodGet, "/pets/{petId}"),
					MethodPut:    op(MethodPut, "/pets/{petId}"),
					MethodDelete: op(MethodDelete, "/pets/{petId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Create:         opPtr(MethodPost, "/pets"),
					Read:           opPtr(MethodGet, "/pets/{petId}"),
					Update:         opPtr(MethodPut, "/pets/{petId}"),
					FullUpdate:     opPtr(MethodPut, "/pets/{petId}"),
					Delete:         opPtr(MethodDelete, "/pets/{petId}"),
					List:           opPtr(MethodGet, "/pets"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
		{
			name: "partial update via PATCH",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets": {
					MethodPost: op(MethodPost, "/pets"),
				},
				"/pets/{petId}": {
					MethodGet:    op(MethodGet, "/pets/{petId}"),
					MethodPatch:  op(MethodPatch, "/pets/{petId}"),
					MethodDelete: op(MethodDelete, "/pets/{petId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Create:         opPtr(MethodPost, "/pets"),
					Read:           opPtr(MethodGet, "/pets/{petId}"),
					Update:         opPtr(MethodPatch, "/pets/{petId}"),
					PartialUpdate:  opPtr(MethodPatch, "/pets/{petId}"),
					Delete:         opPtr(MethodDelete, "/pets/{petId}"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
		{
			name: "both PUT and PATCH prefer PUT",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets": {
					MethodPost: op(MethodPost, "/pets"),
				},
				"/pets/{petId}": {
					MethodGet:    op(MethodGet, "/pets/{petId}"),
					MethodPut:    op(MethodPut, "/pets/{petId}"),
					MethodPatch:  op(MethodPatch, "/pets/{petId}"),
					MethodDelete: op(MethodDelete, "/pets/{petId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Create:         opPtr(MethodPost, "/pets"),
					Read:           opPtr(MethodGet, "/pets/{petId}"),
					Update:         opPtr(MethodPut, "/pets/{petId}"),
					FullUpdate:     opPtr(MethodPut, "/pets/{petId}"),
					PartialUpdate:  opPtr(MethodPatch, "/pets/{petId}"),
					Delete:         opPtr(MethodDelete, "/pets/{petId}"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
		{
			name: "composite ID",
			paths: map[string]map[HTTPMethod]Operation{
				"/projects/{projectId}/tasks": {
					MethodPost: op(MethodPost, "/projects/{projectId}/tasks"),
					MethodGet:  op(MethodGet, "/projects/{projectId}/tasks"),
				},
				"/projects/{projectId}/tasks/{taskId}": {
					MethodGet:    op(MethodGet, "/projects/{projectId}/tasks/{taskId}"),
					MethodPut:    op(MethodPut, "/projects/{projectId}/tasks/{taskId}"),
					MethodDelete: op(MethodDelete, "/projects/{projectId}/tasks/{taskId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "task",
					CollectionPath: "/projects/{projectId}/tasks",
					InstancePath:   "/projects/{projectId}/tasks/{taskId}",
					Create:         opPtr(MethodPost, "/projects/{projectId}/tasks"),
					Read:           opPtr(MethodGet, "/projects/{projectId}/tasks/{taskId}"),
					Update:         opPtr(MethodPut, "/projects/{projectId}/tasks/{taskId}"),
					FullUpdate:     opPtr(MethodPut, "/projects/{projectId}/tasks/{taskId}"),
					Delete:         opPtr(MethodDelete, "/projects/{projectId}/tasks/{taskId}"),
					List:           opPtr(MethodGet, "/projects/{projectId}/tasks"),
					ID:             IDInfo{Kind: IDComposite, ParameterNames: []string{"projectId", "taskId"}, ImportFormat: "%s:%s"},
				},
			},
		},
		{
			name: "no collection path infers read update delete only",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets/{petId}": {
					MethodGet:    op(MethodGet, "/pets/{petId}"),
					MethodPut:    op(MethodPut, "/pets/{petId}"),
					MethodDelete: op(MethodDelete, "/pets/{petId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Read:           opPtr(MethodGet, "/pets/{petId}"),
					Update:         opPtr(MethodPut, "/pets/{petId}"),
					FullUpdate:     opPtr(MethodPut, "/pets/{petId}"),
					Delete:         opPtr(MethodDelete, "/pets/{petId}"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
		{
			name: "collection only infers create and list",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets": {
					MethodPost: op(MethodPost, "/pets"),
					MethodGet:  op(MethodGet, "/pets"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					Create:         opPtr(MethodPost, "/pets"),
					List:           opPtr(MethodGet, "/pets"),
					ID:             IDInfo{},
				},
			},
		},
		{
			name: "multiple resources sorted by name",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets": {
					MethodPost: op(MethodPost, "/pets"),
					MethodGet:  op(MethodGet, "/pets"),
				},
				"/pets/{petId}": {
					MethodGet: op(MethodGet, "/pets/{petId}"),
				},
				"/owners": {
					MethodPost: op(MethodPost, "/owners"),
					MethodGet:  op(MethodGet, "/owners"),
				},
				"/owners/{ownerId}": {
					MethodGet: op(MethodGet, "/owners/{ownerId}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "owner",
					CollectionPath: "/owners",
					InstancePath:   "/owners/{ownerId}",
					Create:         opPtr(MethodPost, "/owners"),
					Read:           opPtr(MethodGet, "/owners/{ownerId}"),
					List:           opPtr(MethodGet, "/owners"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"ownerId"}, AttributeName: "owner_id", ImportFormat: "%s"},
				},
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Create:         opPtr(MethodPost, "/pets"),
					Read:           opPtr(MethodGet, "/pets/{petId}"),
					List:           opPtr(MethodGet, "/pets"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
		{
			name: "path parameter regex constraint parsed",
			paths: map[string]map[HTTPMethod]Operation{
				"/pets/{petId:uuid}": {
					MethodGet: op(MethodGet, "/pets/{petId:uuid}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId:uuid}",
					Read:           opPtr(MethodGet, "/pets/{petId:uuid}"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferResourceCRUD(tt.paths)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InferResourceCRUD() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestToSnakeCaseCanonical(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"petId", "pet_id"},
		{"projectID", "project_id"},
		{"page-size", "page_size"},
		{"pet.name", "pet_name"},
		{"pet name", "pet_name"},
		{"id", "id"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ToSnakeCase(tt.in); got != tt.want {
				t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSingularize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"pets", "pet"},
		{"categories", "category"},
		{"geese", "geese"},
		{"species", "species"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := singularize(tt.in); got != tt.want {
				t.Errorf("singularize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestInferResourceCRUDDedupsNameCollisions locks in the M-39 fix: two collection
// paths that map to the same resource name (here /v1/pets and /v2/pets both ->
// "pet") produce a single resource, not two same-named entries that would
// collide downstream as duplicate Terraform type names. The surviving entry is
// the lexicographically-first collection (/v1/pets), deterministically.
func TestInferResourceCRUDDedupsNameCollisions(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/v1/pets": {
			MethodPost: {Method: MethodPost, Path: "/v1/pets", OperationID: "createV1Pet"},
			MethodGet:  {Method: MethodGet, Path: "/v1/pets", OperationID: "listV1Pets"},
		},
		"/v1/pets/{petId}": {
			MethodGet:    {Method: MethodGet, Path: "/v1/pets/{petId}", OperationID: "showV1Pet"},
			MethodDelete: {Method: MethodDelete, Path: "/v1/pets/{petId}", OperationID: "deleteV1Pet"},
		},
		"/v2/pets": {
			MethodPost: {Method: MethodPost, Path: "/v2/pets", OperationID: "createV2Pet"},
			MethodGet:  {Method: MethodGet, Path: "/v2/pets", OperationID: "listV2Pets"},
		},
		"/v2/pets/{petId}": {
			MethodGet:    {Method: MethodGet, Path: "/v2/pets/{petId}", OperationID: "showV2Pet"},
			MethodDelete: {Method: MethodDelete, Path: "/v2/pets/{petId}", OperationID: "deleteV2Pet"},
		},
	}

	// Run several times; output must be stable and contain exactly one "pet".
	for i := 0; i < 5; i++ {
		resources := InferResourceCRUD(pathOps)
		if len(resources) != 1 {
			t.Fatalf("iteration %d: expected 1 deduped resource, got %d: %+v", i, len(resources), resources)
		}
		if resources[0].Name != "pet" {
			t.Fatalf("expected name %q, got %q", "pet", resources[0].Name)
		}
		if resources[0].CollectionPath != "/v1/pets" {
			t.Fatalf("expected surviving collection /v1/pets, got %q", resources[0].CollectionPath)
		}
	}
}

func TestInferResourceCRUDPrefersCompleteCRUDOnNameCollision(t *testing.T) {
	// A degenerate sub-path group (/access-control/.../teams, no create) shares
	// the name "team" with a full-CRUD group (/teams). The full-CRUD group must
	// survive dedup (G7).
	pathOps := map[string]map[HTTPMethod]Operation{
		"/teams": {
			MethodPost: {Method: MethodPost, Path: "/teams", OperationID: "createTeam"},
			MethodGet:  {Method: MethodGet, Path: "/teams", OperationID: "searchTeams"},
		},
		"/teams/{team_id}": {
			MethodGet:    {Method: MethodGet, Path: "/teams/{team_id}", OperationID: "getTeamByID"},
			MethodPut:    {Method: MethodPut, Path: "/teams/{team_id}", OperationID: "updateTeam"},
			MethodDelete: {Method: MethodDelete, Path: "/teams/{team_id}", OperationID: "deleteTeamByID"},
		},
		// Same plural noun but only a GET on a deeper sub-path -> degenerate.
		"/access-control/{resource}/{resourceID}/teams": {
			MethodGet: {Method: MethodGet, Path: "/access-control/{resource}/{resourceID}/teams", OperationID: "listResourceTeams"},
		},
	}

	resources := InferResourceCRUD(pathOps)
	var team *ResourceCRUD
	for i := range resources {
		if resources[i].Name == "team" {
			team = &resources[i]
			break
		}
	}
	if team == nil {
		t.Fatalf("expected a \"team\" resource, got %+v", resources)
	}
	if team.CollectionPath != "/teams" {
		t.Fatalf("expected surviving collection /teams, got %q", team.CollectionPath)
	}
	if team.Create == nil || team.Read == nil || team.Delete == nil || team.Update == nil {
		t.Fatalf("expected full CRUD on the surviving team group, got create=%v read=%v update=%v delete=%v",
			team.Create != nil, team.Read != nil, team.Update != nil, team.Delete != nil)
	}
}
