package transformer

import (
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestDetectIDFromPath covers the exported entry point to parsePath+detectID:
// simple, composite, and parameterless paths.
func TestDetectIDFromPath(t *testing.T) {
	simple := DetectIDFromPath("/pets/{petId}")
	if simple.Kind != IDSimple || simple.AttributeName != "pet_id" {
		t.Errorf("simple path = %+v, want IDSimple pet_id", simple)
	}
	composite := DetectIDFromPath("/x/{notifType}/{taskId}")
	if composite.Kind != IDComposite || len(composite.ParameterNames) != 2 {
		t.Errorf("composite path = %+v, want IDComposite with 2 params", composite)
	}
	none := DetectIDFromPath("/pets")
	if len(none.ParameterNames) != 0 {
		t.Errorf("parameterless path = %+v, want no parameters", none)
	}
}

// TestUserSettableIdentifier covers the identifier-preference heuristic: exact,
// suffix, and prefix name matches, the response-echo and user-settable gates,
// and the exact > suffix > prefix precedence with longest-name tiebreak.
func TestUserSettableIdentifier(t *testing.T) {
	attrs := func(names ...string) []ir.AttributeIR {
		out := make([]ir.AttributeIR, 0, len(names))
		for _, n := range names {
			out = append(out, ir.AttributeIR{Name: n, Schema: ir.SchemaIR{Type: ir.TypeString}})
		}
		return out
	}
	state := func(names ...string) *SchemaSpec {
		props := make(map[string]SchemaSpec, len(names))
		for _, n := range names {
			props[n] = SchemaSpec{Type: "string"}
		}
		return &SchemaSpec{Type: "object", Properties: props}
	}
	request := func(names ...string) *SchemaSpec {
		props := make(map[string]SchemaSpec, len(names))
		for _, n := range names {
			props[n] = SchemaSpec{Type: "string"}
		}
		return &SchemaSpec{Type: "object", Properties: props}
	}

	t.Run("nil specs return empty", func(t *testing.T) {
		if got := userSettableIdentifier(nil, nil, nil, "id"); got != "" {
			t.Errorf("nil specs = %q, want empty", got)
		}
	})

	t.Run("exact match wins", func(t *testing.T) {
		got := userSettableIdentifier(attrs("username", "email"), state("username", "email"), request("username", "email"), "userName")
		if got != "username" {
			t.Errorf("exact match = %q, want username", got)
		}
	})

	t.Run("suffix match", func(t *testing.T) {
		got := userSettableIdentifier(attrs("alias"), state("alias"), request("alias"), "serverAlias")
		if got != "alias" {
			t.Errorf("suffix match = %q, want alias", got)
		}
	})

	t.Run("prefix match", func(t *testing.T) {
		got := userSettableIdentifier(attrs("port"), state("port"), request("port"), "portId")
		if got != "port" {
			t.Errorf("prefix match = %q, want port", got)
		}
	})

	t.Run("suffix beats prefix", func(t *testing.T) {
		// {serverAlias}: "alias" (suffix) and "server" (prefix) both relate; no
		// exact match, so suffix wins over prefix.
		got := userSettableIdentifier(attrs("alias", "server"), state("alias", "server"), request("alias", "server"), "serverAlias")
		if got != "alias" {
			t.Errorf("suffix-over-prefix = %q, want alias", got)
		}
	})

	t.Run("longest name wins within a category", func(t *testing.T) {
		// {serverAlias}: "alias" (suffix, len 5) beats "s" (suffix, len 1).
		got := userSettableIdentifier(attrs("s", "alias"), state("s", "alias"), request("s", "alias"), "serverAlias")
		if got != "alias" {
			t.Errorf("longest suffix = %q, want alias", got)
		}
	})

	t.Run("computed-only attribute is not settable", func(t *testing.T) {
		a := attrs("port")
		a[0].Computed = true
		a[0].Optional = false
		got := userSettableIdentifier(a, state("port"), request("port"), "portId")
		if got != "" {
			t.Errorf("computed-only = %q, want empty", got)
		}
	})

	t.Run("non-echoed attribute is not a candidate", func(t *testing.T) {
		// "port" is in the request body but not the response.
		got := userSettableIdentifier(attrs("port"), state("other"), request("port"), "portId")
		if got != "" {
			t.Errorf("non-echoed = %q, want empty", got)
		}
	})

	t.Run("no related name returns empty", func(t *testing.T) {
		got := userSettableIdentifier(attrs("label"), state("label"), request("label"), "policyId")
		if got != "" {
			t.Errorf("unrelated = %q, want empty", got)
		}
	})
}

// TestResolveIdentifierAttribute covers the identifier decision: the
// user-settable preference, the synthetic fallback carrying the path parameter's
// description, and the skipUserSettableID gate.
func TestResolveIdentifierAttribute(t *testing.T) {
	base := func() ([]ir.AttributeIR, ResourceCRUD, *SchemaSpec, *SchemaSpec) {
		attrs := []ir.AttributeIR{{Name: "port", Schema: ir.SchemaIR{Type: ir.TypeString}}}
		c := ResourceCRUD{
			Read: &Operation{
				Method: MethodGet,
				Path:   "/ports/{portId}",
				Parameters: []Parameter{
					{Name: "portId", In: "path", Description: "id of the target device Port"},
				},
			},
		}
		state := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"port": {Type: "string"}}}
		request := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"port": {Type: "string"}}}
		return attrs, c, state, request
	}

	t.Run("user-settable identifier preferred", func(t *testing.T) {
		attrs, c, state, request := base()
		id, out := resolveIdentifierAttribute(attrs, c, state, request, "port_id", false)
		if id != "port" {
			t.Errorf("resolved id = %q, want port", id)
		}
		if len(out) != 1 {
			t.Errorf("no synthetic should be added, got %+v", out)
		}
	})

	t.Run("synthetic fallback carries path param description", func(t *testing.T) {
		attrs, c, state, request := base()
		// Remove "port" from the request body so no user-settable identifier.
		request.Properties = map[string]SchemaSpec{"other": {Type: "string"}}
		id, out := resolveIdentifierAttribute(attrs, c, state, request, "port_id", false)
		if id != "port_id" {
			t.Errorf("resolved id = %q, want port_id", id)
		}
		if len(out) != 2 {
			t.Fatalf("synthetic should be appended, got %+v", out)
		}
		synth := out[1]
		if synth.Name != "port_id" || !synth.Computed {
			t.Errorf("synthetic = %+v, want Computed port_id", synth)
		}
		if synth.Description != "id of the target device Port" {
			t.Errorf("synthetic description = %q, want the path param's prose", synth.Description)
		}
	})

	t.Run("skipUserSettableID forces the synthetic", func(t *testing.T) {
		attrs, c, state, request := base()
		id, out := resolveIdentifierAttribute(attrs, c, state, request, "port_id", true)
		if id != "port_id" {
			t.Errorf("resolved id = %q, want port_id (skipUserSettableID)", id)
		}
		if len(out) != 2 {
			t.Fatalf("synthetic should be appended despite user-settable, got %+v", out)
		}
	})
}

// TestPathParamDescription covers the path-parameter description lookup across
// the read/update/delete/create operations and the not-found case.
func TestPathParamDescription(t *testing.T) {
	t.Run("description from read path param", func(t *testing.T) {
		c := ResourceCRUD{
			Read: &Operation{
				Method: MethodGet,
				Path:   "/ports/{portId}",
				Parameters: []Parameter{
					{Name: "verbose", In: "query", Description: "not a path param"},
					{Name: "portId", In: "path", Description: "the port id"},
				},
			},
		}
		if got := pathParamDescription(c, "port_id"); got != "the port id" {
			t.Errorf("pathParamDescription = %q, want the port id", got)
		}
	})

	t.Run("description from update path param", func(t *testing.T) {
		c := ResourceCRUD{
			Update: &Operation{
				Method: MethodPut,
				Path:   "/ports/{portId}",
				Parameters: []Parameter{
					{Name: "portId", In: "path", Description: "update prose"},
				},
			},
		}
		if got := pathParamDescription(c, "port_id"); got != "update prose" {
			t.Errorf("pathParamDescription = %q, want update prose", got)
		}
	})

	t.Run("no matching path param", func(t *testing.T) {
		c := ResourceCRUD{
			Read: &Operation{
				Method: MethodGet,
				Path:   "/ports/{portId}",
				Parameters: []Parameter{
					{Name: "portId", In: "path", Description: "the port id"},
				},
			},
		}
		if got := pathParamDescription(c, "other_id"); got != "" {
			t.Errorf("pathParamDescription = %q, want empty", got)
		}
	})

	t.Run("nil operations", func(t *testing.T) {
		if got := pathParamDescription(ResourceCRUD{}, "id"); got != "" {
			t.Errorf("pathParamDescription = %q, want empty", got)
		}
	})
}

// TestManagedResourceSchema_SkipUserSettableID drives the skipUserSettableID
// gate through the public schema builder: an explicit id_attribute/import_format
// override keeps the synthetic Computed placeholder named for the path parameter
// instead of preferring a practitioner-supplied create-body attribute.
func TestManagedResourceSchema_SkipUserSettableID(t *testing.T) {
	c := ResourceCRUD{
		Name:           "port",
		CollectionPath: "/ports",
		InstancePath:   "/ports/{portId}",
		Create: &Operation{
			Method:        MethodPost,
			Path:          "/ports",
			RequestSchema: &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"port": {Type: "string"}}},
		},
		Read: &Operation{
			Method: MethodGet,
			Path:   "/ports/{portId}",
			ResponseSchema: &SchemaSpec{
				Type:     "object",
				Required: []string{"port"},
				Properties: map[string]SchemaSpec{
					"port": {Type: "string"},
				},
			},
		},
		Delete: &Operation{Method: MethodDelete, Path: "/ports/{portId}"},
		ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"portId"}, AttributeName: "port_id", ImportFormat: "%s"},
	}

	// Without skip: the user-settable "port" attribute is the identifier.
	schema, idAttr := ManagedResourceSchemaWithDiagnostics(c, nil, false, false)
	if idAttr != "port" {
		t.Errorf("idAttr = %q, want port (user-settable preference)", idAttr)
	}
	if _, ok := findAttr(schema.Attributes, "port_id"); ok {
		t.Errorf("no synthetic port_id expected without skip, got %+v", schema.Attributes)
	}

	// With skip: the synthetic port_id placeholder is added and is the identifier.
	schema, idAttr = ManagedResourceSchemaWithDiagnostics(c, nil, true, false)
	if idAttr != "port_id" {
		t.Errorf("idAttr = %q, want port_id (skipUserSettableID)", idAttr)
	}
	if _, ok := findAttr(schema.Attributes, "port_id"); !ok {
		t.Errorf("synthetic port_id expected with skip, got %+v", schema.Attributes)
	}
}
