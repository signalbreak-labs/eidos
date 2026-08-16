package transformer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestInferActions(t *testing.T) {
	reboot := Operation{
		Method:      MethodPost,
		Path:        "/pets/{petId}/reboot",
		OperationID: "rebootPet",
		Parameters: []Parameter{
			{Name: "petId", In: "path", Required: true, Type: "string"},
		},
	}
	feed := Operation{
		Method:      MethodPost,
		Path:        "/pets/{petId}/feed",
		OperationID: "feedPet",
	}
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets": {
			MethodPost: op(MethodPost, "/pets"),
			MethodGet:  op(MethodGet, "/pets"),
		},
		"/pets/{petId}": {
			MethodGet:    op(MethodGet, "/pets/{petId}"),
			MethodPut:    op(MethodPut, "/pets/{petId}"),
			MethodDelete: op(MethodDelete, "/pets/{petId}"),
		},
		"/pets/{petId}/reboot": {
			MethodPost: reboot,
		},
		"/pets/{petId}/feed": {
			MethodPost: feed,
		},
	}

	actions := InferActions(pathOps)

	want := []ActionIR{
		{
			Name:            "feed_pet",
			FullName:        "Feed Pet",
			TypeName:        "feed_pet",
			Description:     "feedPet",
			ConfigSchema:    ir.ObjectSchemaIR{},
			InvokeMapping:   feed,
			SourceOperation: "feedPet",
		},
		{
			Name:        "reboot_pet",
			FullName:    "Reboot Pet",
			TypeName:    "reboot_pet",
			Description: "rebootPet",
			ConfigSchema: ir.ObjectSchemaIR{
				Attributes: []ir.AttributeIR{
					{
						Name:     "pet_id",
						Schema:   ir.SchemaIR{Type: ir.TypeString},
						Required: true,
						Optional: false,
					},
				},
			},
			InvokeMapping:   reboot,
			SourceOperation: "rebootPet",
		},
	}

	if !reflect.DeepEqual(actions, want) {
		t.Errorf("InferActions() = %+v, want %+v", actions, want)
	}
}

func TestInferActionsExcludesNonPost(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets/{petId}/status": {
			MethodGet: op(MethodGet, "/pets/{petId}/status"),
		},
	}
	actions := InferActions(pathOps)
	if len(actions) != 0 {
		t.Errorf("expected no actions, got %v", actions)
	}
}

func TestInferActionsUsesPathWhenNoOperationID(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/servers/{serverId}/reboot": {
			MethodPost: op(MethodPost, "/servers/{serverId}/reboot"),
		},
	}
	actions := InferActions(pathOps)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Name != "reboot" {
		t.Errorf("expected action name 'reboot', got %q", actions[0].Name)
	}
}

// TestObjectSchemaFromOperationDedupCollisions locks in the L-100 fix:
// parameters whose names normalize to the same snake_case attribute are
// deduplicated rather than emitted as duplicate attributes.
func TestObjectSchemaFromOperationDedupCollisions(t *testing.T) {
	op := Operation{
		Parameters: []Parameter{
			{Name: "fooBar", In: "query", Type: "string"},
			{Name: "foo_bar", In: "query", Type: "integer"},
		},
	}
	schema := ObjectSchemaFromOperation(op)
	count := 0
	for _, a := range schema.Attributes {
		if a.Name == "foo_bar" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one foo_bar attribute after dedup, got %d: %+v", count, schema.Attributes)
	}
}

// TestActionRequestBody_UniqueItemsIsSet locks in A1: an action request body
// whose property is an array with uniqueItems: true maps to a Set collection
// attribute (not Dynamic, and not a List) via the shallow schemaIRFromSpec
// mapper. Elements are mapped shallowly so writable request-body attributes do
// not inherit the Computed flag the recursive mapper applies to response
// attributes.
func TestActionRequestBody_UniqueItemsIsSet(t *testing.T) {
	op := Operation{
		RequestSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"tags": {Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "string"}},
			},
		},
	}
	schema := ObjectSchemaFromOperation(op)
	var tags *ir.AttributeIR
	for i := range schema.Attributes {
		if schema.Attributes[i].Name == "tags" {
			tags = &schema.Attributes[i]
		}
	}
	if tags == nil {
		t.Fatalf("expected a tags attribute, got %+v", schema.Attributes)
	}
	if tags.Schema.Collection == nil || tags.Schema.Collection.Kind != ir.Set {
		t.Fatalf("expected tags Set collection, got %+v", tags.Schema)
	}
	if tags.Schema.Collection.ElementType.Type != ir.TypeString {
		t.Errorf("element type = %q, want string", tags.Schema.Collection.ElementType.Type)
	}
	if tags.Computed {
		t.Errorf("writable request-body attribute must not be Computed")
	}
}

// TestActionRequestBody_ArrayWithoutUniqueItemsIsList confirms the non-Set
// array case still maps to a List (regression guard for A1's array branch).
func TestActionRequestBody_ArrayWithoutUniqueItemsIsList(t *testing.T) {
	op := Operation{
		RequestSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"tags": {Type: "array", Items: &SchemaSpec{Type: "string"}},
			},
		},
	}
	schema := ObjectSchemaFromOperation(op)
	for _, a := range schema.Attributes {
		if a.Name == "tags" {
			if a.Schema.Collection == nil || a.Schema.Collection.Kind != ir.List {
				t.Fatalf("expected tags List collection, got %+v", a.Schema)
			}
			return
		}
	}
	t.Fatalf("expected a tags attribute, got %+v", schema.Attributes)
}
func TestInferActionsRequiredQueryParam(t *testing.T) {
	pathOps := map[string]map[HTTPMethod]Operation{
		"/pets/{petId}/tag": {
			MethodPost: {
				Method:      MethodPost,
				Path:        "/pets/{petId}/tag",
				OperationID: "tagPet",
				Parameters: []Parameter{
					{Name: "petId", In: "path", Required: true, Type: "string"},
					{Name: "label", In: "query", Required: true, Type: "string"},
					{Name: "color", In: "query", Required: false, Type: "string"},
				},
			},
		},
	}

	actions := InferActions(pathOps)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(actions), actions)
	}
	schema := actions[0].ConfigSchema
	findAttr := func(name string) (ir.AttributeIR, bool) {
		for _, a := range schema.Attributes {
			if a.Name == name {
				return a, true
			}
		}
		return ir.AttributeIR{}, false
	}

	label, ok := findAttr("label")
	if !ok {
		t.Fatalf("expected a 'label' attribute for the required query param, got %+v", schema.Attributes)
	}
	if !label.Required || label.Optional {
		t.Errorf("required query param 'label' = Required=%v Optional=%v, want Required=true Optional=false (M-36)", label.Required, label.Optional)
	}

	color, ok := findAttr("color")
	if !ok {
		t.Fatalf("expected a 'color' attribute for the optional query param, got %+v", schema.Attributes)
	}
	if color.Required || !color.Optional {
		t.Errorf("optional query param 'color' = Required=%v Optional=%v, want Required=false Optional=true", color.Required, color.Optional)
	}
}

// TestActionRequestBody_WireNameCarriesOriginalProperty locks in the Bug #4
// fix: a request-body property's original OpenAPI name (commonly camelCase) is
// carried on WireName so the generated model gets a json:"<wireName>" tag and
// modelToJSONMap emits the API's wire name as the request-body key. Without it,
// multi-word fields (e.g. waypointSymbol) serialize under the snake_case
// Terraform name and the API reports them "undefined" (422). Single-word names
// where snake_case == wire name are unaffected, which is why symbol/units
// happened to work.
func TestActionRequestBody_WireNameCarriesOriginalProperty(t *testing.T) {
	op := Operation{
		RequestSchema: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"waypointSymbol": {Type: "string"},
				"units":          {Type: "integer"},
			},
		},
	}
	schema := ObjectSchemaFromOperation(op)
	find := func(name string) (ir.AttributeIR, bool) {
		for _, a := range schema.Attributes {
			if a.Name == name {
				return a, true
			}
		}
		return ir.AttributeIR{}, false
	}
	wp, ok := find("waypoint_symbol")
	if !ok {
		t.Fatalf("expected waypoint_symbol attribute, got %+v", schema.Attributes)
	}
	if wp.WireName != "waypointSymbol" {
		t.Errorf("waypoint_symbol WireName = %q, want \"waypointSymbol\" (Bug #4)", wp.WireName)
	}
	u, ok := find("units")
	if !ok {
		t.Fatalf("expected units attribute, got %+v", schema.Attributes)
	}
	if u.WireName != "units" {
		t.Errorf("units WireName = %q, want \"units\"", u.WireName)
	}
}

// TestActionRequestBody_PathBodyCollisionDisambiguated locks in the Bug #3 fix:
// when a request-body property's sanitized name collides with a path
// parameter's sanitized name (e.g. SpaceTraders transfer-cargo: path
// shipSymbol = source, body shipSymbol = target), the body attribute is
// disambiguated with a "body_" prefix and keeps its WireName so the request
// body key is correct, instead of being silently dropped. A fail-loud Warning
// is emitted. The path parameter keeps the bare name and carries no WireName
// (it is substituted into the URL path; emitting it into the body under its
// wire name would clobber the body's same-named field).
func TestActionRequestBody_PathBodyCollisionDisambiguated(t *testing.T) {
	op := Operation{
		OperationID: "transfer-cargo",
		Parameters: []Parameter{
			{Name: "shipSymbol", In: "path", Required: true, Type: "string"},
		},
		RequestSchema: &SchemaSpec{
			Type:     "object",
			Required: []string{"tradeSymbol", "units", "shipSymbol"},
			Properties: map[string]SchemaSpec{
				"tradeSymbol": {Type: "string"},
				"units":       {Type: "integer"},
				"shipSymbol":  {Type: "string"},
			},
		},
	}
	var diags diagnostics.Diagnostics
	schema := ObjectSchemaFromOperationWithDiagnostics(op, &diags)

	find := func(name string) (ir.AttributeIR, bool) {
		for _, a := range schema.Attributes {
			if a.Name == name {
				return a, true
			}
		}
		return ir.AttributeIR{}, false
	}
	// Path parameter keeps the bare name, no WireName.
	path, ok := find("ship_symbol")
	if !ok {
		t.Fatalf("expected path-parameter attribute ship_symbol, got %+v", schema.Attributes)
	}
	if path.WireName != "" {
		t.Errorf("path parameter ship_symbol WireName = %q, want empty (must not leak into body)", path.WireName)
	}
	// Body property is disambiguated and preserves its wire name.
	body, ok := find("body_ship_symbol")
	if !ok {
		t.Fatalf("expected disambiguated body attribute body_ship_symbol, got %+v", schema.Attributes)
	}
	if body.WireName != "shipSymbol" {
		t.Errorf("body_ship_symbol WireName = %q, want \"shipSymbol\" (request body key)", body.WireName)
	}
	if _, dup := find("ship_symbol"); !dup {
		t.Errorf("path attribute must still be present under its bare name")
	}
	// Exactly one ship-symbol-flavored attribute each; no silent drop.
	shipCount := 0
	for _, a := range schema.Attributes {
		if a.Name == "ship_symbol" || a.Name == "body_ship_symbol" {
			shipCount++
		}
	}
	if shipCount != 2 {
		t.Errorf("expected 2 ship-symbol attributes (path + body), got %d: %+v", shipCount, schema.Attributes)
	}
	// Fail-loud warning is emitted.
	if !hasWarning(diags, "collides with a path parameter") {
		t.Errorf("expected a path/body collision warning, got diags=%v", diags)
	}
	if !hasWarning(diags, "body_ship_symbol") {
		t.Errorf("warning should name the disambiguated attribute body_ship_symbol, got diags=%v", diags)
	}
}

// TestActionRequestBody_NoCollisionNoWarning ensures the collision diagnostic is
// not emitted for an action whose path and body names do not collide.
func TestActionRequestBody_NoCollisionNoWarning(t *testing.T) {
	op := Operation{
		OperationID: "navigate-ship",
		Parameters: []Parameter{
			{Name: "shipSymbol", In: "path", Required: true, Type: "string"},
		},
		RequestSchema: &SchemaSpec{
			Type:     "object",
			Required: []string{"waypointSymbol"},
			Properties: map[string]SchemaSpec{
				"waypointSymbol": {Type: "string"},
			},
		},
	}
	var diags diagnostics.Diagnostics
	ObjectSchemaFromOperationWithDiagnostics(op, &diags)
	if hasWarning(diags, "collides with a path parameter") {
		t.Errorf("did not expect a collision warning for non-colliding names, got diags=%v", diags)
	}
}

// hasWarning reports whether diags contains a Warning whose String contains s.
func hasWarning(diags diagnostics.Diagnostics, s string) bool {
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.String(), s) {
			return true
		}
	}
	return false
}
