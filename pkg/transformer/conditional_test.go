package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestIsConditionalHeader(t *testing.T) {
	for _, name := range []string{"If-Match", "if-match", " IF-MATCH ", "If-None-Match", "if-none-match"} {
		if !IsConditionalHeader(name) {
			t.Errorf("IsConditionalHeader(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "If-Modified-Since", "If-Unmodified-Since", "ETag", "X-If-Match"} {
		if IsConditionalHeader(name) {
			t.Errorf("IsConditionalHeader(%q) = true, want false", name)
		}
	}
}

func conditionalResource(createINM, updIM, delIM string) ir.ResourceIR {
	r := ir.ResourceIR{Name: "widget", TypeName: "example_widget"}
	r.CRUDMapping.Create = ir.OperationMappingIR{Method: "POST", PathTemplate: "/widgets", IfNoneMatch: createINM}
	r.CRUDMapping.Read = ir.OperationMappingIR{Method: "GET", PathTemplate: "/widgets/{id}"}
	r.CRUDMapping.Update = &ir.OperationMappingIR{Method: "PUT", PathTemplate: "/widgets/{id}", IfMatch: updIM}
	r.CRUDMapping.Delete = ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/widgets/{id}", IfMatch: delIM}
	return r
}

func TestPlanConditionalRequests(t *testing.T) {
	const req, opt = ir.ConditionalHeaderRequired, ir.ConditionalHeaderOptional
	cases := []struct {
		name     string
		r        ir.ResourceIR
		mutate   func(*ir.ResourceIR)
		want     ConditionalRequestPlan
		wantsAny bool
	}{
		{name: "none declared", r: conditionalResource("", "", "")},
		{
			name: "required update and delete",
			r:    conditionalResource("", req, req),
			want: ConditionalRequestPlan{TrackETag: true, UpdateIfMatch: true, UpdateIfMatchRequired: true, DeleteIfMatch: true, DeleteIfMatchRequired: true},
		},
		{
			name: "optional update tracks, optional delete skipped by default",
			r:    conditionalResource("", opt, opt),
			want: ConditionalRequestPlan{TrackETag: true, UpdateIfMatch: true},
		},
		{
			name:   "optional delete sent with conditional_delete always",
			r:      conditionalResource("", "", opt),
			mutate: func(r *ir.ResourceIR) { r.ConditionalDelete = ir.ConditionalDeleteAlways },
			want:   ConditionalRequestPlan{TrackETag: true, DeleteIfMatch: true},
		},
		{
			name: "required delete alone tracks",
			r:    conditionalResource("", "", req),
			want: ConditionalRequestPlan{TrackETag: true, DeleteIfMatch: true, DeleteIfMatchRequired: true},
		},
		{
			name: "create If-None-Match alone does not track",
			r:    conditionalResource(opt, "", ""),
			want: ConditionalRequestPlan{CreateIfNoneMatch: true},
		},
		{
			name:   "tracking disabled keeps only required headers as wildcard",
			r:      conditionalResource("", opt, req),
			mutate: func(r *ir.ResourceIR) { r.ETagTrackingDisabled = true },
			want:   ConditionalRequestPlan{DeleteIfMatch: true, DeleteIfMatchRequired: true},
		},
		{
			name: "tracking disabled drops optional delete even with always",
			r:    conditionalResource("", "", opt),
			mutate: func(r *ir.ResourceIR) {
				r.ETagTrackingDisabled = true
				r.ConditionalDelete = ir.ConditionalDeleteAlways
			},
			want: ConditionalRequestPlan{},
		},
		{
			name:   "no update mapping",
			r:      conditionalResource("", "", req),
			mutate: func(r *ir.ResourceIR) { r.CRUDMapping.Update = nil },
			want:   ConditionalRequestPlan{TrackETag: true, DeleteIfMatch: true, DeleteIfMatchRequired: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			if tc.mutate != nil {
				tc.mutate(&r)
			}
			got := PlanConditionalRequests(r)
			if got != tc.want {
				t.Fatalf("PlanConditionalRequests() = %+v, want %+v", got, tc.want)
			}
			if got.Any() != (tc.want != ConditionalRequestPlan{}) {
				t.Fatalf("Any() = %v for %+v", got.Any(), got)
			}
		})
	}
}

func TestConditionalRequestDiagnostics(t *testing.T) {
	const req, opt = ir.ConditionalHeaderRequired, ir.ConditionalHeaderOptional
	r := conditionalResource(opt, req, opt)
	r.CRUDMapping.Read.IfNoneMatch = req
	r.CRUDMapping.Create.IfMatch = opt
	var diags diagnostics.Diagnostics
	ConditionalRequestDiagnostics(&ir.ProviderIR{Resources: []ir.ResourceIR{r}}, &diags)

	find := func(substr string) *diagnostics.Diagnostic {
		for i := range diags {
			if strings.Contains(diags[i].Detail, substr) {
				return &diags[i]
			}
		}
		return nil
	}
	if d := find(`Create sends "If-None-Match: *"; Update sends If-Match.`); d == nil || d.Severity != diagnostics.Info {
		t.Fatalf("missing enabled Info diagnostic; got %v", diags)
	}
	if d := find("The delete operation of resource \"example_widget\" declares the optional If-Match"); d == nil || d.Severity != diagnostics.Info || !strings.Contains(d.Detail, "conditional_delete: always") {
		t.Fatalf("missing optional-delete Info with override hint; got %v", diags)
	}
	if d := find("The read operation of resource \"example_widget\" declares the required If-None-Match"); d == nil || d.Severity != diagnostics.Warning {
		t.Fatalf("missing required read If-None-Match Warning; got %v", diags)
	}
	if d := find("The create operation of resource \"example_widget\" declares the optional If-Match"); d == nil || d.Severity != diagnostics.Info {
		t.Fatalf("missing create If-Match Info; got %v", diags)
	}
	if len(diags) != 4 {
		t.Fatalf("got %d diagnostics, want 4: %v", len(diags), diags)
	}

	// A resource with no conditional headers stays silent.
	diags = nil
	ConditionalRequestDiagnostics(&ir.ProviderIR{Resources: []ir.ResourceIR{conditionalResource("", "", "")}}, &diags)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics for a plain resource: %v", diags)
	}
	ConditionalRequestDiagnostics(nil, &diags)
	ConditionalRequestDiagnostics(&ir.ProviderIR{}, nil)
}

func TestConditionalRequestDiagnostics_TrackingDisabled(t *testing.T) {
	r := conditionalResource("", ir.ConditionalHeaderOptional, ir.ConditionalHeaderRequired)
	r.ETagTrackingDisabled = true
	var diags diagnostics.Diagnostics
	ConditionalRequestDiagnostics(&ir.ProviderIR{Resources: []ir.ResourceIR{r}}, &diags)
	var sawEnabled, sawUpdate bool
	for _, d := range diags {
		if strings.Contains(d.Detail, `sent as "*"`) {
			sawEnabled = true
		}
		if strings.Contains(d.Detail, "The update operation") && strings.Contains(d.Detail, "etag_tracking: false") {
			sawUpdate = true
		}
	}
	if !sawEnabled || !sawUpdate {
		t.Fatalf("diagnostics = %v; want the wildcard note and the unsent optional update header", diags)
	}
}

// TestManagedResourceSchema_SkipsConditionalHeaders pins that If-Match and
// If-None-Match never become schema attributes while other header parameters
// still fold in.
func TestManagedResourceSchema_SkipsConditionalHeaders(t *testing.T) {
	body := &SchemaSpec{Type: "object", Properties: map[string]SchemaSpec{"name": {Type: "string"}, "id": {Type: "string"}}}
	c := ResourceCRUD{
		Create: &Operation{Method: MethodPost, Path: "/widgets", RequestBody: true, RequestSchema: body, ResponseSchema: body,
			Parameters: []Parameter{{Name: "If-None-Match", In: "header", Type: "string"}}},
		Read: &Operation{Method: MethodGet, Path: "/widgets/{id}", ResponseSchema: body},
		Update: &Operation{Method: MethodPut, Path: "/widgets/{id}", RequestBody: true, RequestSchema: body, ResponseSchema: body,
			Parameters: []Parameter{{Name: "If-Match", In: "header", Required: true, Type: "string"}, {Name: "X-Trace-Id", In: "header", Type: "string"}}},
		Delete: &Operation{Method: MethodDelete, Path: "/widgets/{id}",
			Parameters: []Parameter{{Name: "if-match", In: "header", Type: "string"}}},
	}
	schema, _ := ManagedResourceSchema(c)
	seen := map[string]bool{}
	for _, a := range schema.Attributes {
		seen[a.Name] = true
	}
	for _, banned := range []string{"if_match", "if_none_match"} {
		if seen[banned] {
			t.Errorf("schema carries %q; conditional headers must not become attributes", banned)
		}
	}
	if !seen["x_trace_id"] {
		t.Errorf("schema lost the ordinary x_trace_id header attribute: %v", seen)
	}
}
