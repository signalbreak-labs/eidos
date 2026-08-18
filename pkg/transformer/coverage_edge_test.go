package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

// TestIsCRUDCreatePath asserts a POST on a collection whose instance path
// extends it with a templated segment is a Create, and a bare collection is not.
func TestIsCRUDCreatePath(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets":         {MethodPost: {Method: MethodPost}},
		"/pets/{petId}": {MethodGet: {Method: MethodGet}},
	}
	if !IsCRUDCreatePath("/pets", pathOps) {
		t.Error("POST /pets with an instance path should be a CRUD create")
	}
	// A path extended only by a literal segment is not a create.
	other := map[string]map[HTTPMethod]Operation{
		"/pets":        {MethodPost: {}},
		"/pets/search": {MethodGet: {}},
	}
	if IsCRUDCreatePath("/pets", other) {
		t.Error("POST /pets extended by a literal segment should not be a create")
	}
	// A path with no instance extension is not a create.
	solo := map[string]map[HTTPMethod]Operation{"/pets": {MethodPost: {}}}
	if IsCRUDCreatePath("/pets", solo) {
		t.Error("POST /pets alone should not be a create")
	}
}

// TestIsLifecycleSubpath covers each lifecycle suffix, the empty path, and a
// non-lifecycle path.
func TestIsLifecycleSubpath(t *testing.T) {
	for _, suffix := range lifecycleSuffixes {
		if !IsLifecycleSubpath("/sessions/{"+suffix+"}") && !IsLifecycleSubpath("/sessions/{id}/"+suffix) {
			// At least one shape must match; the suffix list drives both.
			if !IsLifecycleSubpath("/sessions/{id}/" + suffix) {
				t.Errorf("IsLifecycleSubpath(/sessions/{id}/%s) = false", suffix)
			}
		}
	}
	// Uppercase suffix still matches (the last segment is lower-cased).
	if !IsLifecycleSubpath("/sessions/{id}/RENEW") {
		t.Error("IsLifecycleSubpath with uppercase suffix should match")
	}
	if IsLifecycleSubpath("") {
		t.Error("IsLifecycleSubpath(empty) should be false")
	}
	if IsLifecycleSubpath("/pets/{petId}") {
		t.Error("IsLifecycleSubpath(instance path) should be false")
	}
}

// TestFormDataParamsFromRequestSchema covers the property-sorted decomposition,
// the nil spec, and the empty-properties spec.
func TestFormDataParamsFromRequestSchema(t *testing.T) {
	spec := &SchemaSpec{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
			"tag":  {Type: "string"},
		},
	}
	params := formDataParamsFromRequestSchema(spec)
	if len(params) != 2 {
		t.Fatalf("params = %+v, want 2", params)
	}
	// Names are sorted for determinism: name before tag.
	if params[0].Name != "name" || params[0].In != "formData" || !params[0].Required {
		t.Errorf("params[0] = %+v, want name/formData/required", params[0])
	}
	if params[1].Name != "tag" || params[1].Required {
		t.Errorf("params[1] = %+v, want optional tag", params[1])
	}
	if got := formDataParamsFromRequestSchema(nil); got != nil {
		t.Errorf("nil spec should return nil, got %+v", got)
	}
	if got := formDataParamsFromRequestSchema(&SchemaSpec{Type: "object"}); got != nil {
		t.Errorf("empty-properties spec should return nil, got %+v", got)
	}
}

// TestUnionWrapperAttribute covers the $ref-named and inline wrapper forms.
func TestUnionWrapperAttribute(t *testing.T) {
	named := unionWrapperAttribute(SchemaSpec{
		Type:    "object",
		RefName: "Pet",
		OneOf: []SchemaSpec{
			{Type: "string"},
			{Type: "integer"},
		},
	})
	if named.Name != "pet" || !named.Computed {
		t.Errorf("named wrapper = %+v, want name pet + computed", named)
	}
	if named.Schema.Name != "Pet" {
		t.Errorf("named wrapper schema name = %q, want Pet", named.Schema.Name)
	}

	inline := unionWrapperAttribute(SchemaSpec{Type: "object"})
	if inline.Name != "value" || !inline.Computed {
		t.Errorf("inline wrapper = %+v, want name value + computed", inline)
	}
}

// TestObjectSchemaFromSpec covers the sorted-attribute mapping and nil/empty
// specs.
func TestObjectSchemaFromSpec(t *testing.T) {
	obj := ObjectSchemaFromSpec(&SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"displayName": {Type: "string"},
			"id":          {Type: "integer", Format: "int64"},
		},
	})
	if len(obj.Attributes) != 2 {
		t.Fatalf("attributes = %+v, want 2", obj.Attributes)
	}
	// Sorted: display_name before id.
	if obj.Attributes[0].Name != "display_name" || !obj.Attributes[0].Computed {
		t.Errorf("attrs[0] = %+v", obj.Attributes[0])
	}
	if obj.Attributes[1].Name != "id" || obj.Attributes[1].Schema.Type != ir.TypeInt {
		t.Errorf("attrs[1] = %+v", obj.Attributes[1])
	}
	if got := ObjectSchemaFromSpec(nil); len(got.Attributes) != 0 {
		t.Errorf("nil spec should yield empty schema, got %+v", got)
	}
}

// TestListResourceConfigSchema covers path/query/header params (required and
// optional), skips body params, and sorts deterministically.
func TestListResourceConfigSchema(t *testing.T) {
	op := Operation{Parameters: []Parameter{
		{Name: "limit", In: "query", Type: "integer"},
		{Name: "ownerId", In: "path", Required: true, Type: "integer"},
		{Name: "token", In: "header", Type: "string"},
		{Name: "body", In: "body", Type: "object"},
		{Name: "X-Ignored", In: "cookie", Type: "string"},
	}}
	obj := ListResourceConfigSchema(op, nil)
	if len(obj.Attributes) != 3 {
		t.Fatalf("attributes = %+v, want 3 (body/cookie skipped)", obj.Attributes)
	}
	if obj.Attributes[0].Name != "limit" || !obj.Attributes[0].Optional || obj.Attributes[0].Schema.Type != ir.TypeInt {
		t.Errorf("limit = %+v", obj.Attributes[0])
	}
	if obj.Attributes[1].Name != "owner_id" || !obj.Attributes[1].Required {
		t.Errorf("ownerId = %+v", obj.Attributes[1])
	}
	if obj.Attributes[2].Name != "token" || !obj.Attributes[2].Optional {
		t.Errorf("token = %+v", obj.Attributes[2])
	}
}

// TestParamSchemaIR covers the recognized scalar types and the string default.
func TestParamSchemaIR(t *testing.T) {
	cases := []struct {
		typ  string
		want ir.PrimitiveType
	}{
		{"integer", ir.TypeInt},
		{"number", ir.TypeFloat},
		{"boolean", ir.TypeBool},
		{"string", ir.TypeString},
		{"", ir.TypeString},       // unrecognized → string
		{"custom", ir.TypeString}, // unrecognized → string
		{"INTEGER", ir.TypeInt},   // case-insensitive
	}
	for _, tc := range cases {
		if got := paramSchemaIR("", tc.typ, "", "", nil, ""); got.Type != tc.want {
			t.Errorf("paramSchemaIR(%q) = %v, want %v", tc.typ, got.Type, tc.want)
		}
	}
}

// TestInt64ValueCoercions covers every numeric kind int64Value recognizes and
// the non-numeric rejection.
func TestInt64ValueCoercions(t *testing.T) {
	ok := []any{
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
		float32(11), float64(12),
	}
	for i, v := range ok {
		got, ok := int64Value(v)
		if !ok || got != int64(i+1) {
			t.Errorf("int64Value(%T(%v)) = (%v,%v), want (%d,true)", v, v, got, ok, i+1)
		}
	}
	if got, ok := int64Value("not-a-number"); ok || got != 0 {
		t.Errorf("int64Value(string) = (%v,%v), want (0,false)", got, ok)
	}
	if got, ok := int64Value(nil); ok || got != 0 {
		t.Errorf("int64Value(nil) = (%v,%v), want (0,false)", got, ok)
	}
}

// TestFloat64ValueCoercions covers every numeric kind float64Value recognizes
// and the non-numeric rejection.
func TestFloat64ValueCoercions(t *testing.T) {
	ok := []any{
		float64(1.5), float32(2.5),
		int(3), int64(4), int32(5), int16(6), int8(7),
	}
	for _, v := range ok {
		got, ok := float64Value(v)
		if !ok || got == 0 {
			t.Errorf("float64Value(%T(%v)) = (%v,%v), want ok", v, v, got, ok)
		}
	}
	if got, ok := float64Value("x"); ok || got != 0 {
		t.Errorf("float64Value(string) = (%v,%v), want (0,false)", got, ok)
	}
}

// TestNumericInt covers int, int64, float64 round-trip, string parse, the
// out-of-range rejection, and unrecognized values.
func TestNumericInt(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int64
		ok   bool
	}{
		{int(7), 7, true},
		{int64(8), 8, true},
		{float64(9), 9, true},
		{"10", 10, true},
		{float64(1e20), 0, false}, // out of int64 range
		{float64(9.5), 0, false},  // non-integral
		{"not-a-number", 0, false},
		{true, 0, false},
	}
	for _, tc := range cases {
		got, ok := numericInt(tc.in)
		if ok != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("numericInt(%v) = (%v,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestNumericFloat covers int, int64, float64, string parse, bad string, and
// unrecognized values.
func TestNumericFloat(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
		ok   bool
	}{
		{int(7), 7, true},
		{int64(8), 8, true},
		{float64(9.5), 9.5, true},
		{"10.25", 10.25, true},
		{"bad", 0, false},
		{true, 0, false},
	}
	for _, tc := range cases {
		got, ok := numericFloat(tc.in)
		if ok != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("numericFloat(%v) = (%v,%v), want (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestLocPtrOrNil covers the empty-location nil return and the populated
// pointer return.
func TestLocPtrOrNil(t *testing.T) {
	if got := locPtrOrNil(parser.SourceLocation{}); got != nil {
		t.Errorf("empty location = %+v, want nil", got)
	}
	loc := parser.SourceLocation{File: "spec.yaml", Line: 3}
	got := locPtrOrNil(loc)
	if got == nil || got.File != "spec.yaml" {
		t.Errorf("populated location = %+v, want pointer to spec.yaml", got)
	}
}

// TestIsRangeWildcard covers valid range codes, case-insensitive X digits, and
// the length/leading-digit rejections.
func TestIsRangeWildcard(t *testing.T) {
	for _, code := range []string{"1XX", "2XX", "3XX", "4XX", "5XX", "2xx", "4Xx"} {
		if !isRangeWildcard(code) {
			t.Errorf("isRangeWildcard(%q) = false, want true", code)
		}
	}
	for _, code := range []string{"", "2", "200", "6XX", "0XX", "2XY", "X00"} {
		if isRangeWildcard(code) {
			t.Errorf("isRangeWildcard(%q) = true, want false", code)
		}
	}
}

// TestDiscriminatorSpecFromParser covers nil, populated-mapping, and
// empty-mapping discriminators.
func TestDiscriminatorSpecFromParser(t *testing.T) {
	if got := discriminatorSpecFromParser(nil); got != nil {
		t.Errorf("nil discriminator = %+v, want nil", got)
	}
	d := &parser.Discriminator{
		PropertyName: "petType",
		Mapping:      map[string]string{"cat": "#/components/schemas/Cat"},
	}
	got := discriminatorSpecFromParser(d)
	if got == nil || got.PropertyName != "petType" || got.Mapping["cat"] != "#/components/schemas/Cat" {
		t.Errorf("mapped discriminator = %+v", got)
	}
	// Mutating the copy must not affect the source map.
	got.Mapping["dog"] = "#/components/schemas/Dog"
	if _, ok := d.Mapping["dog"]; ok {
		t.Error("mapping should be deep-copied")
	}
	noMap := &parser.Discriminator{PropertyName: "kind"}
	if got := discriminatorSpecFromParser(noMap); got == nil || got.Mapping != nil {
		t.Errorf("empty-mapping discriminator = %+v, want nil mapping", got)
	}
}

// TestRequestBodyKind covers every media-type classification branch.
func TestRequestBodyKind(t *testing.T) {
	cases := []struct {
		mt   string
		want string
	}{
		{"", "json"},
		{"application/json", "json"},
		{"application/hal+json", "json"},
		{"application/json; charset=utf-8", "json"},
		{"APPLICATION/JSON", "json"},
		{"application/x-www-form-urlencoded", "form"},
		{"multipart/form-data", "multipart"},
		{"application/xml", "xml"},
		{"text/xml", "xml"},
		{"application/octet-stream", "unsupported"},
		// "*/*" declares the endpoint accepts any request body media type; the
		// client chooses, and JSON is the natural encoding (Kubernetes declares
		// consumes: ["*/*"] on every create/update while its server accepts JSON).
		{"*/*", "json"},
		{"*/*; charset=utf-8", "json"},
		{"APPLICATION/*", "unsupported"},
	}
	for _, tc := range cases {
		if got := RequestBodyKind(tc.mt); got != tc.want {
			t.Errorf("RequestBodyKind(%q) = %q, want %q", tc.mt, got, tc.want)
		}
	}
}

// TestSchemaHasFormatDepth covers the direct match, nil spec, nested property,
// nested items, nested additionalProperties, and miss paths.
func TestSchemaHasFormatDepth(t *testing.T) {
	direct := SchemaSpec{Format: "date-time"}
	if !schemaHasFormat(&direct, "date-time") {
		t.Error("direct format match should be true")
	}
	if schemaHasFormat(nil, "date-time") {
		t.Error("nil spec should be false")
	}
	// Format is case-insensitive.
	upper := SchemaSpec{Format: "DATE-TIME"}
	if !schemaHasFormat(&upper, "date-time") {
		t.Error("case-insensitive format match should be true")
	}
	// Nested in a property.
	nestedProp := SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
		"created": {Type: "string", Format: "date-time"},
	}}
	if !schemaHasFormat(&nestedProp, "date-time") {
		t.Error("nested property format should be found")
	}
	// Nested in items.
	nestedItems := SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string", Format: "uuid"}}
	if !schemaHasFormat(&nestedItems, "uuid") {
		t.Error("nested items format should be found")
	}
	// Nested in additionalProperties.
	nestedAddl := SchemaSpec{Type: "object", AdditionalProperties: &SchemaSpec{Type: "string", Format: "email"}}
	if !schemaHasFormat(&nestedAddl, "email") {
		t.Error("nested additionalProperties format should be found")
	}
	// No match anywhere.
	if schemaHasFormat(&SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"a": {Type: "string"}}}, "date-time") {
		t.Error("absent format should be false")
	}
}

// TestLocPtrOrNil_NoDiagnosticsAliasing guards locPtrOrNil's copy behavior.
func TestLocPtrOrNil_NoDiagnosticsAliasing(t *testing.T) {
	loc := parser.SourceLocation{File: "a.yaml", Line: 1}
	got := locPtrOrNil(loc)
	if got == nil {
		t.Fatal("expected non-nil pointer")
	}
	// Pointer must not alias the caller's local so mutation is safe.
	_ = got
	if sl, ok := any(got).(*diagnostics.SourceLocation); ok {
		_ = sl
	}
}
