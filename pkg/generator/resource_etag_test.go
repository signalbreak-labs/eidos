package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// etagResourceIR returns the sample resource with a required If-Match on
// update (whose spec documents 412), an optional If-Match on delete, and
// If-None-Match on create.
func etagResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.CRUDMapping.Create.IfNoneMatch = ir.ConditionalHeaderOptional
	r.CRUDMapping.Update.IfMatch = ir.ConditionalHeaderRequired
	r.CRUDMapping.Update.ErrorMappings = map[int]ir.ErrorMappingIR{412: {StatusCode: 412, Description: "Edited by someone else (50% chance)"}}
	r.CRUDMapping.Delete.IfMatch = ir.ConditionalHeaderOptional
	return r
}

func renderResource(t *testing.T, r ir.ResourceIR) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ResourceFile(r, testClientImport).Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

func TestPlanResourceWiring_ETag(t *testing.T) {
	plan := planResourceWiring(etagResourceIR())
	if !plan.wired || !plan.trackETag {
		t.Fatalf("plan wired=%v trackETag=%v, want both true", plan.wired, plan.trackETag)
	}
	if !plan.create.ifNoneMatch || !plan.create.captureETag || !plan.create.etagFromHeader {
		t.Errorf("create plan = %+v; want If-None-Match and header ETag capture", plan.create)
	}
	if !plan.read.captureETag || !plan.read.etagFromHeader {
		t.Errorf("read plan should capture the header ETag")
	}
	u := plan.updateOp
	if !u.ifMatch || !u.ifMatchParam || !u.ifMatchRequired || !u.captureETag {
		t.Errorf("update plan = %+v; want a required, tracked If-Match", u)
	}
	if plan.delete.ifMatch || plan.delete.ifMatchParam {
		t.Errorf("optional delete If-Match must not be sent by default")
	}
	if got := u.errorMappings[412]; !strings.HasPrefix(got, etagUpdateConflictDetail) || !strings.Contains(got, "(API: Edited by someone else (50%% chance))") {
		t.Errorf("update 412 mapping = %q; want the provider explanation plus the escaped spec description", got)
	}
	if u.errorMappings[428] != etagRequiredDetail {
		t.Errorf("update 428 mapping = %q", u.errorMappings[428])
	}
	if plan.create.errorMappings[412] != etagCreateConflictDetail {
		t.Errorf("create 412 mapping = %q", plan.create.errorMappings[412])
	}
	if _, ok := plan.delete.errorMappings[412]; ok {
		t.Errorf("a delete that sends no If-Match must not map 412")
	}
}

func TestPlanResourceWiring_ETagVariants(t *testing.T) {
	t.Run("conditional delete always", func(t *testing.T) {
		r := etagResourceIR()
		r.ConditionalDelete = ir.ConditionalDeleteAlways
		plan := planResourceWiring(r)
		if !plan.delete.ifMatch || !plan.delete.ifMatchParam || plan.delete.ifMatchRequired {
			t.Fatalf("delete plan = %+v; want an optional tracked If-Match", plan.delete)
		}
	})
	t.Run("collection read takes no header ETag", func(t *testing.T) {
		r := etagResourceIR()
		r.CRUDMapping.Read.ResponseIsCollection = true
		if plan := planResourceWiring(r); !plan.read.captureETag || plan.read.etagFromHeader {
			t.Fatalf("read plan = %+v; a collection GET's ETag header describes the collection", plan.read)
		}
	})
	t.Run("tracking disabled", func(t *testing.T) {
		r := etagResourceIR()
		r.ETagTrackingDisabled = true
		plan := planResourceWiring(r)
		if plan.trackETag || plan.updateOp.ifMatchParam || plan.updateOp.captureETag || plan.read.captureETag {
			t.Fatalf("plan tracks ETags despite etag_tracking: false: %+v", plan)
		}
		if !plan.updateOp.ifMatch || !plan.updateOp.ifMatchRequired {
			t.Fatalf("required update If-Match must still be sent as *")
		}
		if AnyResourceTracksETag([]ir.ResourceIR{r}) {
			t.Fatalf("AnyResourceTracksETag must be false without tracking")
		}
	})
	t.Run("update not wired, delete only", func(t *testing.T) {
		r := etagResourceIR()
		r.CRUDMapping.Update = nil
		r.CRUDMapping.Delete.IfMatch = ir.ConditionalHeaderRequired
		plan := planResourceWiring(r)
		if !plan.trackETag || !plan.delete.ifMatchParam || plan.update {
			t.Fatalf("plan = %+v; want tracking for the required delete", plan)
		}
	})
	t.Run("update If-Match only tracked when update wired", func(t *testing.T) {
		r := etagResourceIR()
		r.CRUDMapping.Update.Method = "" // unwireable update
		plan := planResourceWiring(r)
		if plan.trackETag {
			t.Fatalf("an unwired update cannot send an ETag, so nothing should be tracked")
		}
	})
	t.Run("no conditional headers", func(t *testing.T) {
		plan := planResourceWiring(sampleResourceIR())
		if plan.trackETag || plan.create.ifNoneMatch || plan.updateOp.ifMatch || plan.delete.ifMatch {
			t.Fatalf("plain resource picked up conditional behavior: %+v", plan)
		}
		if AnyResourceTracksETag([]ir.ResourceIR{sampleResourceIR()}) {
			t.Fatalf("AnyResourceTracksETag must be false for a plain resource")
		}
	})
}

func TestResourceFile_ETagRender(t *testing.T) {
	got := renderResource(t, etagResourceIR())
	for _, want := range []string{
		`etag := r.createRemote(ctx, &plan, resp)`,
		`func (r *PetResource) createRemote(ctx context.Context, plan *PetResourceModel, resp *resource.CreateResponse) (etag string) {`,
		`httpReq.Header.Set("If-None-Match", "*")`,
		`removed, etag := r.readRemote(ctx, &state, resp)`,
		`(removed bool, etag string)`,
		`ifMatch, etagDiags := privateETag(ctx, req.Private)`,
		`etag := r.updateRemote(ctx, &plan, ifMatch, resp)`,
		`ifMatch string, resp *resource.UpdateResponse) (etag string) {`,
		"if ifMatch != \"\" {\n\t\thttpReq.Header.Set(\"If-Match\", ifMatch)\n\t} else {\n\t\thttpReq.Header.Set(\"If-Match\", \"*\")\n\t}",
		`etag = responseETag(httpResp)`,
		`resp.Diagnostics.Append(setPrivateETag(ctx, resp.Private, etag)...)`,
		`case 412:`,
		`case 428:`,
		`r.deleteRemote(ctx, &state, resp)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered resource missing %q", want)
		}
	}
	// The optional delete If-Match is not sent by default.
	i := strings.Index(got, "func (r *PetResource) deleteRemote")
	if i < 0 {
		t.Fatalf("rendered resource has no deleteRemote")
	}
	del := got[i:]
	if strings.Contains(del, "If-Match") {
		t.Errorf("deleteRemote sends If-Match although the header is optional")
	}
}

func TestResourceFile_ETagTrackingDisabledRender(t *testing.T) {
	r := etagResourceIR()
	r.ETagTrackingDisabled = true
	got := renderResource(t, r)
	if !strings.Contains(got, `httpReq.Header.Set("If-Match", "*")`) {
		t.Errorf("required If-Match should be sent as * without tracking")
	}
	for _, banned := range []string{"privateETag", "setPrivateETag", "responseETag", "ifMatch string"} {
		if strings.Contains(got, banned) {
			t.Errorf("tracking disabled but rendered %q", banned)
		}
	}
}

func TestResourceFile_NoETagUnchanged(t *testing.T) {
	got := renderResource(t, sampleResourceIR())
	for _, banned := range []string{"etag", "If-Match", "If-None-Match", "Private"} {
		if strings.Contains(got, banned) {
			t.Errorf("plain resource rendered %q", banned)
		}
	}
	if !strings.Contains(got, `if r.readRemote(ctx, &state, resp) {`) {
		t.Errorf("plain resource read call shape changed")
	}
}

func TestBodyETagField(t *testing.T) {
	cases := []struct {
		attr      ir.AttributeIR
		wantField string
		wantWire  string
	}{
		{ir.AttributeIR{Name: "etag", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}}, "Etag", "etag"},
		{ir.AttributeIR{Name: "etag_", WireName: "_etag", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}}, "Etag", "_etag"},
		{ir.AttributeIR{Name: "odata_etag", WireName: "@odata.etag", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}}, "OdataEtag", "@odata.etag"},
		{ir.AttributeIR{Name: "etag", Computed: true, Schema: ir.SchemaIR{Type: ir.TypeInt}}, "", ""},
		{ir.AttributeIR{Name: "etag", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString}}, "", ""},
		{ir.AttributeIR{Name: "etag", WirePath: "meta.etag", Schema: ir.SchemaIR{Type: ir.TypeString}}, "", ""},
		{ir.AttributeIR{Name: "version", Schema: ir.SchemaIR{Type: ir.TypeString}}, "", ""},
	}
	for _, tc := range cases {
		r := ir.ResourceIR{Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{tc.attr}}}
		field, wire := bodyETagField(r)
		if tc.wantField != "" && field == "" || tc.wantField == "" && field != "" || wire != tc.wantWire {
			t.Errorf("bodyETagField(%+v) = (%q, %q), want (%q, %q)", tc.attr, field, wire, tc.wantField, tc.wantWire)
		}
	}
}

func TestETagHelpersFile_Renders(t *testing.T) {
	for _, f := range []File{ETagHelpersFile(), ETagHelpersTestFile()} {
		var buf bytes.Buffer
		if err := f.Render(&buf); err != nil {
			t.Fatalf("%s: Render() error = %v", f.Path, err)
		}
		if !strings.HasPrefix(buf.String(), "package provider") {
			t.Fatalf("%s: unexpected output %q", f.Path, buf.String()[:40])
		}
	}
}

func TestConvertResources_ConditionalOverridesRoundTrip(t *testing.T) {
	r := etagResourceIR()
	r.ConditionalDelete = ir.ConditionalDeleteAlways
	r.ETagTrackingDisabled = true
	cfg, err := GenerateConfig(ir.ProviderIR{Name: "mycloud", Resources: []ir.ResourceIR{r, sampleResourceIR()}})
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	ro := cfg.ResourceOverrides[0]
	if ro.ConditionalDelete != "always" || ro.ETagTracking == nil || *ro.ETagTracking {
		t.Fatalf("override = %+v; want conditional_delete always and etag_tracking false", ro)
	}
	if plain := cfg.ResourceOverrides[1]; plain.ConditionalDelete != "" || plain.ETagTracking != nil {
		t.Fatalf("plain resource re-emitted conditional overrides: %+v", plain)
	}
}

func TestResourceDocsNotes_ConditionalRequests(t *testing.T) {
	notes := strings.Join(resourceDocsNotes(etagResourceIR()), "\n")
	for _, want := range []string{"optimistic concurrency", "`If-Match` on updates,", "If-None-Match: *", "terraform apply -refresh-only"} {
		if !strings.Contains(notes, want) {
			t.Errorf("docs notes %q missing %q", notes, want)
		}
	}
	r := etagResourceIR()
	r.ETagTrackingDisabled = true
	if notes := strings.Join(resourceDocsNotes(r), "\n"); !strings.Contains(notes, "sends `If-Match: *`") {
		t.Errorf("tracking-disabled note = %q", notes)
	}
	if notes := resourceDocsNotes(sampleResourceIR()); len(notes) != 0 {
		t.Errorf("plain resource got notes %v", notes)
	}
}
