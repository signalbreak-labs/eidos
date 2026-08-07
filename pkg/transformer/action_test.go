package transformer

import (
	"reflect"
	"testing"

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
