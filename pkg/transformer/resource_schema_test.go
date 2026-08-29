package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
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

// TestManagedResourceSchema_SingleArrayResponseUnwraps verifies a "get one"
// read that returns a single-array response wrapper (e.g. Gigamon
// {"Policies": [{...}]}) derives the resource schema from the array item's
// properties, not the collection itself. UnwrapResponseEnvelope flattens the
// wrapper to an array schema (E1); the resource represents a single instance,
// so its shape is the item. Unwrapping the array lets the resource wire with
// the item's fields instead of scaffolding with an empty schema (issue #35).
func TestManagedResourceSchema_SingleArrayResponseUnwraps(t *testing.T) {
	policyItem := func() *SchemaSpec {
		return &SchemaSpec{
			Type: "object",
			Properties: map[string]SchemaSpec{
				"policyId":    {Type: "string"},
				"policyName":  {Type: "string"},
				"description": {Type: "string"},
			},
		}
	}
	c := ResourceCRUD{
		Name:           "policy",
		CollectionPath: "/intent/policies",
		InstancePath:   "/intent/policies/{policyId}",
		Create: &Operation{
			Method: MethodPost,
			Path:   "/intent/policies",
			RequestSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"policyName"},
				Properties: map[string]SchemaSpec{
					"policyName":  {Type: "string"},
					"description": {Type: "string"},
				},
			},
		},
		Read: &Operation{
			Method:         MethodGet,
			Path:           "/intent/policies/{policyId}",
			ResponseSchema: &SchemaSpec{Type: "array", Items: policyItem()},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/intent/policies/{policyId}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"policyId"}, AttributeName: "policy_id", ImportFormat: "%s"},
	}

	schema, _ := ManagedResourceSchema(c)

	// The item's fields must be exposed as attributes, not an empty schema.
	policyName, ok := findAttr(schema.Attributes, "policy_name")
	if !ok {
		t.Fatalf("no policy_name attribute in schema (array item not unwrapped): %+v", schema.Attributes)
	}
	if !policyName.Required {
		t.Errorf("policy_name must be Required (declared in the create request's required list): got Required=%v", policyName.Required)
	}
	if _, ok := findAttr(schema.Attributes, "policy_id"); !ok {
		t.Errorf("no policy_id attribute in schema (should derive from the array item): %+v", schema.Attributes)
	}
	if len(schema.Attributes) == 0 {
		t.Errorf("schema must expose the array item's attributes; got empty schema")
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

	t.Run("singleton path has no placeholder, id stays Computed", func(t *testing.T) {
		// A singleton PUT-as-create (e.g. PUT /fm/copilot/config) substitutes
		// nothing from the plan, so forcing the identifier Required would demand
		// a value no request ever sends. The synthetic id keeps its Computed
		// placeholder semantics (the import can still target it).
		c := ResourceCRUD{
			Name:           "copilot_config",
			CollectionPath: "/fm/copilot/config",
			InstancePath:   "/fm/copilot/config",
			Create: &Operation{
				Method: MethodPut,
				Path:   "/fm/copilot/config",
				RequestSchema: &SchemaSpec{
					Type:     "object",
					Required: []string{"enabled"},
					Properties: map[string]SchemaSpec{
						"enabled":   {Type: "boolean"},
						"serverUrl": {Type: "string"},
					},
				},
			},
			Read: &Operation{
				Method: MethodGet,
				Path:   "/fm/copilot/config",
				ResponseSchema: &SchemaSpec{
					Type:     "object",
					Required: []string{"enabled"},
					Properties: map[string]SchemaSpec{
						"enabled":   {Type: "boolean"},
						"serverUrl": {Type: "string"},
					},
				},
			},
			Delete: &Operation{Method: MethodDelete, Path: "/fm/copilot/config"},
		}
		schema, _ := ManagedResourceSchema(c)
		id, ok := findAttr(schema.Attributes, "id")
		if !ok {
			t.Fatalf("no synthetic id attribute: %+v", schema.Attributes)
		}
		if id.Required || !id.Computed {
			t.Errorf("singleton PUT-as-create id must stay Computed (not Required): got Required=%v Computed=%v", id.Required, id.Computed)
		}
		if id.Optional {
			t.Errorf("singleton PUT-as-create id must not be Optional: got Optional=%v", id.Optional)
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

// TestDataSourceSchema_DeprecatedParameter verifies M-10: a parameter whose
// spec marks it deprecated surfaces as a deprecated input attribute so the flag
// reaches the generated schema.
func TestDataSourceSchema_DeprecatedParameter(t *testing.T) {
	op := Operation{
		Method: MethodGet,
		Path:   "/pets",
		Parameters: []Parameter{
			{Name: "limit", In: "query", Type: "integer", Deprecated: true},
			{Name: "status", In: "query", Type: "string"},
		},
		ResponseSchema: &SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string"}},
	}
	schema := DataSourceSchema(op, nil)

	var limit, status *ir.AttributeIR
	for i := range schema.Attributes {
		switch schema.Attributes[i].Name {
		case "limit":
			limit = &schema.Attributes[i]
		case "status":
			status = &schema.Attributes[i]
		}
	}
	if limit == nil {
		t.Fatalf("expected a limit attribute, got %+v", schema.Attributes)
	}
	if !limit.Deprecated {
		t.Errorf("expected limit attribute to be Deprecated")
	}
	if limit.DeprecationMessage == "" {
		t.Errorf("expected limit attribute to carry a DeprecationMessage")
	}
	if status == nil {
		t.Fatalf("expected a status attribute, got %+v", schema.Attributes)
	}
	if status.Deprecated {
		t.Errorf("expected status attribute to not be Deprecated")
	}
}

// TestListResourceConfigSchema_DeprecatedParameter verifies M-10: a deprecated
// parameter surfaces as a deprecated filter attribute on a list resource.
func TestListResourceConfigSchema_DeprecatedParameter(t *testing.T) {
	op := Operation{
		Method: MethodGet,
		Path:   "/pets",
		Parameters: []Parameter{
			{Name: "limit", In: "query", Type: "integer", Deprecated: true},
		},
		ResponseSchema: &SchemaSpec{Type: "array", Items: &SchemaSpec{Type: "string"}},
	}
	schema := ListResourceConfigSchema(op, nil)
	if len(schema.Attributes) != 1 {
		t.Fatalf("expected 1 attribute, got %+v", schema.Attributes)
	}
	attr := schema.Attributes[0]
	if attr.Name != "limit" {
		t.Fatalf("attribute name = %q, want limit", attr.Name)
	}
	if !attr.Deprecated {
		t.Errorf("expected limit attribute to be Deprecated")
	}
	if attr.DeprecationMessage == "" {
		t.Errorf("expected limit attribute to carry a DeprecationMessage")
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

// TestDescriptionsReachAttributes pins the plumbing that carries an OpenAPI
// `description` onto ir.AttributeIR, which the generators render as the
// framework MarkdownDescription and as the docs attribute blurb. Every
// reference spec predates this and the corpus is thin on property
// descriptions, so the golden files alone do not guard it.
func TestDescriptionsReachAttributes(t *testing.T) {
	stateSpec := &SchemaSpec{
		Type:     "object",
		Required: []string{"id", "name"},
		Properties: map[string]SchemaSpec{
			"id":   {Type: "integer", Format: "int64", Description: "Server-assigned identifier."},
			"name": {Type: "string", Description: "Display name of the pet."},
			"settings": {
				Type: "object",
				Properties: map[string]SchemaSpec{
					"nickname": {Type: "string", Description: "Nested property description."},
				},
			},
		},
	}

	t.Run("managed resource properties", func(t *testing.T) {
		obj, _ := ManagedResourceSchema(ResourceCRUD{
			Name:           "pet",
			CollectionPath: "/pets",
			InstancePath:   "/pets/{petId}",
			Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}", ResponseSchema: stateSpec},
			Delete:         &Operation{Method: MethodDelete, Path: "/pets/{petId}"},
		})
		for name, want := range map[string]string{
			"id":   "Server-assigned identifier.",
			"name": "Display name of the pet.",
		} {
			attr, ok := findAttr(obj.Attributes, name)
			if !ok {
				t.Fatalf("attribute %q not found", name)
			}
			if attr.Description != want {
				t.Errorf("attribute %q description = %q, want %q", name, attr.Description, want)
			}
		}
	})

	t.Run("nested properties", func(t *testing.T) {
		obj, _ := ManagedResourceSchema(ResourceCRUD{
			Name:           "pet",
			CollectionPath: "/pets",
			InstancePath:   "/pets/{petId}",
			Read:           &Operation{Method: MethodGet, Path: "/pets/{petId}", ResponseSchema: stateSpec},
			Delete:         &Operation{Method: MethodDelete, Path: "/pets/{petId}"},
		})
		parent, ok := findAttr(obj.Attributes, "settings")
		if !ok {
			t.Fatal("settings attribute not found")
		}
		nested, ok := findAttr(parent.Schema.Attributes, "nickname")
		if !ok {
			t.Fatal("nested nickname attribute not found")
		}
		if nested.Description != "Nested property description." {
			t.Errorf("nested description = %q, want %q", nested.Description, "Nested property description.")
		}
	})

	t.Run("parameters", func(t *testing.T) {
		obj := DataSourceSchema(Operation{
			Method: MethodGet,
			Path:   "/pets",
			Parameters: []Parameter{
				{Name: "limit", In: "query", Type: "integer", Description: "How many items to return at one time."},
			},
			ResponseSchema: stateSpec,
		}, nil)
		attr, ok := findAttr(obj.Attributes, "limit")
		if !ok {
			t.Fatal("limit attribute not found")
		}
		if attr.Description != "How many items to return at one time." {
			t.Errorf("parameter description = %q, want the OpenAPI text", attr.Description)
		}
	})

	t.Run("dropped colliding parameter does not relabel the kept one", func(t *testing.T) {
		// A path param and an optional query param whose names both sanitize to
		// "id" (L-102): the optional one is dropped with a warning, so its prose
		// must not end up describing the surviving required attribute.
		obj := DataSourceSchema(Operation{
			Method: MethodGet,
			Path:   "/projects/{id}/state",
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Type: "string", Description: "The project identifier."},
				{Name: "ID", In: "query", Type: "string", Description: "Deprecated uppercase alias."},
			},
		}, nil)
		attr, ok := findAttr(obj.Attributes, "id")
		if !ok {
			t.Fatal("id attribute not found")
		}
		if attr.Description != "The project identifier." {
			t.Errorf("description = %q, want the kept path parameter's text", attr.Description)
		}
	})
}

// TestManagedResourceDescriptionFallsBackToRequestBody covers the common spec
// split where the create request body documents a field and the response
// schema echoes it bare. resourceStateSpec prefers the response, so without a
// fallback the only prose the spec supplied is dropped.
func TestManagedResourceDescriptionFallsBackToRequestBody(t *testing.T) {
	obj, _ := ManagedResourceSchema(ResourceCRUD{
		Name:           "pet",
		CollectionPath: "/pets",
		InstancePath:   "/pets/{petId}",
		Create: &Operation{
			Method: MethodPost,
			Path:   "/pets",
			RequestSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]SchemaSpec{
					"name": {Type: "string", Description: "Display name of the pet."},
				},
			},
		},
		Read: &Operation{
			Method: MethodGet,
			Path:   "/pets/{petId}",
			ResponseSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"id", "name"},
				Properties: map[string]SchemaSpec{
					"id":   {Type: "integer", Format: "int64"},
					"name": {Type: "string"}, // bare echo, no description
				},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/pets/{petId}"},
	})
	attr, ok := findAttr(obj.Attributes, "name")
	if !ok {
		t.Fatal("name attribute not found")
	}
	if attr.Description != "Display name of the pet." {
		t.Errorf("description = %q, want the request body's text", attr.Description)
	}
}

// TestManagedResourceSchemaDedupsSnakeCaseCollisions verifies that two distinct
// response properties that sanitize to the same Terraform attribute name
// ("fooBar" and "foo_bar") do not both survive as duplicate attributes: the
// lexicographically smaller original name wins and a Warning is emitted for the
// dropped property (H-3).
func TestManagedResourceSchemaDedupsSnakeCaseCollisions(t *testing.T) {
	var diags diagnostics.Diagnostics
	obj, _ := ManagedResourceSchemaWithDiagnostics(ResourceCRUD{
		Name:           "collision",
		CollectionPath: "/collisions",
		InstancePath:   "/collisions/{id}",
		Read: &Operation{
			Method: MethodGet,
			Path:   "/collisions/{id}",
			ResponseSchema: &SchemaSpec{
				Type: "object",
				Properties: map[string]SchemaSpec{
					"fooBar":  {Type: "string"},
					"foo_bar": {Type: "string"},
					"id":      {Type: "string"},
				},
			},
		},
		ID: IDInfo{Kind: IDSimple, ParameterNames: []string{"id"}, AttributeName: "id", ImportFormat: "%s"},
	}, &diags, false)

	// Exactly one foo_bar attribute survives.
	count := 0
	for _, a := range obj.Attributes {
		if a.Name == "foo_bar" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("foo_bar attribute count = %d, want 1 (duplicate dropped): %+v", count, obj.Attributes)
	}
	// The winner is the lexicographically smaller original name ("fooBar").
	if a, ok := findAttr(obj.Attributes, "foo_bar"); !ok || a.WireName != "fooBar" {
		t.Errorf("surviving foo_bar WireName = %q, want %q", a.WireName, "fooBar")
	}
	// A Warning names the dropped property.
	if !hasWarning(diags, "duplicate attribute after name normalization") {
		t.Errorf("expected a duplicate-attribute Warning, got %v", diags)
	}
}

// TestObjectSchemaFromSpecDedupsSnakeCaseCollisions verifies the list-resource
// identity/resource schema builder dedups snake_case collisions with a Warning
// (H-3).
func TestObjectSchemaFromSpecDedupsSnakeCaseCollisions(t *testing.T) {
	var diags diagnostics.Diagnostics
	obj := ObjectSchemaFromSpecWithDiagnostics(&SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"fooBar":  {Type: "string"},
			"foo_bar": {Type: "string"},
		},
	}, &diags)
	count := 0
	for _, a := range obj.Attributes {
		if a.Name == "foo_bar" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("foo_bar attribute count = %d, want 1: %+v", count, obj.Attributes)
	}
	if !hasWarning(diags, "duplicate attribute after name normalization") {
		t.Errorf("expected a duplicate-attribute Warning, got %v", diags)
	}
}

// TestResultSchemaFromResponseDedupsSnakeCaseCollisions verifies the ephemeral
// result-schema builder dedups snake_case collisions with a Warning (H-3).
func TestResultSchemaFromResponseDedupsSnakeCaseCollisions(t *testing.T) {
	var diags diagnostics.Diagnostics
	obj := ResultSchemaFromResponseWithDiagnostics(&SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"fooBar":  {Type: "string"},
			"foo_bar": {Type: "string"},
		},
	}, &diags)
	count := 0
	for _, a := range obj.Attributes {
		if a.Name == "foo_bar" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("foo_bar attribute count = %d, want 1: %+v", count, obj.Attributes)
	}
	if !hasWarning(diags, "duplicate attribute after name normalization") {
		t.Errorf("expected a duplicate-attribute Warning, got %v", diags)
	}
}

// TestManagedResourceSchema_RequiredQueryParamForcesRequired locks in that a
// required query parameter that shares a name with a response/body property
// must be Required on the managed resource, not Optional+Computed. Specs
// commonly declare a parent-scope id (clusterId, tenantId, accountId) as a
// required query param on create while the body property is readOnly and not
// in the body's required list.
func TestManagedResourceSchema_RequiredQueryParamForcesRequired(t *testing.T) {
	c := mapCRUDWithClusterQuery(true)
	schema, _ := ManagedResourceSchema(c)

	clusterID, ok := findAttr(schema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("no cluster_id attribute in schema: %+v", schema.Attributes)
	}
	if !clusterID.Required || clusterID.Optional || clusterID.Computed {
		t.Errorf("cluster_id must be Required (required query param), got Required=%v Optional=%v Computed=%v",
			clusterID.Required, clusterID.Optional, clusterID.Computed)
	}
	name, ok := findAttr(schema.Attributes, "name")
	if !ok {
		t.Fatal("no name attribute in schema")
	}
	if !name.Required || name.Computed || name.Optional {
		t.Errorf("name must stay Required (body required), got Required=%v Optional=%v Computed=%v",
			name.Required, name.Optional, name.Computed)
	}
}

// TestManagedResourceSchema_RequiredQueryParamAppended locks in that a required
// query parameter with no matching body/response property is added as a
// Required attribute so the generated request can send it.
func TestManagedResourceSchema_RequiredQueryParamAppended(t *testing.T) {
	c := mapCRUDWithClusterQuery(true)
	// Drop clusterId from the body and response so it exists only as a query param.
	c.Create.RequestSchema = &SchemaSpec{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]SchemaSpec{
			"name": {Type: "string"},
		},
	}
	c.Read.ResponseSchema = &SchemaSpec{
		Type: "object",
		Properties: map[string]SchemaSpec{
			"id":   {Type: "string"},
			"name": {Type: "string"},
		},
	}

	schema, _ := ManagedResourceSchema(c)
	clusterID, ok := findAttr(schema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("required query param clusterId was dropped from the schema: %+v", schema.Attributes)
	}
	if !clusterID.Required || clusterID.Optional || clusterID.Computed {
		t.Errorf("appended cluster_id must be Required, got Required=%v Optional=%v Computed=%v",
			clusterID.Required, clusterID.Optional, clusterID.Computed)
	}
	if clusterID.WireName != "clusterId" {
		t.Errorf("cluster_id WireName = %q, want clusterId", clusterID.WireName)
	}
	if clusterID.Description != "id of the defining cluster" {
		t.Errorf("cluster_id description = %q, want the query param's description", clusterID.Description)
	}
}

// TestManagedResourceSchema_OptionalQueryParamAppended locks in that an
// optional query parameter with no matching body/response property is added
// as Optional so it can be sent when the practitioner sets it.
func TestManagedResourceSchema_OptionalQueryParamAppended(t *testing.T) {
	c := mapCRUDWithClusterQuery(false)
	c.Create.Parameters = []Parameter{
		{Name: "page", In: "query", Type: "integer", Description: "page number"},
	}

	schema, _ := ManagedResourceSchema(c)
	page, ok := findAttr(schema.Attributes, "page")
	if !ok {
		t.Fatalf("optional query param page was dropped from the schema: %+v", schema.Attributes)
	}
	if !page.Optional || page.Required || page.Computed {
		t.Errorf("appended page must be Optional, got Required=%v Optional=%v Computed=%v",
			page.Required, page.Optional, page.Computed)
	}
}

// TestManagedResourceSchema_RequiredQueryParamOnUpdateOnly still forces
// Required when only the update operation declares the required query param.
func TestManagedResourceSchema_RequiredQueryParamOnUpdateOnly(t *testing.T) {
	c := mapCRUDWithClusterQuery(false)
	c.Update = &Operation{
		Method: MethodPut,
		Path:   "/maps/{id}",
		Parameters: []Parameter{
			{Name: "clusterId", In: "query", Required: true, Type: "string"},
		},
	}

	schema, _ := ManagedResourceSchema(c)
	clusterID, ok := findAttr(schema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("no cluster_id attribute in schema: %+v", schema.Attributes)
	}
	if !clusterID.Required || clusterID.Optional || clusterID.Computed {
		t.Errorf("cluster_id must be Required (required on update), got Required=%v Optional=%v Computed=%v",
			clusterID.Required, clusterID.Optional, clusterID.Computed)
	}
}

// TestManagedResourceSchema_RequiredHeaderParamForcesRequired covers header
// parameters the same way as query parameters: a required header that maps to
// an existing attribute is Required, not Optional+Computed.
func TestManagedResourceSchema_RequiredHeaderParamForcesRequired(t *testing.T) {
	c := mapCRUDWithClusterQuery(false)
	c.Create.Parameters = []Parameter{
		{Name: "X-Cluster-Id", In: "header", Required: true, Type: "string", Description: "target cluster"},
	}
	c.Read.ResponseSchema.Properties["X-Cluster-Id"] = SchemaSpec{Type: "string"}

	schema, _ := ManagedResourceSchema(c)
	attr, ok := findAttr(schema.Attributes, "x_cluster_id")
	if !ok {
		t.Fatalf("no x_cluster_id attribute in schema: %+v", schema.Attributes)
	}
	if !attr.Required || attr.Optional || attr.Computed {
		t.Errorf("x_cluster_id must be Required (required header), got Required=%v Optional=%v Computed=%v",
			attr.Required, attr.Optional, attr.Computed)
	}
}

// TestManagedResourceSchema_ReadOnlyBodyPropertyIsComputed locks in that a
// readOnly request-body property is not a practitioner input. Without a
// matching required query param it is Computed-only, not Optional+Computed.
func TestManagedResourceSchema_ReadOnlyBodyPropertyIsComputed(t *testing.T) {
	c := mapCRUDWithClusterQuery(false)
	schema, _ := ManagedResourceSchema(c)
	clusterID, ok := findAttr(schema.Attributes, "cluster_id")
	if !ok {
		t.Fatalf("no cluster_id attribute in schema: %+v", schema.Attributes)
	}
	if !clusterID.Computed || clusterID.Required || clusterID.Optional {
		t.Errorf("readOnly cluster_id with no required query param must be Computed-only, got Required=%v Optional=%v Computed=%v",
			clusterID.Required, clusterID.Optional, clusterID.Computed)
	}
}

// TestManagedResourceSchema_OptionalQueryParamDoesNotDemoteRequired keeps a
// body-required attribute Required when an optional query param collides
// with the same sanitized name.
func TestManagedResourceSchema_OptionalQueryParamDoesNotDemoteRequired(t *testing.T) {
	c := mapCRUDWithClusterQuery(false)
	c.Create.Parameters = []Parameter{
		{Name: "name", In: "query", Type: "string"},
	}
	schema, _ := ManagedResourceSchema(c)
	name, ok := findAttr(schema.Attributes, "name")
	if !ok {
		t.Fatal("no name attribute in schema")
	}
	if !name.Required || name.Optional || name.Computed {
		t.Errorf("name must stay Required, got Required=%v Optional=%v Computed=%v",
			name.Required, name.Optional, name.Computed)
	}
}

// mapCRUDWithClusterQuery is a managed resource whose clusterId is a readOnly
// body/response property (not in the body's required list). When requiredQuery
// is true the create operation also declares clusterId as a required query
// parameter — the same parent-scope query pattern many collection APIs use.
func mapCRUDWithClusterQuery(requiredQuery bool) ResourceCRUD {
	create := &Operation{
		Method: MethodPost,
		Path:   "/maps",
		RequestSchema: &SchemaSpec{
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]SchemaSpec{
				"name":      {Type: "string"},
				"clusterId": {Type: "string", ReadOnly: true, Description: "id of the defining cluster"},
			},
		},
	}
	if requiredQuery {
		create.Parameters = []Parameter{
			{Name: "clusterId", In: "query", Required: true, Type: "string", Description: "id of the defining cluster"},
		}
	}
	return ResourceCRUD{
		Name:           "map",
		CollectionPath: "/maps",
		InstancePath:   "/maps/{id}",
		Create:         create,
		Read: &Operation{
			Method: MethodGet,
			Path:   "/maps/{id}",
			ResponseSchema: &SchemaSpec{
				Type: "object",
				Properties: map[string]SchemaSpec{
					"id":        {Type: "string"},
					"name":      {Type: "string"},
					"clusterId": {Type: "string", ReadOnly: true, Description: "id of the defining cluster"},
				},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/maps/{id}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"id"}, AttributeName: "id", ImportFormat: "%s"},
	}
}

// TestManagedResourceSchema_MergesUpdateOnlyRequestProperties locks in the
// G39 fix: a property the UPDATE request body carries but the CREATE body
// lacks must stay settable (Optional+Computed, not response-only Computed)
// and its description must surface, instead of the update half of the API
// being silently dropped. Create stays authoritative: an update-required-only
// property is Optional (forcing it Required would demand a value the create
// API never accepts), and a property present in both bodies keeps the Create
// body's description.
func TestManagedResourceSchema_MergesUpdateOnlyRequestProperties(t *testing.T) {
	c := ResourceCRUD{
		Name: "gadget",
		Create: &Operation{
			Method: MethodPost, Path: "/gadgets", OperationID: "createGadget",
			RequestSchema: &SchemaSpec{
				Required: []string{"name"},
				Properties: map[string]SchemaSpec{
					"name":  {Type: "string", Description: "create-side name"},
					"color": {Type: "string", Description: "create-side color"},
				},
			},
		},
		Update: &Operation{
			Method: MethodPut, Path: "/gadgets/{gadgetId}", OperationID: "updateGadget",
			RequestSchema: &SchemaSpec{
				Required: []string{"name", "rebuildPolicy"},
				Properties: map[string]SchemaSpec{
					"name":          {Type: "string", Description: "update-side name"},
					"rebuildPolicy": {Type: "string", Description: "how the gadget is rebuilt on update"},
				},
			},
		},
		Read: &Operation{
			Method: MethodGet, Path: "/gadgets/{gadgetId}", OperationID: "getGadget",
			ResponseSchema: &SchemaSpec{Properties: map[string]SchemaSpec{
				"name":          {Type: "string"},
				"color":         {Type: "string"},
				"rebuildPolicy": {Type: "string"},
			}},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/gadgets/{gadgetId}", OperationID: "deleteGadget"},
	}

	schema, _ := ManagedResourceSchema(c)
	attrs := map[string]ir.AttributeIR{}
	for _, a := range schema.Attributes {
		attrs[a.Name] = a
	}

	rebuild, ok := attrs["rebuild_policy"]
	if !ok {
		t.Fatalf("update-only request property rebuildPolicy missing from schema, got %+v", schema.Attributes)
	}
	if !rebuild.Optional || !rebuild.Computed || rebuild.Required {
		t.Errorf("update-only property must be Optional+Computed (settable), got Optional=%v Computed=%v Required=%v",
			rebuild.Optional, rebuild.Computed, rebuild.Required)
	}
	if rebuild.Description != "how the gadget is rebuilt on update" {
		t.Errorf("update-only property description dropped, got %q", rebuild.Description)
	}

	name, ok := attrs["name"]
	if !ok {
		t.Fatal("name attribute missing")
	}
	if !name.Required {
		t.Errorf("create-required name must stay Required, got %+v", name)
	}
	if name.Description != "create-side name" {
		t.Errorf("create body must stay authoritative for shared properties, got description %q", name.Description)
	}

	color, ok := attrs["color"]
	if !ok {
		t.Fatal("color attribute missing")
	}
	if !color.Optional || !color.Computed || color.Required {
		t.Errorf("create-optional color must stay Optional+Computed, got %+v", color)
	}
}

func TestManagedResourceSchema_PromotesNestedPathParameters(t *testing.T) {
	metadata := SchemaSpec{
		Type:     "object",
		Required: []string{"name", "workspace"},
		Properties: map[string]SchemaSpec{
			"labels":    {Type: "object", AdditionalProperties: &SchemaSpec{Type: "string"}},
			"name":      {Type: "string"},
			"workspace": {Type: "string"},
		},
	}
	state := &SchemaSpec{
		Type:     "object",
		Required: []string{"metadata"},
		Properties: map[string]SchemaSpec{
			"metadata": metadata,
			"spec":     {Type: "object", Properties: map[string]SchemaSpec{"image": {Type: "string"}}},
		},
	}
	request := &SchemaSpec{
		Type:     "object",
		Required: []string{"metadata"},
		Properties: map[string]SchemaSpec{
			"metadata": metadata,
			"spec":     {Type: "object", Properties: map[string]SchemaSpec{"image": {Type: "string"}}},
		},
	}
	c := ResourceCRUD{
		Name:           "instance",
		CollectionPath: "/workspaces/{workspace}/instances",
		InstancePath:   "/workspaces/{workspace}/instances/{name}",
		Create: &Operation{
			Method: MethodPost, Path: "/workspaces/{workspace}/instances", RequestSchema: request, ResponseSchema: state,
			Parameters: []Parameter{{Name: "workspace", In: "path", Required: true, Type: "string"}},
		},
		Read: &Operation{
			Method: MethodGet, Path: "/workspaces/{workspace}/instances/{name}", ResponseSchema: state,
			Parameters: []Parameter{
				{Name: "workspace", In: "path", Required: true, Type: "string"},
				{Name: "name", In: "path", Required: true, Type: "string"},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/workspaces/{workspace}/instances/{name}"},
		ID: IDInfo{
			Kind: IDComposite, ParameterNames: []string{"workspace", "name"}, ImportFormat: "%s:%s",
		},
	}

	schema, _ := ManagedResourceSchema(c)
	for _, name := range []string{"name", "workspace"} {
		attr, ok := findAttr(schema.Attributes, name)
		if !ok {
			t.Fatalf("promoted path attribute %q missing from schema: %+v", name, schema.Attributes)
		}
		if attr.WirePath != "metadata" {
			t.Errorf("%s WirePath = %q, want metadata", name, attr.WirePath)
		}
		if !attr.Required {
			t.Errorf("%s must remain Required after promotion: %+v", name, attr)
		}
	}

	meta, ok := findAttr(schema.Attributes, "metadata")
	if !ok {
		t.Fatal("metadata with unrelated labels must remain in the schema")
	}
	if _, ok := findAttr(meta.Schema.Attributes, "name"); ok {
		t.Error("promoted name must not remain duplicated under metadata")
	}
	if _, ok := findAttr(meta.Schema.Attributes, "workspace"); ok {
		t.Error("promoted workspace must not remain duplicated under metadata")
	}
	if _, ok := findAttr(meta.Schema.Attributes, "labels"); !ok {
		t.Error("unrelated metadata.labels must remain nested")
	}

	// Schema construction must not mutate operation schemas shared by other
	// transformer passes.
	if _, ok := state.Properties["metadata"].Properties["name"]; !ok {
		t.Error("state schema was mutated while building the managed schema")
	}
	if _, ok := request.Properties["metadata"].Properties["workspace"]; !ok {
		t.Error("request schema was mutated while building the managed schema")
	}
}

func TestManagedResourceSchema_AmbiguousNestedPathParameterStaysUnwired(t *testing.T) {
	state := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
		"dashboard": {Type: "object", Properties: map[string]SchemaSpec{"uid": {Type: "string"}}},
		"metadata":  {Type: "object", Properties: map[string]SchemaSpec{"uid": {Type: "string"}}},
	}}
	c := ResourceCRUD{
		Name:   "dashboard",
		Create: &Operation{Method: MethodPost, Path: "/dashboards", ResponseSchema: state},
		Read: &Operation{
			Method: MethodGet, Path: "/dashboards/{uid}", ResponseSchema: state,
			Parameters: []Parameter{{Name: "uid", In: "path", Required: true, Type: "string"}},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/dashboards/{uid}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"uid"}, AttributeName: "uid", ImportFormat: "%s"},
	}
	var diags diagnostics.Diagnostics

	schema, _ := ManagedResourceSchemaWithDiagnostics(c, &diags, false)
	if _, ok := findAttr(schema.Attributes, "uid"); ok {
		t.Fatalf("ambiguous nested uid must not be promoted or synthesized: %+v", schema.Attributes)
	}
	if !hasWarning(diags, "ambiguous nested path parameter") {
		t.Fatalf("expected fail-loud ambiguity warning, got %+v", diags)
	}
}

func TestManagedResourceSchema_TopLevelPathParameterWins(t *testing.T) {
	state := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{
		"dashboard": {Type: "object", Properties: map[string]SchemaSpec{"uid": {Type: "string"}}},
		"uid":       {Type: "string"},
	}}
	c := ResourceCRUD{
		Name:   "dashboard",
		Create: &Operation{Method: MethodPost, Path: "/dashboards", ResponseSchema: state},
		Read: &Operation{
			Method: MethodGet, Path: "/dashboards/{uid}", ResponseSchema: state,
			Parameters: []Parameter{{Name: "uid", In: "path", Required: true, Type: "string"}},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/dashboards/{uid}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"uid"}, AttributeName: "uid", ImportFormat: "%s"},
	}

	schema, _ := ManagedResourceSchema(c)
	uid, ok := findAttr(schema.Attributes, "uid")
	if !ok {
		t.Fatalf("top-level uid missing: %+v", schema.Attributes)
	}
	if uid.WirePath != "" {
		t.Fatalf("top-level uid WirePath = %q, want empty", uid.WirePath)
	}
}
