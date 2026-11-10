package api

import (
	"os"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func findResource(t *testing.T, p *ir.ProviderIR, name string) ir.ResourceIR {
	t.Helper()
	for _, r := range p.Resources {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("resource %q not found among %d resources", name, len(p.Resources))
	return ir.ResourceIR{}
}

func hasAttr(r ir.ResourceIR, name string) bool {
	for _, a := range r.Schema.Attributes {
		if a.Name == name {
			return true
		}
	}
	return false
}

// TestConditionalHeaders_ReferenceSpec pins If-Match / If-None-Match detection
// on the etag-concurrency reference spec: $ref'd and inline headers, required
// and optional, case-insensitive names, ETag response headers, and the headers
// staying out of both HeaderParams and the resource schema.
func TestConditionalHeaders_ReferenceSpec(t *testing.T) {
	spec, err := os.ReadFile("../../test/specs/etag-concurrency.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, _, diags, err := BuildProviderIR(spec, nil)
	if err != nil {
		t.Fatalf("BuildProviderIR: %v", err)
	}
	if diags.HasErrors() {
		t.Fatalf("unexpected errors: %v", diags)
	}

	widget := findResource(t, p, "widget")
	if got := widget.CRUDMapping.Create.IfNoneMatch; got != ir.ConditionalHeaderOptional {
		t.Errorf("widget create If-None-Match = %q, want optional ($ref)", got)
	}
	if widget.CRUDMapping.Update == nil || widget.CRUDMapping.Update.IfMatch != ir.ConditionalHeaderRequired {
		t.Errorf("widget update If-Match = %+v, want required ($ref)", widget.CRUDMapping.Update)
	}
	if got := widget.CRUDMapping.Delete.IfMatch; got != ir.ConditionalHeaderOptional {
		t.Errorf("widget delete If-Match = %q, want optional", got)
	}
	if !widget.CRUDMapping.Read.ResponseETag || !widget.CRUDMapping.Update.ResponseETag {
		t.Errorf("widget read/update should record the declared ETag response header")
	}
	for _, h := range widget.CRUDMapping.Update.HeaderParams {
		if strings.EqualFold(h.Name, "If-Match") {
			t.Errorf("If-Match leaked into update HeaderParams: %+v", widget.CRUDMapping.Update.HeaderParams)
		}
	}
	if len(widget.CRUDMapping.Update.HeaderParams) != 1 || widget.CRUDMapping.Update.HeaderParams[0].Name != "X-Trace-Id" {
		t.Errorf("update HeaderParams = %+v, want only X-Trace-Id", widget.CRUDMapping.Update.HeaderParams)
	}
	if hasAttr(widget, "if_match") || hasAttr(widget, "if_none_match") {
		t.Errorf("widget schema exposes a conditional header attribute")
	}
	if !hasAttr(widget, "x_trace_id") {
		t.Errorf("widget schema lost the ordinary X-Trace-Id header attribute")
	}

	gadget := findResource(t, p, "gadget")
	if gadget.CRUDMapping.Update == nil || gadget.CRUDMapping.Update.IfMatch != ir.ConditionalHeaderOptional {
		t.Errorf("gadget update (lower-case if-match) = %+v, want optional", gadget.CRUDMapping.Update)
	}
	if got := gadget.CRUDMapping.Delete.IfMatch; got != ir.ConditionalHeaderRequired {
		t.Errorf("gadget delete If-Match = %q, want required ($ref)", got)
	}
	if gadget.CRUDMapping.Read.ResponseETag {
		t.Errorf("gadget read declares no ETag header")
	}

	var enabled int
	var optionalDelete bool
	for _, d := range diags {
		if d.Summary == "ETag conditional requests enabled" {
			enabled++
		}
		if d.Severity == diagnostics.Info && strings.Contains(d.Detail, `resource "inventory-api_widget" declares the optional If-Match`) {
			optionalDelete = true
		}
	}
	if enabled != 2 || !optionalDelete {
		t.Errorf("diagnostics = %v; want two enabled Infos and the optional-delete Info", diags)
	}
}

// TestConditionalHeaders_Overrides pins the conditional_delete and
// etag_tracking overrides, including their fail-loud no-op warnings.
func TestConditionalHeaders_Overrides(t *testing.T) {
	spec, err := os.ReadFile("../../test/specs/etag-concurrency.yaml")
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "inventory", Version: "1.0.0"},
		ResourceOverrides: []config.ResourceOverride{
			{Operation: "createWidget", ConditionalDelete: "always"},
			{Operation: "createGadget", ETagTracking: &off},
		},
	}
	p, _, diags, err := BuildProviderIR(spec, cfg)
	if err != nil {
		t.Fatalf("BuildProviderIR: %v", err)
	}
	if w := findResource(t, p, "widget"); w.ConditionalDelete != ir.ConditionalDeleteAlways {
		t.Errorf("widget ConditionalDelete = %q, want always", w.ConditionalDelete)
	}
	if g := findResource(t, p, "gadget"); !g.ETagTrackingDisabled {
		t.Errorf("gadget ETagTrackingDisabled = false, want true")
	}
	for _, d := range diags {
		if strings.Contains(d.Detail, `resource "inventory_widget" declares the optional If-Match`) {
			t.Errorf("optional-delete Info should disappear once conditional_delete: always is set: %v", d)
		}
		if strings.Contains(d.Summary, "override has no effect") {
			t.Errorf("unexpected no-op override warning: %v", d)
		}
	}
}

// TestConditionalHeaders_PathLevelAndSwagger2 covers a path-level If-Match
// parameter (inherited by the operations of the path item, with an
// operation-level override winning) on a Swagger 2.0 spec, and the no-op
// override warnings for a resource that declares no If-Match.
func TestConditionalHeaders_PathLevelAndSwagger2(t *testing.T) {
	spec := []byte(`
swagger: "2.0"
info: {title: Things, version: 1.0.0}
host: things.example.com
paths:
  /things:
    post:
      operationId: createThing
      parameters:
        - in: body
          name: body
          required: true
          schema: {$ref: '#/definitions/Thing'}
      responses:
        "201":
          description: created
          schema: {$ref: '#/definitions/Thing'}
  /things/{id}:
    parameters:
      - {name: id, in: path, required: true, type: string}
      - {name: If-Match, in: header, required: true, type: string}
    get:
      operationId: getThing
      parameters:
        - {name: If-Match, in: header, required: false, type: string}
      responses:
        "200":
          description: ok
          headers:
            etag: {type: string}
          schema: {$ref: '#/definitions/Thing'}
    put:
      operationId: updateThing
      parameters:
        - in: body
          name: body
          required: true
          schema: {$ref: '#/definitions/Thing'}
      responses:
        "200":
          description: ok
          schema: {$ref: '#/definitions/Thing'}
    delete:
      operationId: deleteThing
      responses:
        "204": {description: deleted}
  /others:
    post:
      operationId: createOther
      parameters:
        - in: body
          name: body
          required: true
          schema: {$ref: '#/definitions/Thing'}
      responses:
        "201":
          description: created
          schema: {$ref: '#/definitions/Thing'}
  /others/{id}:
    parameters:
      - {name: id, in: path, required: true, type: string}
    get:
      operationId: getOther
      responses:
        "200":
          description: ok
          schema: {$ref: '#/definitions/Thing'}
    delete:
      operationId: deleteOther
      responses:
        "204": {description: deleted}
definitions:
  Thing:
    type: object
    properties:
      id: {type: string, readOnly: true}
      name: {type: string}
`)
	off := false
	cfg := &config.Config{
		Provider: config.ProviderConfig{Name: "things", Version: "1.0.0"},
		ResourceOverrides: []config.ResourceOverride{
			{Operation: "createOther", ConditionalDelete: "always", ETagTracking: &off},
		},
	}
	p, _, diags, err := BuildProviderIR(spec, cfg)
	if err != nil {
		t.Fatalf("BuildProviderIR: %v", err)
	}
	thing := findResource(t, p, "thing")
	if thing.CRUDMapping.Update == nil || thing.CRUDMapping.Update.IfMatch != ir.ConditionalHeaderRequired {
		t.Errorf("update should inherit the path-level required If-Match: %+v", thing.CRUDMapping.Update)
	}
	if thing.CRUDMapping.Delete.IfMatch != ir.ConditionalHeaderRequired {
		t.Errorf("delete should inherit the path-level required If-Match, got %q", thing.CRUDMapping.Delete.IfMatch)
	}
	if thing.CRUDMapping.Read.IfMatch != ir.ConditionalHeaderOptional {
		t.Errorf("read's operation-level optional If-Match should win over the path-level one, got %q", thing.CRUDMapping.Read.IfMatch)
	}
	if !thing.CRUDMapping.Read.ResponseETag {
		t.Errorf("lower-case etag response header should be detected")
	}
	if hasAttr(thing, "if_match") {
		t.Errorf("thing schema exposes if_match")
	}

	var deleteNoop, trackingNoop bool
	for _, d := range diags {
		if d.Severity != diagnostics.Warning {
			continue
		}
		switch d.Summary {
		case "conditional_delete override has no effect":
			deleteNoop = true
		case "etag_tracking override has no effect":
			trackingNoop = true
		}
	}
	if !deleteNoop || !trackingNoop {
		t.Errorf("want both no-op override warnings for the other resource, got %v", diags)
	}
}
