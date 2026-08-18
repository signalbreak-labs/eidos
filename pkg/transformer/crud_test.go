package transformer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
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
			// N-18: a path parameter whose name is a reserved Terraform root
			// ("provider") must sanitize to "provider_" so the inferred ID
			// AttributeName matches the schema attribute the state shape produces.
			// A bare ToSnakeCase "provider" would leave the synthetic-id check and
			// forcePutAsCreateIdentifiers looking up the wrong name.
			name: "reserved root path parameter sanitized",
			paths: map[string]map[HTTPMethod]Operation{
				"/providers/{provider}": {
					MethodGet: op(MethodGet, "/providers/{provider}"),
				},
			},
			want: []ResourceCRUD{
				{
					Name:           "provider",
					CollectionPath: "/providers",
					InstancePath:   "/providers/{provider}",
					Read:           opPtr(MethodGet, "/providers/{provider}"),
					ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"provider"}, AttributeName: "provider_", ImportFormat: "%s"},
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
			// usePutAsCreate=false preserves the legacy inference shape these
			// assertions lock in (a PUT-only group stays Create-less). The
			// PUT-as-create behavior is exercised by TestInferResourceCRUDPutAsCreate.
			got := InferResourceCRUD(tt.paths, false)
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
		// N-12: plurals of sibilant-final words add "es"; stripping only the
		// trailing "s" would mangle them.
		{"statuses", "status"},
		{"classes", "class"},
		{"addresses", "address"},
		{"processes", "process"},
		{"boxes", "box"},
		{"churches", "church"},
		{"dishes", "dish"},
		{"aliases", "alias"},
		{"buses", "bus"},
		// The "e"-final class keeps the generic strip.
		{"cases", "case"},
		{"houses", "house"},
		{"phases", "phase"},
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
		resources := InferResourceCRUD(pathOps, false)
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

// TestInferResourceCRUDWithDiagnosticsWarnsOnNameCollision locks in the N-19
// fix: when dedupCRUDByName drops a same-named group, the diagnostics channel
// carries a Warning naming both the surviving and the dropped collection paths,
// so the loss is never silent (AGENTS.md "fail loud, never silently").
func TestInferResourceCRUDWithDiagnosticsWarnsOnNameCollision(t *testing.T) {
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

	var diags diagnostics.Diagnostics
	resources := InferResourceCRUDWithDiagnostics(pathOps, false, &diags)
	if len(resources) != 1 {
		t.Fatalf("expected 1 deduped resource, got %d: %+v", len(resources), resources)
	}
	if !hasWarning(diags, "resource name collision \"pet\" dropped a CRUD group") {
		t.Fatalf("expected a name-collision Warning naming the surviving and dropped paths, got diags: %+v", diags)
	}
	if !hasWarning(diags, "/v2/pets") {
		t.Fatalf("expected the Warning to name the dropped path /v2/pets, got diags: %+v", diags)
	}
	if !hasWarning(diags, "/v1/pets") {
		t.Fatalf("expected the Warning to name the surviving path /v1/pets, got diags: %+v", diags)
	}

	// The no-diagnostics entry point must stay silent-capable: passing a nil
	// channel never panics and emits nothing.
	resources2 := InferResourceCRUD(pathOps, false)
	if len(resources2) != 1 {
		t.Fatalf("expected 1 deduped resource via InferResourceCRUD, got %d", len(resources2))
	}
}

// TestDedupByNameWarnsOnDuplicate locks in the generic dedupByName fail-loud
// behavior used by list resources (N-19): a later same-named entry is dropped
// with a Warning rather than silently.
func TestDedupByNameWarnsOnDuplicate(t *testing.T) {
	items := []struct {
		Name string
	}{
		{Name: "pets"},
		{Name: "pets"},
	}
	var diags diagnostics.Diagnostics
	out := dedupByName(items, func(it struct{ Name string }) string { return it.Name }, &diags)
	if len(out) != 1 {
		t.Fatalf("expected 1 deduped item, got %d: %+v", len(out), out)
	}
	if !hasWarning(diags, `duplicate`) || !hasWarning(diags, `"pets"`) {
		t.Fatalf("expected a duplicate-name Warning for %q, got diags: %+v", "pets", diags)
	}
}

// TestInferListResourcesWithDiagnosticsWarnsOnDuplicate locks in the live
// list-resource path surfacing a same-named dedup as a Warning (N-19).
func TestInferListResourcesWithDiagnosticsWarnsOnDuplicate(t *testing.T) {
	resources := []ResourceCRUD{
		{
			Name:           "pet",
			CollectionPath: "/v1/pets",
			InstancePath:   "/v1/pets/{petId}",
			Read:           &Operation{Method: MethodGet, Path: "/v1/pets/{petId}"},
			List:           &Operation{Method: MethodGet, Path: "/v1/pets"},
		},
		{
			Name:           "pet",
			CollectionPath: "/v2/pets",
			InstancePath:   "/v2/pets/{petId}",
			Read:           &Operation{Method: MethodGet, Path: "/v2/pets/{petId}"},
			List:           &Operation{Method: MethodGet, Path: "/v2/pets"},
		},
	}

	var diags diagnostics.Diagnostics
	lists := InferListResourcesWithDiagnostics(resources, &diags)
	if len(lists) != 1 {
		t.Fatalf("expected 1 deduped list resource, got %d: %+v", len(lists), lists)
	}
	var found bool
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.String(), "pets") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a duplicate-name Warning for %q, got diags: %+v", "pets", diags)
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

	resources := InferResourceCRUD(pathOps, false)
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

// TestInferResourceCRUDPutAsCreate locks in the default-on PUT-as-create
// inference: a CRUD group whose collection path has no POST but whose instance
// path has PUT+GET+DELETE uses the PUT as the Create (upsert). With the
// kill-switch (usePutAsCreate=false) the group stays Create-less (legacy
// scaffold behavior). Collection POST still wins when present.
func TestInferResourceCRUDPutAsCreate(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/alarms/{alarmId}": {
			MethodGet:    op(MethodGet, "/alarms/{alarmId}"),
			MethodPut:    op(MethodPut, "/alarms/{alarmId}"),
			MethodDelete: op(MethodDelete, "/alarms/{alarmId}"),
		},
	}

	t.Run("default-on uses PUT as create", func(t *testing.T) {
		resources := InferResourceCRUD(pathOps, true)
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d: %+v", len(resources), resources)
		}
		g := resources[0]
		if g.Create == nil || g.Create.Method != MethodPut || g.Create.Path != "/alarms/{alarmId}" {
			t.Fatalf("expected Create = PUT /alarms/{alarmId}, got %+v", g.Create)
		}
		// The same PUT remains the Update mapping (upsert for both Create and Update).
		if g.Update == nil || g.Update.Method != MethodPut {
			t.Fatalf("expected Update = PUT, got %+v", g.Update)
		}
		if g.Read == nil || g.Delete == nil {
			t.Fatalf("expected Read and Delete present, got read=%+v delete=%+v", g.Read, g.Delete)
		}
	})

	t.Run("kill-switch leaves Create nil", func(t *testing.T) {
		resources := InferResourceCRUD(pathOps, false)
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d: %+v", len(resources), resources)
		}
		g := resources[0]
		if g.Create != nil {
			t.Fatalf("expected Create == nil with usePutAsCreate=false, got %+v", g.Create)
		}
		// Update still resolves from the PUT (legacy behavior).
		if g.Update == nil || g.Update.Method != MethodPut {
			t.Fatalf("expected Update = PUT, got %+v", g.Update)
		}
	})
}

// TestInferResourceCRUDPutAsCreatePostWins confirms a collection POST still
// wins as Create when present, even with usePutAsCreate=true: the PUT stays the
// Update mapping only.
func TestInferResourceCRUDPutAsCreatePostWins(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/alarms": {
			MethodPost: op(MethodPost, "/alarms"),
			MethodGet:  op(MethodGet, "/alarms"),
		},
		"/alarms/{alarmId}": {
			MethodGet:    op(MethodGet, "/alarms/{alarmId}"),
			MethodPut:    op(MethodPut, "/alarms/{alarmId}"),
			MethodDelete: op(MethodDelete, "/alarms/{alarmId}"),
		},
	}
	resources := InferResourceCRUD(pathOps, true)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d: %+v", len(resources), resources)
	}
	g := resources[0]
	if g.Create == nil || g.Create.Method != MethodPost || g.Create.Path != "/alarms" {
		t.Fatalf("expected Create = POST /alarms, got %+v", g.Create)
	}
	if g.Update == nil || g.Update.Method != MethodPut {
		t.Fatalf("expected Update = PUT, got %+v", g.Update)
	}
}
