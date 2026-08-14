package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestParamSchemaIR_ArrayQuery covers the array-query-parameter modeling: an
// array query parameter is modeled as a List of the element primitive (default
// form+explode serialization emits one repeated query value per element), while
// the same array type in a non-query location (path/header) stays scalar.
func TestParamSchemaIR_ArrayQuery(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		typeStr   string
		itemsType string
		wantKind  ir.CollectionKind
		wantElem  ir.PrimitiveType
		wantArray bool
	}{
		{
			name:      "array query string items",
			in:        "query",
			typeStr:   "array",
			itemsType: "string",
			wantKind:  ir.List,
			wantElem:  ir.TypeString,
			wantArray: true,
		},
		{
			name:      "array query integer items",
			in:        "query",
			typeStr:   "array",
			itemsType: "integer",
			wantKind:  ir.List,
			wantElem:  ir.TypeInt,
			wantArray: true,
		},
		{
			name:      "array path stays scalar (default string)",
			in:        "path",
			typeStr:   "array",
			itemsType: "string",
			wantArray: false,
		},
		{
			name:      "array header stays scalar (default string)",
			in:        "header",
			typeStr:   "array",
			itemsType: "string",
			wantArray: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paramSchemaIR(tc.in, tc.typeStr, tc.itemsType, "", nil, "p")
			if !tc.wantArray {
				if got.Collection != nil {
					t.Fatalf("expected scalar (no collection) for %s, got %+v", tc.name, got)
				}
				if got.Type != ir.TypeString {
					t.Errorf("%s: Type = %q, want %q (default string)", tc.name, got.Type, ir.TypeString)
				}
				return
			}
			if got.Collection == nil {
				t.Fatalf("%s: expected a collection, got %+v", tc.name, got)
			}
			if got.Collection.Kind != tc.wantKind {
				t.Errorf("%s: collection kind = %v, want %v", tc.name, got.Collection.Kind, tc.wantKind)
			}
			if got.Collection.ElementType.Type != tc.wantElem {
				t.Errorf("%s: element type = %q, want %q", tc.name, got.Collection.ElementType.Type, tc.wantElem)
			}
		})
	}
}

// TestParamSchemaIR_ArrayQueryWarnings covers the fail-loud warnings: a
// non-scalar element and a non-form serialization style are surfaced rather than
// dropped silently. The common case (scalar items, default form style) emits no
// warning.
func TestParamSchemaIR_ArrayQueryWarnings(t *testing.T) {
	t.Run("scalar items default style no warning", func(t *testing.T) {
		var diags diagnostics.Diagnostics
		paramSchemaIR("query", "array", "string", "", &diags, "expand")
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics for scalar+default, got %d: %v", len(diags), diags)
		}
	})

	t.Run("non-scalar items warns", func(t *testing.T) {
		var diags diagnostics.Diagnostics
		paramSchemaIR("query", "array", "object", "", &diags, "filters")
		if len(diags) != 1 {
			t.Fatalf("expected 1 diagnostic for non-scalar items, got %d: %v", len(diags), diags)
		}
		if diags[0].Severity != diagnostics.Warning {
			t.Errorf("severity = %v, want Warning", diags[0].Severity)
		}
		if diags[0].Summary == "" || !strings.Contains(diags[0].Summary, "non-scalar") {
			t.Errorf("summary %q should mention non-scalar items", diags[0].Summary)
		}
	})

	t.Run("non-form style warns", func(t *testing.T) {
		var diags diagnostics.Diagnostics
		paramSchemaIR("query", "array", "string", "spaceDelimited", &diags, "ids")
		if len(diags) != 1 {
			t.Fatalf("expected 1 diagnostic for non-form style, got %d: %v", len(diags), diags)
		}
		if diags[0].Severity != diagnostics.Warning {
			t.Errorf("severity = %v, want Warning", diags[0].Severity)
		}
		if diags[0].Summary == "" || !strings.Contains(diags[0].Summary, "style") {
			t.Errorf("summary %q should mention style", diags[0].Summary)
		}
	})
}

// TestDataSourceSchema_ArrayQueryParam covers the end-to-end data source path:
// an array query parameter becomes an Optional List attribute of the element
// primitive.
func TestDataSourceSchema_ArrayQueryParam(t *testing.T) {
	op := Operation{
		Method: MethodGet,
		Path:   "/accounts",
		Parameters: []Parameter{
			{Name: "expand", In: "query", Type: "array", ItemsType: "string"},
			{Name: "limit", In: "query", Type: "integer"},
		},
	}
	schema := DataSourceSchema(op, nil)
	var expand, limit *ir.AttributeIR
	for i := range schema.Attributes {
		switch schema.Attributes[i].Name {
		case "expand":
			expand = &schema.Attributes[i]
		case "limit":
			limit = &schema.Attributes[i]
		}
	}
	if expand == nil {
		t.Fatalf("missing expand attribute: %+v", schema.Attributes)
	}
	if expand.Schema.Collection == nil || expand.Schema.Collection.Kind != ir.List {
		t.Fatalf("expand should be a List collection, got %+v", expand.Schema)
	}
	if expand.Schema.Collection.ElementType.Type != ir.TypeString {
		t.Errorf("expand element type = %q, want string", expand.Schema.Collection.ElementType.Type)
	}
	if !expand.Optional {
		t.Errorf("expand should be Optional, got Required=%v Optional=%v", expand.Required, expand.Optional)
	}
	if limit == nil || limit.Schema.Type != ir.TypeInt {
		t.Errorf("limit should be an integer primitive, got %+v", limit)
	}
}
