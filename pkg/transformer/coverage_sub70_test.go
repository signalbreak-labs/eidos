package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestOperationExcluded covers the skip-priority, include-only, and
// include-match branches of the operation filtering decision.
func TestOperationExcluded(t *testing.T) {
	if !operationExcluded("admin_delete", []string{"admin_*"}, nil) {
		t.Error("skip pattern should exclude the operation")
	}
	if operationExcluded("admin_delete", []string{"pet_*"}, nil) {
		t.Error("non-matching skip pattern should not exclude")
	}
	if operationExcluded("pet_get", nil, nil) {
		t.Error("no filters should keep everything")
	}
	if operationExcluded("pet_get", nil, []string{"pet_*"}) {
		t.Error("operation matching an include pattern should be kept")
	}
	if !operationExcluded("owner_get", nil, []string{"pet_*"}) {
		t.Error("operation matching no include pattern should be excluded")
	}
	// Skip wins over include.
	if !operationExcluded("admin_get", []string{"admin_*"}, []string{"admin_*"}) {
		t.Error("skip pattern should take precedence over a matching include")
	}
}

// TestCreateFormDataParams covers the nil op, the direct formData parameter
// passthrough, and the form/multipart request-body decomposition fallback.
func TestCreateFormDataParams(t *testing.T) {
	if got := createFormDataParams(nil); got != nil {
		t.Errorf("nil op should return nil, got %+v", got)
	}
	direct := createFormDataParams(&Operation{Parameters: []Parameter{
		{Name: "name", In: "formData", Required: true, Type: "string"},
		{Name: "id", In: "path", Required: true, Type: "integer"},
	}})
	if len(direct) != 1 || direct[0].Name != "name" || !direct[0].Required {
		t.Errorf("direct formData params = %+v, want the single formData param", direct)
	}
	// No in:formData parameters remain, so the form-encoded request body schema
	// is decomposed back into per-field parameters.
	fallback := createFormDataParams(&Operation{
		RequestMediaType: "application/x-www-form-urlencoded",
		RequestSchema: &SchemaSpec{
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]SchemaSpec{
				"name": {Type: "string"},
			},
		},
	})
	if len(fallback) != 1 || fallback[0].In != "formData" {
		t.Errorf("form fallback = %+v, want one formData param", fallback)
	}
	// A JSON request body with no formData params yields nothing.
	if got := createFormDataParams(&Operation{RequestMediaType: "application/json"}); got != nil {
		t.Errorf("json request = %+v, want nil", got)
	}
}

// TestAppendFormDataOnlyAttributes covers the present-name skip, the required
// and optional branches, and name snake-casing.
func TestAppendFormDataOnlyAttributes(t *testing.T) {
	formData := []Parameter{
		{Name: "displayName", Required: true, Type: "string"},
		{Name: "tag", Required: false, Type: "string"},
	}
	attrs := appendFormDataOnlyAttributes(
		[]ir.AttributeIR{{Name: "display_name", Schema: ir.SchemaIR{Type: ir.TypeString}}},
		formData,
		map[string]bool{"displayName": true},
	)
	if len(attrs) != 2 {
		t.Fatalf("attrs = %+v, want 2 (display_name already present)", attrs)
	}
	// display_name was already present, so it is not duplicated; tag is appended
	// as an optional write-only input.
	if attrs[0].Name != "display_name" || attrs[0].Optional || attrs[0].Required {
		t.Errorf("attrs[0] = %+v, want the pre-existing display_name untouched", attrs[0])
	}
	if attrs[1].Name != "tag" || !attrs[1].Optional {
		t.Errorf("attrs[1] = %+v, want optional tag", attrs[1])
	}
}

// TestDataSourceSchema covers input parameters (path/query/header), the
// cookie-skip, response-property merge keeping Required inputs Required, the
// union wrapper, and the array response (List and Set).
func TestDataSourceSchema(t *testing.T) {
	op := Operation{
		Parameters: []Parameter{
			{Name: "ownerId", In: "path", Required: true, Type: "integer"},
			{Name: "limit", In: "query", Type: "integer"},
			{Name: "token", In: "header", Type: "string"},
			{Name: "ignored", In: "cookie", Type: "string"},
		},
		ResponseSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"ownerId":   {Type: "string"}, // response type wins over the path param
				"createdAt": {Type: "string", Format: "date-time"},
			},
		},
	}
	obj := DataSourceSchema(op, nil)
	// ownerId path param merges with the response property into one attribute, so
	// the cookie param is the only one skipped: owner_id, limit, token, created_at.
	if len(obj.Attributes) != 4 {
		t.Fatalf("attributes = %+v, want 4 (cookie skipped, ownerId merged)", obj.Attributes)
	}
	byName := map[string]ir.AttributeIR{}
	for _, a := range obj.Attributes {
		byName[a.Name] = a
	}
	// Path param merged with the response property: response type, still
	// Required (never Required+Computed), and WireName from the response.
	owner := byName["owner_id"]
	if !owner.Required || owner.Computed || owner.Schema.Type != ir.TypeString || owner.WireName != "ownerId" {
		t.Errorf("owner_id = %+v, want required string from response", owner)
	}
	limit := byName["limit"]
	if !limit.Optional || limit.Required || limit.Schema.Type != ir.TypeInt {
		t.Errorf("limit = %+v, want optional int", limit)
	}
	if !byName["token"].Optional {
		t.Errorf("token = %+v, want optional header param", byName["token"])
	}
	if !byName["created_at"].Computed {
		t.Errorf("created_at = %+v, want computed response property", byName["created_at"])
	}
}

// TestDataSourceSchemaReservedNameParam ensures a path/query/header parameter
// whose name normalizes to a reserved Terraform root attribute name (e.g.
// "provider") is suffixed with "_" via SanitizeAttributeName — not left as the
// bare reserved name, which fails provider schema validation at runtime — and
// merges with a same-named response property under the sanitized key so the
// data source has one attribute, not two (L-102).
func TestDataSourceSchemaReservedNameParam(t *testing.T) {
	op := Operation{
		Parameters: []Parameter{
			{Name: "provider", In: "query", Type: "string"},
		},
		ResponseSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"provider": {Type: "string"},
			},
		},
	}
	obj := DataSourceSchema(op, nil)
	byName := map[string]ir.AttributeIR{}
	for _, a := range obj.Attributes {
		byName[a.Name] = a
	}
	if _, bad := byName["provider"]; bad {
		t.Errorf("found unsanitized reserved attribute \"provider\"; attrs=%+v", obj.Attributes)
	}
	merged, ok := byName["provider_"]
	if !ok {
		t.Fatalf("expected sanitized attribute \"provider_\"; attrs=%+v", obj.Attributes)
	}
	if merged.Schema.Type != ir.TypeString {
		t.Errorf("provider_ schema = %s, want string", merged.Schema.Type)
	}
}

// TestDataSourceSchemaParamNameCollision verifies that an optional query/header
// parameter whose name normalizes to the same attribute name as a required path
// parameter (e.g. GitLab's GET /api/v4/projects/{id}/terraform/state/{name},
// which declares a path param `id` and a query param `ID`, both → "id") does not
// produce an invalid Required+Optional attribute. The path param wins (it is the
// essential instance identifier); the colliding optional param is dropped and the
// collision is surfaced as a fail-loud warning rather than lost silently.
func TestDataSourceSchemaParamNameCollision(t *testing.T) {
	var diags diagnostics.Diagnostics
	op := Operation{
		Parameters: []Parameter{
			{Name: "id", In: "path", Required: true, Type: "integer"},
			{Name: "name", In: "path", Required: true, Type: "string"},
			{Name: "ID", In: "query", Required: false, Type: "integer"},
		},
		ResponseSchema: &SchemaSpec{
			Type:       "object",
			Properties: map[string]SchemaSpec{"content": {Type: "string"}},
		},
	}
	obj := DataSourceSchema(op, &diags)
	byName := map[string]ir.AttributeIR{}
	for _, a := range obj.Attributes {
		byName[a.Name] = a
		if a.Required && a.Optional {
			t.Errorf("attribute %q has both Required and Optional set: %+v", a.Name, a)
		}
	}
	id, ok := byName["id"]
	if !ok {
		t.Fatalf("expected merged id attribute; attrs=%+v", obj.Attributes)
	}
	if !id.Required || id.Optional {
		t.Errorf("id = %+v, want Required and not Optional (path param wins)", id)
	}
	// The dropped optional query param must surface a warning (fail-loud).
	found := false
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Summary, "collides") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a collision warning for the dropped optional query param; diags=%+v", diags)
	}
}

// TestDataSourceSchemaUnionResponse covers the top-level oneOf response wrapper.
func TestDataSourceSchemaUnionResponse(t *testing.T) {
	op := Operation{ResponseSchema: &SchemaSpec{
		Type:    "object",
		RefName: "Pet",
		OneOf:   []SchemaSpec{{Type: "object", RefName: "Cat"}, {Type: "object", RefName: "Dog"}},
	}}
	obj := DataSourceSchema(op, nil)
	if len(obj.Attributes) != 1 || obj.Attributes[0].Name != "pet" || !obj.Attributes[0].Computed {
		t.Errorf("union response = %+v, want computed pet wrapper", obj.Attributes)
	}
}

// TestDataSourceSchemaArrayResponse covers List and Set array responses.
func TestDataSourceSchemaArrayResponse(t *testing.T) {
	listOp := Operation{ResponseSchema: &SchemaSpec{
		Type:  "array",
		Items: &SchemaSpec{Type: "string"},
	}}
	obj := DataSourceSchema(listOp, nil)
	if len(obj.Attributes) != 1 {
		t.Fatalf("list response = %+v, want one items attribute", obj.Attributes)
	}
	items := obj.Attributes[0]
	if items.Name != "items" || !items.Computed || items.Schema.Collection == nil || items.Schema.Collection.Kind != ir.List {
		t.Errorf("list items = %+v, want computed List", items)
	}
	setOp := Operation{ResponseSchema: &SchemaSpec{
		Type:        "array",
		UniqueItems: true,
		Items:       &SchemaSpec{Type: "string"},
	}}
	set := DataSourceSchema(setOp, nil)
	if set.Attributes[0].Schema.Collection.Kind != ir.Set {
		t.Errorf("uniqueItems response = %+v, want Set", set.Attributes[0])
	}
}

// TestInferNotValidators covers the nil, string, int, float, bool, and
// unsupported-type branches of the not→NoneOf mapping.
func TestInferNotValidators(t *testing.T) {
	if got := inferNotValidators(nil); got != nil {
		t.Errorf("nil not schema = %+v, want nil", got)
	}
	str := inferNotValidators(&ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a", "b"}})
	if len(str) != 1 || str[0].Type != "stringvalidator.NoneOf" {
		t.Errorf("string not = %+v", str)
	}
	// A non-string value in a string enum renders with %v, never dropped.
	mixed := inferNotValidators(&ir.SchemaIR{Type: ir.TypeString, EnumValues: []any{"a", 42}})
	if len(mixed) != 1 || len(mixed[0].Args) != 2 {
		t.Errorf("mixed string not = %+v", mixed)
	}
	ints := inferNotValidators(&ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{1, 2}})
	if len(ints) != 1 || ints[0].Type != "int64validator.NoneOf" {
		t.Errorf("int not = %+v", ints)
	}
	// An int not whose values all fail to render as int64 yields no validator.
	if got := inferNotValidators(&ir.SchemaIR{Type: ir.TypeInt, EnumValues: []any{"x"}}); got != nil {
		t.Errorf("bad int not = %+v, want nil", got)
	}
	floats := inferNotValidators(&ir.SchemaIR{Type: ir.TypeFloat, EnumValues: []any{1.5}})
	if len(floats) != 1 || floats[0].Type != "float64validator.NoneOf" {
		t.Errorf("float not = %+v", floats)
	}
	bools := inferNotValidators(&ir.SchemaIR{Type: ir.TypeBool, EnumValues: []any{true, false}})
	if len(bools) != 1 || bools[0].Type != "boolvalidator.NoneOf" {
		t.Errorf("bool not = %+v", bools)
	}
	// Non-enum not and unsupported types yield nothing.
	if got := inferNotValidators(&ir.SchemaIR{Type: ir.TypeString}); got != nil {
		t.Errorf("non-enum not = %+v, want nil", got)
	}
	if got := inferNotValidators(&ir.SchemaIR{Type: "custom", EnumValues: []any{1}}); got != nil {
		t.Errorf("custom not = %+v, want nil", got)
	}
}

// TestRenderIntValue covers the integral float64/float32, all int kinds, and
// the non-integral/unknown rejections.
func TestRenderIntValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{float64(9), "9", true},
		{float32(8), "8", true},
		{int(7), "7", true},
		{int64(6), "6", true},
		{int32(5), "5", true},
		{float64(2.5), "", false}, // non-integral float64
		{float32(1.5), "", false}, // non-integral float32
		{"x", "", false},          // unknown kind
	}
	for _, tc := range cases {
		got, ok := renderIntValue(tc.in)
		if ok != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("renderIntValue(%v) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestRenderFloatValue covers every numeric kind plus the %v fallback.
func TestRenderFloatValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(1.5), "1.5"},
		{float32(2.5), "2.5"},
		{int(3), "3"},
		{int64(4), "4"},
		{int32(5), "5"},
		{"x", "x"},
	}
	for _, tc := range cases {
		if got := renderFloatValue(tc.in); got != tc.want {
			t.Errorf("renderFloatValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNoneOfValidators covers empty, string, int, float, and unsupported-type
// branches.
func TestNoneOfValidators(t *testing.T) {
	if got := noneOfValidators(ir.TypeString, nil); got != nil {
		t.Errorf("empty values = %+v, want nil", got)
	}
	str := noneOfValidators(ir.TypeString, []interface{}{"a", "b"})
	if len(str) != 1 || str[0].Type != "stringvalidator.NoneOf" {
		t.Errorf("string = %+v", str)
	}
	if got := noneOfValidators(ir.TypeString, []interface{}{1}); got != nil {
		t.Errorf("non-string values = %+v, want nil", got)
	}
	ints := noneOfValidators(ir.TypeInt, []interface{}{1, 2})
	if len(ints) != 1 || ints[0].Type != "int64validator.NoneOf" {
		t.Errorf("int = %+v", ints)
	}
	floats := noneOfValidators(ir.TypeFloat, []interface{}{1.5})
	if len(floats) != 1 || floats[0].Type != "float64validator.NoneOf" {
		t.Errorf("float = %+v", floats)
	}
	if got := noneOfValidators(ir.TypeBool, []interface{}{true}); got != nil {
		t.Errorf("unsupported type = %+v, want nil", got)
	}
}

// TestFloat64ValueUintKinds completes the uint-family branches of float64Value
// that the scalar coercion test does not reach.
func TestFloat64ValueUintKinds(t *testing.T) {
	ok := []any{
		uint(1), uint64(2), uint32(3), uint16(4), uint8(5),
	}
	for i, v := range ok {
		got, ok := float64Value(v)
		if !ok || got != float64(i+1) {
			t.Errorf("float64Value(%T(%v)) = (%v,%v), want (%d,true)", v, v, got, ok, i+1)
		}
	}
	if got, ok := float64Value(nil); ok || got != 0 {
		t.Errorf("float64Value(nil) = (%v,%v), want (0,false)", got, ok)
	}
}
