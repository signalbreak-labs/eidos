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

// TestObjectSchemaFromOperationArrayQueryParamIsList locks in N-14: an array
// query parameter on an action maps to a List of its element type (so the
// generated provider serializes one repeated query value per element), matching
// how data sources model the same parameter, instead of being silently
// stringified by mapParamType's array default.
func TestObjectSchemaFromOperationArrayQueryParamIsList(t *testing.T) {
	op := Operation{
		OperationID: "list-ships",
		Parameters: []Parameter{
			{Name: "tags", In: "query", Type: "array", ItemsType: "string"},
			{Name: "count", In: "query", Type: "integer"},
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
	if tags.Schema.Collection == nil || tags.Schema.Collection.Kind != ir.List {
		t.Fatalf("expected tags List collection, got %+v", tags.Schema)
	}
	if got := tags.Schema.Collection.ElementType.Type; got != ir.TypeString {
		t.Errorf("element type = %q, want string", got)
	}
}

// TestObjectSchemaFromOperationArrayQueryParamWarnsNonScalar locks in the
// fail-loud half of N-14: an array query parameter with non-scalar items is
// still modeled as a List of strings, but surfaces a Warning (mirroring
// paramSchemaIR) rather than being dropped or downgraded silently.
func TestObjectSchemaFromOperationArrayQueryParamWarnsNonScalar(t *testing.T) {
	op := Operation{
		OperationID: "list-ships",
		Parameters: []Parameter{
			{Name: "filters", In: "query", Type: "array", ItemsType: "object"},
		},
	}
	var diags diagnostics.Diagnostics
	ObjectSchemaFromOperationWithDiagnostics(op, &diags)
	if !hasWarning(diags, "non-scalar items") {
		t.Errorf("expected a non-scalar-items warning, got diags=%v", diags)
	}
}

// TestActionRequestBody_UnionDegradesToDynamicBody locks in the N-15 union
// branch: a request body that declares a oneOf/anyOf union (whether or not it
// also declares type: object) yields a single Dynamic `body` attribute plus a
// fail-loud Warning, never zero body attributes.
func TestActionRequestBody_UnionDegradesToDynamicBody(t *testing.T) {
	op := Operation{
		OperationID: "create-thing",
		RequestSchema: &SchemaSpec{
			Type: "object", // union of objects declared alongside type: object
			OneOf: []SchemaSpec{
				{Type: "object", RefName: "Cat", Properties: map[string]SchemaSpec{"meow": {Type: "string"}}},
				{Type: "object", RefName: "Dog", Properties: map[string]SchemaSpec{"bark": {Type: "string"}}},
			},
		},
	}
	var diags diagnostics.Diagnostics
	schema := ObjectSchemaFromOperationWithDiagnostics(op, &diags)
	if len(schema.Attributes) != 1 || schema.Attributes[0].Name != "body" {
		t.Fatalf("expected a single body attribute, got %+v", schema.Attributes)
	}
	if got := schema.Attributes[0].Schema.Type; got != ir.TypeDynamic {
		t.Errorf("body schema type = %q, want dynamic", got)
	}
	if !hasWarning(diags, "union request body") {
		t.Errorf("expected a union-degraded warning, got diags=%v", diags)
	}
}

// TestActionRequestBody_EmptyObjectDegradesToDynamicBody locks in the N-15
// empty-object branch: `type: object` with no declared properties yields a
// single Dynamic `body` attribute plus a fail-loud Warning instead of silently
// producing zero body attributes.
func TestActionRequestBody_EmptyObjectDegradesToDynamicBody(t *testing.T) {
	op := Operation{
		OperationID: "create-thing",
		RequestSchema: &SchemaSpec{
			Type: "object",
		},
	}
	var diags diagnostics.Diagnostics
	schema := ObjectSchemaFromOperationWithDiagnostics(op, &diags)
	if len(schema.Attributes) != 1 || schema.Attributes[0].Name != "body" {
		t.Fatalf("expected a single body attribute, got %+v", schema.Attributes)
	}
	if !hasWarning(diags, "empty object request body") {
		t.Errorf("expected an empty-object warning, got diags=%v", diags)
	}
}

// TestActionRequestBody_BodyBodyCollisionWarns locks in the N-15 collision
// branch: two request-body properties whose sanitized names collide (fooBar and
// foo_bar) keep exactly one configurable attribute but surface a Warning
// instead of being dropped silently.
func TestActionRequestBody_BodyBodyCollisionWarns(t *testing.T) {
	op := Operation{
		OperationID: "create-thing",
		RequestSchema: &SchemaSpec{
			Type:     "object",
			Required: []string{"fooBar"},
			Properties: map[string]SchemaSpec{
				"fooBar":  {Type: "string"},
				"foo_bar": {Type: "integer"},
			},
		},
	}
	var diags diagnostics.Diagnostics
	schema := ObjectSchemaFromOperationWithDiagnostics(op, &diags)
	count := 0
	for _, a := range schema.Attributes {
		if a.Name == "foo_bar" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one foo_bar attribute after body-body dedup, got %d: %+v", count, schema.Attributes)
	}
	if !hasWarning(diags, "dropped on name collision") {
		t.Errorf("expected a body-collision warning, got diags=%v", diags)
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

// hasInfoText reports whether diags contains an Info whose String contains s.
func hasInfoText(diags diagnostics.Diagnostics, s string) bool {
	for _, d := range diags {
		if d.Severity == diagnostics.Info && strings.Contains(d.String(), s) {
			return true
		}
	}
	return false
}

// TestRequestBodyAttributes_BodyFallbackDescription covers the scalar request
// body path, where the whole body collapses to a single `body` attribute and
// the schema's own description is the only prose available to document it.
func TestRequestBodyAttributes_BodyFallbackDescription(t *testing.T) {
	attrs := requestBodyAttributes(SchemaSpec{
		Type:        "string",
		Description: "Raw payload posted to the endpoint.",
	}, nil)
	if len(attrs) != 1 || attrs[0].Name != "body" {
		t.Fatalf("attrs = %+v, want a single \"body\" attribute", attrs)
	}
	if attrs[0].Description != "Raw payload posted to the endpoint." {
		t.Errorf("description = %q, want the schema's own text", attrs[0].Description)
	}
	if !attrs[0].Required {
		t.Error("a declared request body should be Required")
	}
}
