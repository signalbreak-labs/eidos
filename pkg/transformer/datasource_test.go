package transformer

import (
	"reflect"
	"testing"
)

func TestInferDataSources(t *testing.T) {
	tests := []struct {
		name      string
		resources []ResourceCRUD
		want      []DataSource
	}{
		{
			name: "collection only becomes data source",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: []DataSource{
				{
					Name:            "pets",
					CollectionPath:  "/pets",
					List:            &Operation{Method: MethodGet, Path: "/pets"},
					PaginationStyle: PaginationNone,
				},
			},
		},
		{
			name: "paired managed resource becomes list resource not data source",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}"},
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: nil,
		},
		{
			name: "no list operation infers nothing",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}"},
				},
			},
			want: nil,
		},
		{
			name: "instance path without read is not a data source",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: nil,
		},
		{
			name: "multiple data sources are sorted by name",
			resources: []ResourceCRUD{
				{
					Name:           "owner",
					CollectionPath: "/owners",
					List:           &Operation{Method: MethodGet, Path: "/owners"},
				},
				{
					Name:           "pet",
					CollectionPath: "/pets",
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: []DataSource{
				{
					Name:            "owners",
					CollectionPath:  "/owners",
					List:            &Operation{Method: MethodGet, Path: "/owners"},
					PaginationStyle: PaginationNone,
				},
				{
					Name:            "pets",
					CollectionPath:  "/pets",
					List:            &Operation{Method: MethodGet, Path: "/pets"},
					PaginationStyle: PaginationNone,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferDataSources(tt.resources)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InferDataSources() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestInferListResources(t *testing.T) {
	tests := []struct {
		name      string
		resources []ResourceCRUD
		want      []ListResource
	}{
		{
			name: "collection paired with managed resource becomes list resource",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}"},
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: []ListResource{
				{
					Name:            "pets",
					ResourceName:    "pet",
					CollectionPath:  "/pets",
					InstancePath:    "/pets/{petId}",
					List:            &Operation{Method: MethodGet, Path: "/pets"},
					PaginationStyle: PaginationNone,
				},
			},
		},
		{
			name: "collection without instance path is not a list resource",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: nil,
		},
		{
			name: "instance path without read is not a list resource",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
			},
			want: nil,
		},
		{
			name: "multiple list resources are sorted by name",
			resources: []ResourceCRUD{
				{
					Name:           "pet",
					CollectionPath: "/pets",
					InstancePath:   "/pets/{petId}",
					Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}"},
					List:           &Operation{Method: MethodGet, Path: "/pets"},
				},
				{
					Name:           "owner",
					CollectionPath: "/owners",
					InstancePath:   "/owners/{ownerId}",
					Read:           &Operation{Method: MethodGet, Path: "/owners/{ownerId}"},
					List:           &Operation{Method: MethodGet, Path: "/owners"},
				},
			},
			want: []ListResource{
				{
					Name:            "owners",
					ResourceName:    "owner",
					CollectionPath:  "/owners",
					InstancePath:    "/owners/{ownerId}",
					List:            &Operation{Method: MethodGet, Path: "/owners"},
					PaginationStyle: PaginationNone,
				},
				{
					Name:            "pets",
					ResourceName:    "pet",
					CollectionPath:  "/pets",
					InstancePath:    "/pets/{petId}",
					List:            &Operation{Method: MethodGet, Path: "/pets"},
					PaginationStyle: PaginationNone,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferListResources(tt.resources)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InferListResources() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDetectPaginationStyle(t *testing.T) {
	tests := []struct {
		name string
		op   Operation
		want PaginationStyle
	}{
		{
			name: "x-pagination string offset",
			op: Operation{
				Method:     MethodGet,
				Path:       "/pets",
				Extensions: map[string]any{"x-pagination": "offset"},
			},
			want: PaginationOffset,
		},
		{
			name: "x-pagination map cursor",
			op: Operation{
				Method:     MethodGet,
				Path:       "/pets",
				Extensions: map[string]any{"x-pagination": map[string]any{"style": "cursor", "cursor_param": "after"}},
			},
			want: PaginationCursor,
		},
		{
			name: "x-pagination none",
			op: Operation{
				Method:     MethodGet,
				Path:       "/pets",
				Extensions: map[string]any{"x-pagination": "none"},
			},
			want: PaginationNone,
		},
		{
			name: "unknown x-pagination returns none instead of falling back",
			op: Operation{
				Method:     MethodGet,
				Path:       "/pets",
				Extensions: map[string]any{"x-pagination": map[string]any{"style": "rocket"}},
				Parameters: []Parameter{
					{Name: "page", In: "query"},
				},
			},
			want: PaginationNone,
		},
		{
			name: "link header",
			op: Operation{
				Method:          MethodGet,
				Path:            "/pets",
				ResponseHeaders: []string{"Link"},
			},
			want: PaginationLinkHeader,
		},
		{
			name: "link header case insensitive",
			op: Operation{
				Method:          MethodGet,
				Path:            "/pets",
				ResponseHeaders: []string{"link"},
			},
			want: PaginationLinkHeader,
		},
		{
			name: "link header found among multiple headers",
			op: Operation{
				Method:          MethodGet,
				Path:            "/pets",
				ResponseHeaders: []string{"X-RateLimit-Remaining", "Link", "Content-Type"},
			},
			want: PaginationLinkHeader,
		},
		{
			name: "query page parameter",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "page", In: "query"},
					{Name: "limit", In: "query"},
				},
			},
			want: PaginationOffset,
		},
		{
			name: "query offset parameter",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "offset", In: "query"},
					{Name: "per_page", In: "query"},
				},
			},
			want: PaginationOffset,
		},
		{
			name: "query cursor parameter wins over page",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "page", In: "query"},
					{Name: "cursor", In: "query"},
				},
			},
			want: PaginationCursor,
		},
		{
			name: "no pagination signals",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "status", In: "query"},
				},
			},
			want: PaginationNone,
		},
		{
			name: "query location is case insensitive",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "page", In: "Query"},
					{Name: "limit", In: "QUERY"},
				},
			},
			want: PaginationOffset,
		},
		{
			name: "path parameters ignored",
			op: Operation{
				Method: MethodGet,
				Path:   "/pets",
				Parameters: []Parameter{
					{Name: "page", In: "path"},
				},
			},
			want: PaginationNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPaginationStyle(tt.op)
			if got != tt.want {
				t.Errorf("DetectPaginationStyle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPaginationStyleMarshalText(t *testing.T) {
	style := PaginationOffset
	b, err := style.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(b) != "offset" {
		t.Errorf("MarshalText() = %q, want %q", string(b), "offset")
	}
}

func TestPaginationStyleUnmarshalText(t *testing.T) {
	var style PaginationStyle
	if err := style.UnmarshalText([]byte("cursor")); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if style != PaginationCursor {
		t.Errorf("UnmarshalText() = %q, want %q", style, PaginationCursor)
	}

	var invalid PaginationStyle
	if err := invalid.UnmarshalText([]byte("rocket")); err == nil {
		t.Errorf("UnmarshalText(%q) expected error, got nil", "rocket")
	}

	var empty PaginationStyle
	if err := empty.UnmarshalText([]byte("")); err == nil {
		t.Errorf("UnmarshalText(%q) expected error, got nil", "")
	}
}

func TestCollectionName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/pets", "pets"},
		{"/projects/{projectId}/tasks", "tasks"},
		{"/{id}/items", "items"},
		{"/{id}", "collection"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := collectionName(tt.path); got != tt.want {
				t.Errorf("collectionName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPaginationStyleRoundTrip(t *testing.T) {
	tests := []PaginationStyle{
		PaginationOffset,
		PaginationCursor,
		PaginationLinkHeader,
		PaginationNone,
	}
	for _, want := range tests {
		t.Run(string(want), func(t *testing.T) {
			b, err := want.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			var got PaginationStyle
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("UnmarshalText() error = %v", err)
			}
			if got != want {
				t.Errorf("round-trip = %q, want %q", got, want)
			}
		})
	}
}

func TestNormalizePaginationStyle(t *testing.T) {
	tests := []struct {
		in   string
		want PaginationStyle
	}{
		{"offset", PaginationOffset},
		{"page", PaginationOffset},
		{"cursor", PaginationCursor},
		{"link", PaginationLinkHeader},
		{"link_header", PaginationLinkHeader},
		{"none", PaginationNone},
		{"", PaginationNone},
		{"unknown", ""},
		{"Cursor", PaginationCursor},
		{"  LINK  ", PaginationLinkHeader},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizePaginationStyle(tt.in); got != tt.want {
				t.Errorf("normalizePaginationStyle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCloneOperation(t *testing.T) {
	original := &Operation{
		Method:          MethodGet,
		Path:            "/pets",
		Parameters:      []Parameter{{Name: "page", In: "query"}},
		ResponseHeaders: []string{"Link"},
		Extensions:      map[string]any{"x-pagination": "offset"},
		ResponseSchema:  &SchemaSpec{Type: "object"},
	}

	cloned := cloneOperation(original)
	if cloned == original {
		t.Fatal("cloneOperation returned the same pointer")
	}

	// The clone must preserve the original's values, not just isolate
	// mutation: a copy that drops Extensions silently breaks downstream
	// detection (e.g. pagination style from x-pagination).
	if cloned.Parameters[0].Name != "page" {
		t.Errorf("clone lost Parameters: got %q, want %q", cloned.Parameters[0].Name, "page")
	}
	if cloned.ResponseHeaders[0] != "Link" {
		t.Errorf("clone lost ResponseHeaders: got %q, want %q", cloned.ResponseHeaders[0], "Link")
	}
	if cloned.Extensions["x-pagination"] != "offset" {
		t.Errorf("clone lost Extensions: got %v, want %v", cloned.Extensions["x-pagination"], "offset")
	}

	cloned.Parameters[0].Name = "cursor"
	cloned.ResponseHeaders[0] = "X-Total"
	cloned.Extensions["x-pagination"] = "cursor"

	if original.Parameters[0].Name != "page" {
		t.Errorf("mutating clone Parameters affected original: got %q, want %q", original.Parameters[0].Name, "page")
	}
	if original.ResponseHeaders[0] != "Link" {
		t.Errorf("mutating clone ResponseHeaders affected original: got %q, want %q", original.ResponseHeaders[0], "Link")
	}
	if original.Extensions["x-pagination"] != "offset" {
		t.Errorf("mutating clone Extensions affected original: got %v, want %v", original.Extensions["x-pagination"], "offset")
	}

	// ResponseSchema is a shared pointer by design.
	cloned.ResponseSchema.Type = "array"
	if original.ResponseSchema.Type != "array" {
		t.Errorf("expected ResponseSchema pointer to be shared, original type = %q", original.ResponseSchema.Type)
	}
}

func TestInferDataSourcesListIsolation(t *testing.T) {
	list := &Operation{
		Method:     MethodGet,
		Path:       "/pets",
		Parameters: []Parameter{{Name: "page", In: "query"}},
	}
	r := ResourceCRUD{
		Name:           "pet",
		CollectionPath: "/pets",
		List:           list,
	}

	sources := InferDataSources([]ResourceCRUD{r})
	if len(sources) != 1 {
		t.Fatalf("InferDataSources() returned %d sources, want 1", len(sources))
	}
	sources[0].List.Parameters[0].Name = "changed"
	if list.Parameters[0].Name != "page" {
		t.Errorf("mutating inferred data source List affected source: got %q, want %q", list.Parameters[0].Name, "page")
	}

	list2 := &Operation{
		Method:     MethodGet,
		Path:       "/pets",
		Parameters: []Parameter{{Name: "page", In: "query"}},
	}
	r2 := ResourceCRUD{
		Name:           "pet",
		CollectionPath: "/pets",
		InstancePath:   "/pets/{petId}",
		Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}"},
		List:           list2,
	}

	lists := InferListResources([]ResourceCRUD{r2})
	if len(lists) != 1 {
		t.Fatalf("InferListResources() returned %d list resources, want 1", len(lists))
	}
	lists[0].List.Parameters[0].Name = "changed"
	if list2.Parameters[0].Name != "page" {
		t.Errorf("mutating inferred list resource List affected source: got %q, want %q", list2.Parameters[0].Name, "page")
	}
}

func TestInferDataSourcesPaginationFromResourceCRUD(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets": {
			MethodGet: {
				Method:          MethodGet,
				Path:            "/pets",
				ResponseHeaders: []string{"Link"},
			},
		},
		"/stores": {
			MethodGet: {
				Method:     MethodGet,
				Path:       "/stores",
				Extensions: map[string]any{"x-pagination": "cursor"},
			},
		},
	}

	resources := InferResourceCRUD(pathOps, false)
	sources := InferDataSources(resources)

	if len(sources) != 2 {
		t.Fatalf("InferDataSources() returned %d sources, want 2", len(sources))
	}

	want := map[string]PaginationStyle{
		"pets":   PaginationLinkHeader,
		"stores": PaginationCursor,
	}
	for _, s := range sources {
		if s.PaginationStyle != want[s.Name] {
			t.Errorf("source %q pagination style = %q, want %q", s.Name, s.PaginationStyle, want[s.Name])
		}
	}
}
