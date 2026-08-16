package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// petStateSpec mirrors the mycloud-pets Pet response schema: id (int64, required),
// name (string, required), tag (string, optional).
func petStateSpec() *SchemaSpec {
	return &SchemaSpec{
		Type:     "object",
		Required: []string{"id", "name"},
		Properties: map[string]SchemaSpec{
			"id":   {Type: "integer", Format: "int64"},
			"name": {Type: "string"},
			"tag":  {Type: "string"},
		},
	}
}

// petRequestSpec mirrors the mycloud-pets NewPet request schema: name (required),
// tag (optional); no id.
func petRequestSpec() *SchemaSpec {
	return &SchemaSpec{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
			"tag":  {Type: "string"},
		},
	}
}

func findAttr(attrs []ir.AttributeIR, name string) (ir.AttributeIR, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return ir.AttributeIR{}, false
}

func TestManagedResourceSchema_MyCloudPetsReconciliation(t *testing.T) {
	c := ResourceCRUD{
		Name:           "pet",
		CollectionPath: "/pets",
		InstancePath:   "/pets/{petId}",
		Create: &Operation{
			Method:         MethodPost,
			Path:           "/pets",
			RequestSchema:  petRequestSpec(),
			ResponseSchema: nil, // 201 with no body
		},
		Read: &Operation{
			Method:         MethodGet,
			Path:           "/pets/{petId}",
			ResponseSchema: petStateSpec(),
		},
		Delete: &Operation{Method: MethodDelete, Path: "/pets/{petId}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"petId"}, AttributeName: "pet_id", ImportFormat: "%s"},
	}

	schema, idAttr := ManagedResourceSchema(c)

	// The state shape has an "id" property, so the ID attribute defaults to "id"
	// rather than the path-derived "pet_id" (which would not match the response
	// field and would disable wiring).
	if idAttr != "" {
		t.Errorf("idAttribute = %q, want \"\" (default to id)", idAttr)
	}

	id, ok := findAttr(schema.Attributes, "id")
	if !ok {
		t.Fatalf("no id attribute in schema: %+v", schema.Attributes)
	}
	if id.Schema.Type != ir.TypeInt {
		t.Errorf("id schema type = %q, want %q (integer from Pet response)", id.Schema.Type, ir.TypeInt)
	}
	if !id.Computed || id.Required || id.Optional {
		t.Errorf("id must be Computed only (server-assigned): got Required=%v Optional=%v Computed=%v", id.Required, id.Optional, id.Computed)
	}

	name, ok := findAttr(schema.Attributes, "name")
	if !ok {
		t.Fatalf("no name attribute in schema")
	}
	if !name.Required || name.Computed || name.Optional {
		t.Errorf("name must be Required (in NewPet required): got Required=%v Optional=%v Computed=%v", name.Required, name.Optional, name.Computed)
	}

	tag, ok := findAttr(schema.Attributes, "tag")
	if !ok {
		t.Fatalf("no tag attribute in schema")
	}
	if !tag.Optional || tag.Required || !tag.Computed {
		t.Errorf("tag must be Optional+Computed (an optional request input the response also returns): got Required=%v Optional=%v Computed=%v", tag.Required, tag.Optional, tag.Computed)
	}
}

func TestManagedResourceSchema_SyntheticIDWhenResponseHasNoID(t *testing.T) {
	// A response with no "id" property but a path param of {id}: a synthetic
	// Computed string "id" attribute is added so path substitution resolves
	// against an identifier populated via the create request or a Location
	// header (REMAINING_GAPS §2).
	c := ResourceCRUD{
		Name:           "widget",
		CollectionPath: "/widgets",
		InstancePath:   "/widgets/{id}",
		Create:         &Operation{Method: MethodPost, Path: "/widgets"},
		Read: &Operation{
			Method: MethodGet,
			Path:   "/widgets/{id}",
			ResponseSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"label"},
				Properties: map[string]SchemaSpec{
					"label": {Type: "string"},
				},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/widgets/{id}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"id"}, AttributeName: "id", ImportFormat: "%s"},
	}

	schema, idAttr := ManagedResourceSchema(c)
	if idAttr != "id" {
		t.Errorf("idAttribute = %q, want \"id\"", idAttr)
	}
	id, ok := findAttr(schema.Attributes, "id")
	if !ok {
		t.Fatalf("no synthetic id attribute: %+v", schema.Attributes)
	}
	if id.Schema.Type != ir.TypeString {
		t.Errorf("synthetic id type = %q, want string", id.Schema.Type)
	}
	if !id.Computed {
		t.Errorf("synthetic id must be Computed")
	}
}

// TestManagedResourceSchema_PractitionerSetID locks in the §3/#11 fix: an "id"
// property that the create request body declares as required is practitioner-set
// on create, so it must be Required (not forced Computed). Only an id absent from
// the create request is treated as server-assigned (Computed).
func TestManagedResourceSchema_PractitionerSetID(t *testing.T) {
	c := ResourceCRUD{
		Name:           "user",
		CollectionPath: "/users",
		InstancePath:   "/users/{id}",
		Create: &Operation{
			Method:        MethodPost,
			Path:          "/users",
			RequestSchema: &SchemaSpec{Type: "object", Required: []string{"id", "name"}, Properties: map[string]SchemaSpec{"id": {Type: "string"}, "name": {Type: "string"}}},
		},
		Read: &Operation{
			Method: MethodGet,
			Path:   "/users/{id}",
			ResponseSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"id", "name"},
				Properties: map[string]SchemaSpec{
					"id":   {Type: "string"},
					"name": {Type: "string"},
				},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/users/{id}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"id"}, AttributeName: "id", ImportFormat: "%s"},
	}

	schema, _ := ManagedResourceSchema(c)
	id, ok := findAttr(schema.Attributes, "id")
	if !ok {
		t.Fatalf("no id attribute: %+v", schema.Attributes)
	}
	if !id.Required || id.Computed {
		t.Errorf("practitioner-set id must be Required (not Computed): got Required=%v Computed=%v", id.Required, id.Computed)
	}
}

// TestManagedResourceSchema_PutAsCreateForcesIdentifierRequired locks in the
// PUT-as-create schema fix: when Create is a PUT (upsert), the identifier
// attribute the practitioner must supply in the request URI is forced Required
// (Computed=false, Optional=false) so the wired Create body substitutes a real
// value into the path placeholder instead of a null Computed id. Two shapes are
// covered: the state shape carries an "id" property (hasID), and it does not
// (the path-parameter-named synthetic identifier). POST-create resources are
// byte-identical (the gate is Create.Method == PUT).
func TestManagedResourceSchema_PutAsCreateForcesIdentifierRequired(t *testing.T) {
	t.Run("state shape has id property", func(t *testing.T) {
		c := ResourceCRUD{
			Name:           "alarm",
			CollectionPath: "/alarms",
			InstancePath:   "/alarms/{alarmId}",
			Create: &Operation{
				Method:        MethodPut,
				Path:          "/alarms/{alarmId}",
				RequestSchema: &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"name": {Type: "string"}}},
			},
			Read: &Operation{
				Method: MethodGet,
				Path:   "/alarms/{alarmId}",
				ResponseSchema: &SchemaSpec{
					Type:     "object",
					Required: []string{"id", "name"},
					Properties: map[string]SchemaSpec{
						"id":   {Type: "string"},
						"name": {Type: "string"},
					},
				},
			},
			Delete: &Operation{Method: MethodDelete, Path: "/alarms/{alarmId}"},
			ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"alarmId"}, AttributeName: "alarm_id", ImportFormat: "%s"},
		}
		schema, _ := ManagedResourceSchema(c)
		id, ok := findAttr(schema.Attributes, "id")
		if !ok {
			t.Fatalf("no id attribute: %+v", schema.Attributes)
		}
		if !id.Required || id.Computed || id.Optional {
			t.Errorf("PUT-as-create id must be Required only: got Required=%v Computed=%v Optional=%v", id.Required, id.Computed, id.Optional)
		}
	})

	t.Run("state shape has no id, synthetic path-param identifier", func(t *testing.T) {
		c := ResourceCRUD{
			Name:           "alarm",
			CollectionPath: "/alarms",
			InstancePath:   "/alarms/{alarmId}",
			Create: &Operation{
				Method:        MethodPut,
				Path:          "/alarms/{alarmId}",
				RequestSchema: &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"name": {Type: "string"}}},
			},
			Read: &Operation{
				Method: MethodGet,
				Path:   "/alarms/{alarmId}",
				ResponseSchema: &SchemaSpec{
					Type:     "object",
					Required: []string{"name"},
					Properties: map[string]SchemaSpec{
						"name": {Type: "string"},
					},
				},
			},
			Delete: &Operation{Method: MethodDelete, Path: "/alarms/{alarmId}"},
			ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"alarmId"}, AttributeName: "alarm_id", ImportFormat: "%s"},
		}
		schema, idAttr := ManagedResourceSchema(c)
		if idAttr != "alarm_id" {
			t.Errorf("idAttribute = %q, want \"alarm_id\"", idAttr)
		}
		id, ok := findAttr(schema.Attributes, "alarm_id")
		if !ok {
			t.Fatalf("no alarm_id attribute: %+v", schema.Attributes)
		}
		if !id.Required || id.Computed || id.Optional {
			t.Errorf("PUT-as-create synthetic id must be Required only: got Required=%v Computed=%v Optional=%v", id.Required, id.Computed, id.Optional)
		}
	})
}

// TestManagedResourceSchema_BodylessResourceNoSyntheticID locks in the §3/#12
// fix: a resource with no response or request body anywhere returns an empty
// schema with no identifier, so it stays honestly scaffolded rather than wiring
// with an unpopulated synthetic id (the mycloud workspace case).
func TestManagedResourceSchema_BodylessResourceNoSyntheticID(t *testing.T) {
	c := ResourceCRUD{
		Name:           "namespace",
		CollectionPath: "/api/v1/namespaces",
		InstancePath:   "/api/v1/namespaces/{name}",
		Create:         &Operation{Method: MethodPost, Path: "/api/v1/namespaces"},
		Read:           &Operation{Method: MethodGet, Path: "/api/v1/namespaces/{name}"},
		Delete:         &Operation{Method: MethodDelete, Path: "/api/v1/namespaces/{name}"},
		ID:             IDInfo{Kind: IDSimple, ParameterNames: []string{"name"}, AttributeName: "name", ImportFormat: "%s"},
	}
	schema, idAttr := ManagedResourceSchema(c)
	if idAttr != "" {
		t.Errorf("idAttribute = %q, want \"\" for a bodyless resource", idAttr)
	}
	if len(schema.Attributes) != 0 {
		t.Errorf("bodyless resource must have an empty schema (no synthetic id), got %+v", schema.Attributes)
	}
}

// TestManagedResourceSchema_NoDuplicateSyntheticID locks in the dup fix: when
// the path-parameter name is itself a top-level response property (e.g.
// /users/{username} with a response exposing username), the real attribute is
// kept and no duplicate synthetic attribute is added.
func TestManagedResourceSchema_NoDuplicateSyntheticID(t *testing.T) {
	c := ResourceCRUD{
		Name:           "user",
		CollectionPath: "/users",
		InstancePath:   "/users/{username}",
		Create: &Operation{
			Method:        MethodPost,
			Path:          "/users",
			RequestSchema: &SchemaSpec{Type: "object", Required: []string{"username"}, Properties: map[string]SchemaSpec{"username": {Type: "string"}}},
		},
		Read: &Operation{
			Method: MethodGet,
			Path:   "/users/{username}",
			ResponseSchema: &SchemaSpec{
				Type:       "object",
				Required:   []string{"username"},
				Properties: map[string]SchemaSpec{"username": {Type: "string"}, "email": {Type: "string"}},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/users/{username}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"username"}, AttributeName: "username", ImportFormat: "%s"},
	}
	schema, idAttr := ManagedResourceSchema(c)
	if idAttr != "username" {
		t.Errorf("idAttribute = %q, want \"username\"", idAttr)
	}
	count := 0
	for _, a := range schema.Attributes {
		if a.Name == "username" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one username attribute, got %d: %+v", count, schema.Attributes)
	}
}

func TestSchemaIRFromSpecRecursive_ArrayOfObjects(t *testing.T) {
	spec := SchemaSpec{
		Type: "array",
		Items: &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"name": {Type: "string"},
			},
		},
	}
	got := schemaIRFromSpecRecursive(spec)
	if got.Collection == nil || got.Collection.Kind != ir.List {
		t.Fatalf("expected List collection, got %+v", got)
	}
	if len(got.Collection.ElementType.Attributes) != 1 {
		t.Fatalf("expected 1 nested attr, got %+v", got.Collection.ElementType.Attributes)
	}
	if got.Collection.ElementType.Attributes[0].Name != "name" {
		t.Errorf("nested attr name = %q, want name", got.Collection.ElementType.Attributes[0].Name)
	}
}

func TestSchemaIRFromSpecRecursive_UniqueItemsIsSet(t *testing.T) {
	spec := SchemaSpec{Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "string"}}
	got := schemaIRFromSpecRecursive(spec)
	if got.Collection == nil || got.Collection.Kind != ir.Set {
		t.Fatalf("expected Set collection for uniqueItems, got %+v", got)
	}
}

// TestDataSourceSchema_UniqueItemsIsSet covers the data-source array-response
// branch (resource_schema.go:341): an array response with uniqueItems: true
// yields a Computed `items` Set attribute rather than a List (A1).
func TestDataSourceSchema_UniqueItemsIsSet(t *testing.T) {
	op := Operation{
		Method:         MethodGet,
		Path:           "/pets",
		ResponseSchema: &SchemaSpec{Type: "array", UniqueItems: true, Items: &SchemaSpec{Type: "string"}},
	}
	schema := DataSourceSchema(op, nil)

	var items *ir.AttributeIR
	for i := range schema.Attributes {
		if schema.Attributes[i].Name == "items" {
			items = &schema.Attributes[i]
		}
	}
	if items == nil {
		t.Fatalf("expected an items attribute, got %+v", schema.Attributes)
	}
	if items.Schema.Collection == nil || items.Schema.Collection.Kind != ir.Set {
		t.Fatalf("expected items Set collection, got %+v", items.Schema)
	}
	if !items.Computed {
		t.Errorf("expected items attribute to be Computed")
	}
}

func TestSchemaIRFromSpecRecursive_AdditionalPropertiesMap(t *testing.T) {
	spec := SchemaSpec{AdditionalProperties: &SchemaSpec{Type: "string"}}
	got := schemaIRFromSpecRecursive(spec)
	if got.Collection == nil || got.Collection.Kind != ir.Map {
		t.Fatalf("expected Map collection, got %+v", got)
	}
	if got.Collection.ElementType.Type != ir.TypeString {
		t.Errorf("map element type = %q, want string", got.Collection.ElementType.Type)
	}
}

// TestManagedResourceSchema_RequestOnlyInputs verifies G9: create request-body
// inputs the read response does not echo are added as Optional attributes, so a
// resource whose response wraps its payload (e.g. library elements
// {result: {...}}) still exposes its writable inputs.
func TestManagedResourceSchema_RequestOnlyInputs(t *testing.T) {
	c := ResourceCRUD{
		Name: "library_element",
		Create: &Operation{
			Method: MethodPost, Path: "/library-elements", OperationID: "createLibraryElement",
			RequestSchema: &SchemaSpec{Properties: map[string]SchemaSpec{
				"name":  {Type: "string"},
				"kind":  {Type: "integer"},
				"model": {Type: "object"},
			}},
		},
		Read: &Operation{
			Method: MethodGet, Path: "/library-elements/{uid}", OperationID: "getLibraryElement",
			ResponseSchema: &SchemaSpec{Properties: map[string]SchemaSpec{
				"result": {Type: "object", Properties: map[string]SchemaSpec{"id": {Type: "integer"}}},
			}},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/library-elements/{uid}", OperationID: "deleteLibraryElement"},
	}

	schema, _ := ManagedResourceSchema(c)
	names := map[string]ir.AttributeIR{}
	for _, a := range schema.Attributes {
		names[a.Name] = a
	}
	for _, want := range []string{"name", "kind", "model"} {
		a, ok := names[want]
		if !ok {
			t.Errorf("expected request-only input %q in schema, got %+v", want, schema.Attributes)
			continue
		}
		if !a.Optional || a.Computed || a.Required {
			t.Errorf("request-only input %q must be Optional (not Computed/Required), got %+v", want, a)
		}
	}
}

// TestManagedResourceSchema_NestedArrayOfObject_ReconcilesRequired mirrors the
// Gigamon gigamon_role.scope shape: a top-level Required array whose element is
// an object (RoleScope) with required nested fields (type, actions) and an
// optional/response-only field (hierarchy). The nested children must be
// reconciled against the request body so type/actions are Required and
// hierarchy stays Computed, rather than all children being unconditionally
// Computed (the nestedAttributesFromSpec default).
func TestManagedResourceSchema_NestedArrayOfObject_ReconcilesRequired(t *testing.T) {
	roleScope := SchemaSpec{
		Type:     "object",
		Required: []string{"type", "actions"},
		Properties: map[string]SchemaSpec{
			"type":      {Type: "string"},
			"actions":   {Type: "array", Items: &SchemaSpec{Type: "string"}},
			"hierarchy": {Type: "boolean"},
		},
	}
	requestSpec := &SchemaSpec{
		Type:     "object",
		Required: []string{"name", "scope"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
			"scope": {
				Type:  "array",
				Items: &roleScope,
			},
		},
	}
	stateSpec := &SchemaSpec{
		Type:     "object",
		Required: []string{"name", "scope"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
			"scope": {
				Type: "array",
				Items: &SchemaSpec{
					Type:     "object",
					Required: []string{"type", "actions"},
					Properties: map[string]SchemaSpec{
						"type":      {Type: "string"},
						"actions":   {Type: "array", Items: &SchemaSpec{Type: "string"}},
						"hierarchy": {Type: "boolean"},
					},
				},
			},
		},
	}
	c := ResourceCRUD{
		Name: "role",
		Create: &Operation{
			Method:         MethodPost,
			Path:           "/roles",
			RequestSchema:  requestSpec,
			ResponseSchema: stateSpec,
		},
		Read: &Operation{
			Method:         MethodGet,
			Path:           "/roles/{name}",
			ResponseSchema: stateSpec,
		},
		Delete: &Operation{Method: MethodDelete, Path: "/roles/{name}"},
	}

	schema, _ := ManagedResourceSchema(c)

	scope, ok := findAttr(schema.Attributes, "scope")
	if !ok {
		t.Fatalf("no scope attribute: %+v", schema.Attributes)
	}
	if !scope.Required {
		t.Errorf("scope must be Required (request-required), got %+v", scope)
	}
	if scope.Schema.Collection == nil {
		t.Fatalf("scope must be a collection, got %+v", scope.Schema)
	}
	elem := scope.Schema.Collection.ElementType
	if len(elem.Attributes) == 0 {
		t.Fatalf("scope element must have nested attributes, got %+v", elem)
	}
	for _, a := range elem.Attributes {
		switch a.WireName {
		case "type", "actions":
			if !a.Required || a.Computed || a.Optional {
				t.Errorf("nested %q must be Required (request-required), got R=%v O=%v C=%v", a.WireName, a.Required, a.Optional, a.Computed)
			}
		case "hierarchy":
			// hierarchy is in the request's RoleScope properties (with a default)
			// but not required, so it reconciles to Optional+Computed (G18): the
			// practitioner may set it and the response also returns it.
			if !a.Optional || !a.Computed || a.Required {
				t.Errorf("nested hierarchy must be Optional+Computed (in request, not required), got R=%v O=%v C=%v", a.Required, a.Optional, a.Computed)
			}
		}
	}
}

// TestManagedResourceSchema_NestedArrayOfObject_RequestOnly_Reconciles mirrors
// the Gigamon gigamon_role envelope: the Read response is a {role: {...}} wrapper
// whose unwrapped state shape does NOT carry scope, so scope is a request-only
// top-level Required attribute. Its nested children must still be reconciled
// against the request body — type/actions Required, hierarchy Optional only
// (NOT Computed: the response never returns scope, so the server cannot
// repopulate hierarchy after apply; marking it Computed would leave the
// framework expecting a value the provider cannot supply).
func TestManagedResourceSchema_NestedArrayOfObject_RequestOnly_Reconciles(t *testing.T) {
	roleScope := SchemaSpec{
		Type:     "object",
		Required: []string{"type", "actions"},
		Properties: map[string]SchemaSpec{
			"type":      {Type: "string"},
			"actions":   {Type: "array", Items: &SchemaSpec{Type: "string"}},
			"hierarchy": {Type: "boolean"},
		},
	}
	requestSpec := &SchemaSpec{
		Type:     "object",
		Required: []string{"name", "scope"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
			"scope": {
				Type:  "array",
				Items: &roleScope,
			},
		},
	}
	// The state shape is the {role: {...}} envelope unwrapped only to the role
	// object, which does NOT include scope as a peer of name. scope is therefore a
	// request-only input.
	stateSpec := &SchemaSpec{
		Type:     "object",
		Required: []string{"role"},
		Properties: map[string]SchemaSpec{
			"role": {
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]SchemaSpec{
					"name": {Type: "string"},
				},
			},
		},
	}
	c := ResourceCRUD{
		Name: "role",
		Create: &Operation{
			Method:         MethodPost,
			Path:           "/roles",
			RequestSchema:  requestSpec,
			ResponseSchema: requestSpec, // create returns the request body
		},
		Read: &Operation{
			Method:         MethodGet,
			Path:           "/roles/{name}",
			ResponseSchema: stateSpec,
		},
		Delete: &Operation{Method: MethodDelete, Path: "/roles/{name}"},
	}

	schema, _ := ManagedResourceSchema(c)

	scope, ok := findAttr(schema.Attributes, "scope")
	if !ok {
		t.Fatalf("no scope attribute (request-only input): %+v", schema.Attributes)
	}
	if !scope.Required {
		t.Errorf("scope must be Required (request-required), got %+v", scope)
	}
	// scope is request-only: the response does not echo it, so it must not be
	// Computed (the server never repopulates it).
	if scope.Computed {
		t.Errorf("request-only scope must not be Computed, got %+v", scope)
	}
	if scope.Schema.Collection == nil {
		t.Fatalf("scope must be a collection, got %+v", scope.Schema)
	}
	elem := scope.Schema.Collection.ElementType
	if len(elem.Attributes) == 0 {
		t.Fatalf("scope element must have nested attributes, got %+v", elem)
	}
	for _, a := range elem.Attributes {
		switch a.WireName {
		case "type", "actions":
			if !a.Required || a.Computed || a.Optional {
				t.Errorf("nested %q must be Required (request-required), got R=%v O=%v C=%v", a.WireName, a.Required, a.Optional, a.Computed)
			}
		case "hierarchy":
			// hierarchy is in the request (not required) but the response does not
			// echo scope, so it is Optional only — NOT Computed.
			if !a.Optional || a.Computed || a.Required {
				t.Errorf("nested hierarchy must be Optional only (request-only parent), got R=%v O=%v C=%v", a.Required, a.Optional, a.Computed)
			}
		}
	}
}
