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
// TestManagedResourceSchema_ChildRead drives the childRead gate through the
// public schema builder: a child resource (read_collection_path, issue #64)
// derives its state shape from the create request body instead of the
// parent-collection read response, and folds the parent path parameters into
// the schema as Required PathParam attributes so the CRUD paths can be filled
// from state (and the request body, which reflects the whole model, can omit
// them).
func TestManagedResourceSchema_ChildRead(t *testing.T) {
	childCRUD := func() ResourceCRUD {
		return ResourceCRUD{
			Name:           "port_filter_rule",
			CollectionPath: "/ports/{portId}/filters/rules",
			InstancePath:   "/ports/{portId}/filters/rules/{ruleId}",
			Create: &Operation{
				Method: MethodPost,
				Path:   "/ports/{portId}/filters/rules",
				Parameters: []Parameter{
					{Name: "portId", In: "path", Required: true, Type: "string", Description: "parent port"},
				},
				RequestSchema: &SchemaSpec{
					Type:     "object",
					Required: []string{"ruleAction"},
					Properties: map[string]SchemaSpec{
						"ruleId":     {Type: "string"},
						"ruleAction": {Type: "string", Description: "pass or drop"},
					},
				},
			},
			Read: &Operation{
				Method: MethodGet,
				Path:   "/ports/{portId}/filters/rules/{ruleId}",
				Parameters: []Parameter{
					{Name: "portId", In: "path", Required: true, Type: "string"},
					{Name: "ruleId", In: "path", Required: true, Type: "string"},
				},
				// The parent read returns the whole filter, not a rule: its
				// shape must NOT leak into the child's state.
				ResponseSchema: &SchemaSpec{
					Type:       "object",
					Properties: map[string]SchemaSpec{"ruleCount": {Type: "integer"}},
				},
			},
			Delete: &Operation{Method: MethodDelete, Path: "/ports/{portId}/filters/rules/{ruleId}"},
			ID:     IDInfo{Kind: IDSimple, ParameterNames: []string{"ruleId"}, AttributeName: "rule_id", ImportFormat: "%s"},
		}
	}

	t.Run("state shape comes from the create request body", func(t *testing.T) {
		schema, _ := ManagedResourceSchemaWithDiagnostics(childCRUD(), nil, false, true)
		if _, ok := findAttr(schema.Attributes, "rule_action"); !ok {
			t.Errorf("rule_action from the create body expected, got %+v", schema.Attributes)
		}
		if _, ok := findAttr(schema.Attributes, "rule_count"); ok {
			t.Errorf("rule_count belongs to the parent read shape and must be absent, got %+v", schema.Attributes)
		}
	})

	t.Run("path parameters fold in as Required PathParam attributes", func(t *testing.T) {
		schema, _ := ManagedResourceSchemaWithDiagnostics(childCRUD(), nil, false, true)
		attr, ok := findAttr(schema.Attributes, "port_id")
		if !ok {
			t.Fatalf("folded port_id attribute expected, got %+v", schema.Attributes)
		}
		if !attr.Required || attr.Optional || attr.Computed {
			t.Errorf("port_id flags = Required:%v Optional:%v Computed:%v, want Required only", attr.Required, attr.Optional, attr.Computed)
		}
		if !attr.PathParam {
			t.Errorf("port_id PathParam = false, want true (request bodies must omit it)")
		}
		if attr.WireName != "portId" {
			t.Errorf("port_id WireName = %q, want portId", attr.WireName)
		}
		if attr.Description != "parent port" {
			t.Errorf("port_id Description = %q, want parent port", attr.Description)
		}
	})

	t.Run("without childRead the read shape wins and path params stay out", func(t *testing.T) {
		schema, _ := ManagedResourceSchemaWithDiagnostics(childCRUD(), nil, false, false)
		if _, ok := findAttr(schema.Attributes, "rule_count"); !ok {
			t.Errorf("rule_count from the read response expected, got %+v", schema.Attributes)
		}
		if attr, ok := findAttr(schema.Attributes, "port_id"); ok && attr.PathParam {
			t.Errorf("port_id must not be a folded PathParam without childRead, got %+v", attr)
		}
	})
}

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
