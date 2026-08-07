package transformer

import (
	"reflect"
	"strings"
	"testing"
)

func TestMergeOpenAPIParametersCombinesPathAndOperation(t *testing.T) {
	pathParams := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "filter", In: "query", Schema: &Schema{Type: SchemaTypeString}},
	}
	opParams := []OpenAPIParameter{
		{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
	}

	got := MergeOpenAPIParameters(pathParams, opParams)
	want := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "filter", In: "query", Schema: &Schema{Type: SchemaTypeString}},
		{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestMergeOpenAPIParametersOperationOverridesPath(t *testing.T) {
	pathParams := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: false, Description: "path level", Schema: &Schema{Type: SchemaTypeString}},
	}
	opParams := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Description: "op level", Schema: &Schema{Type: SchemaTypeString}},
	}

	got := MergeOpenAPIParameters(pathParams, opParams)
	want := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Description: "op level", Schema: &Schema{Type: SchemaTypeString}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("override mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestMergeOpenAPIParametersSameNameDifferentLocation(t *testing.T) {
	pathParams := []OpenAPIParameter{
		{Name: "id", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
	}
	opParams := []OpenAPIParameter{
		{Name: "id", In: "query", Schema: &Schema{Type: SchemaTypeString}},
	}

	got := MergeOpenAPIParameters(pathParams, opParams)
	want := []OpenAPIParameter{
		{Name: "id", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "id", In: "query", Schema: &Schema{Type: SchemaTypeString}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("location mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestRefName(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"#/components/parameters/petId", "petId"},
		{"petId", "petId"},
		{"#/components/parameters/my~1param", "my/param"},
		{"#/components/parameters/my~0param", "my~param"},
		{"#/components/parameters/a~1b~0c", "a/b~c"},
		{"#/components/parameters/~01param", "~1param"},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			if got := refName(tc.ref); got != tc.want {
				t.Fatalf("refName(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestResolveOpenAPIParameterRefsSharesSchemaPointer(t *testing.T) {
	schema := &Schema{Type: SchemaTypeString}
	components := map[string]*OpenAPIParameter{
		"petId": {Name: "petId", In: "path", Required: true, Schema: schema},
	}
	params := []OpenAPIParameter{{Ref: "#/components/parameters/petId"}}

	got, err := ResolveOpenAPIParameterRefs(params, components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one resolved parameter, got %#v", got)
	}
	if got[0].Schema != schema {
		t.Fatalf("resolved parameter Schema is not shared with component entry")
	}
}

func TestResolveOpenAPIParameterRefs(t *testing.T) {
	components := map[string]*OpenAPIParameter{
		"petId": {Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
	}
	params := []OpenAPIParameter{
		{Ref: "#/components/parameters/petId"},
		{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
	}

	got, err := ResolveOpenAPIParameterRefs(params, components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolve mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestResolveOpenAPIParameterRefsClearsRef(t *testing.T) {
	components := map[string]*OpenAPIParameter{
		"petId": {Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
	}
	params := []OpenAPIParameter{{Ref: "petId"}}

	got, err := ResolveOpenAPIParameterRefs(params, components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Ref != "" {
		t.Fatalf("expected ref to be cleared, got %#v", got)
	}
}

func TestResolveOpenAPIParameterRefsMissing(t *testing.T) {
	params := []OpenAPIParameter{{Ref: "missing"}}
	_, err := ResolveOpenAPIParameterRefs(params, map[string]*OpenAPIParameter{})
	if err == nil {
		t.Fatalf("expected error for missing ref, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error does not mention missing ref: %v", err)
	}
}

func TestResolveOpenAPIParameterRefsNoComponents(t *testing.T) {
	params := []OpenAPIParameter{{Ref: "petId"}}
	_, err := ResolveOpenAPIParameterRefs(params, nil)
	if err == nil {
		t.Fatalf("expected error when components are nil, got nil")
	}
}

func TestNormalizeOperationOpenAPIParameters(t *testing.T) {
	components := map[string]*OpenAPIParameter{
		"petId": {Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
	}
	pathItem := &PathItem{
		OpenAPIParameters: []OpenAPIParameter{
			{Ref: "petId"},
			{Name: "filter", In: "query", Schema: &Schema{Type: SchemaTypeString}},
		},
	}
	op := &OpenAPIOperation{
		OpenAPIParameters: []OpenAPIParameter{
			{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
			{Name: "filter", In: "query", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		},
	}

	got, err := NormalizeOperationOpenAPIParameters(pathItem, op, components)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []OpenAPIParameter{
		{Name: "petId", In: "path", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "filter", In: "query", Required: true, Schema: &Schema{Type: SchemaTypeString}},
		{Name: "limit", In: "query", Schema: &Schema{Type: SchemaTypeInteger}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationOpenAPIParametersNilInputs(t *testing.T) {
	got, err := NormalizeOperationOpenAPIParameters(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty parameters, got %#v", got)
	}
}
