package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderDocs renders a docs File to a string for substring assertions.
func renderDocs(t *testing.T, f File) string {
	t.Helper()
	var buf bytes.Buffer
	if err := f.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

// TestResourceDocsNotes_WiredResourceHasNoNotes locks in §3.4: a fully wired
// resource's doc page carries no admonition, so the note only appears where
// the honest-scaffold invariant applies.
func TestResourceDocsNotes_WiredResourceHasNoNotes(t *testing.T) {
	if notes := resourceDocsNotes(sampleResourceIR()); len(notes) != 0 {
		t.Errorf("resourceDocsNotes(wired) = %v, want no notes", notes)
	}
}

// TestResourceDocsNotes_UpdateNotWired locks in the distinct Update-scaffold
// note: a resource wired for create/read/delete but without a usable update
// mapping documents that in-place changes fail and the resource must be
// replaced.
func TestResourceDocsNotes_UpdateNotWired(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Update = nil

	notes := resourceDocsNotes(r)
	if len(notes) != 1 || !strings.Contains(notes[0], "update operation for this resource is not wired") {
		t.Fatalf("resourceDocsNotes(update scaffold) = %v, want the update-not-wired note", notes)
	}

	got := renderDocs(t, ResourceDocsFile(r))
	if !strings.Contains(got, "update operation for this resource is not wired") {
		t.Errorf("resource docs missing the update-not-wired note\ncontent:\n%s", got)
	}
	// The wired CRUD bodies keep their docs sections (import, schema).
	if !strings.Contains(got, "## Import") {
		t.Errorf("wired resource docs must keep the import section\ncontent:\n%s", got)
	}
}

// TestResourceDocsNotes_FullyScaffolded locks in the not-wired note for a
// resource whose CRUD mapping does not resolve at all: the doc page must say
// so, not look fully functional.
func TestResourceDocsNotes_FullyScaffolded(t *testing.T) {
	r := sampleResourceIR()
	r.CRUDMapping.Create = ir.OperationMappingIR{}

	notes := resourceDocsNotes(r)
	if len(notes) != 1 || !strings.Contains(notes[0], "This resource is not yet wired") {
		t.Fatalf("resourceDocsNotes(scaffold) = %v, want the not-wired note", notes)
	}

	got := renderDocs(t, ResourceDocsFile(r))
	if !strings.Contains(got, "This resource is not yet wired") {
		t.Errorf("resource docs missing the not-wired note\ncontent:\n%s", got)
	}
}

// TestDataSourceDocsNotes locks in the not-wired note for a data source whose
// Read keeps the honest scaffold (the sample has no read mapping at all).
func TestDataSourceDocsNotes(t *testing.T) {
	notes := dataSourceDocsNotes(sampleDataSourceIR())
	if len(notes) != 1 || !strings.Contains(notes[0], "This data source is not yet wired") {
		t.Fatalf("dataSourceDocsNotes(scaffold) = %v, want the not-wired note", notes)
	}

	got := renderDocs(t, DataSourceDocsFile(sampleDataSourceIR()))
	if !strings.Contains(got, "This data source is not yet wired") {
		t.Errorf("data source docs missing the not-wired note\ncontent:\n%s", got)
	}
}

// TestActionDocsNotes_TFVersionAndNotWired locks in both action notes: every
// action page states the Terraform 1.14 requirement, and an unwired action
// additionally carries the not-wired note.
func TestActionDocsNotes_TFVersionAndNotWired(t *testing.T) {
	wired := sampleActionIR()
	notes := actionDocsNotes(wired)
	if len(notes) != 1 || !strings.Contains(notes[0], "requires Terraform 1.14 or later") {
		t.Fatalf("actionDocsNotes(wired) = %v, want only the TF-version note", notes)
	}

	unwired := sampleActionIR()
	unwired.ModifyPlanMapping = nil
	unwired.InvokeMapping = ir.OperationMappingIR{Method: "POST", PathTemplate: "/servers/{server_id}/reboot"}
	unwired.ConfigSchema = ir.ObjectSchemaIR{}
	notes = actionDocsNotes(unwired)
	if len(notes) != 2 ||
		!strings.Contains(notes[0], "requires Terraform 1.14 or later") ||
		!strings.Contains(notes[1], "This action is not yet wired") {
		t.Fatalf("actionDocsNotes(scaffold) = %v, want TF-version + not-wired notes", notes)
	}

	got := renderDocs(t, ActionDocsFile(unwired))
	if !strings.Contains(got, "requires Terraform 1.14 or later") ||
		!strings.Contains(got, "This action is not yet wired") {
		t.Errorf("action docs missing notes\ncontent:\n%s", got)
	}
}

// TestEphemeralResourceDocsNotes locks in the not-wired note for an ephemeral
// resource with no result output (its Open keeps the honest scaffold); the
// sample with a result schema and no unreferenced required inputs carries no
// note.
func TestEphemeralResourceDocsNotes(t *testing.T) {
	wired := sampleEphemeralResourceIR()
	// The sample's required `duration` is not referenced by the Open mapping,
	// which keeps the scaffold (a wired bodiless Open would drop it). Make it
	// optional so the Open resolves and the resource wires.
	wired.ConfigSchema.Attributes[0].Required = false
	wired.ConfigSchema.Attributes[0].Optional = true
	if notes := ephemeralResourceDocsNotes(wired); len(notes) != 0 {
		t.Errorf("ephemeralResourceDocsNotes(wired) = %v, want no notes", notes)
	}

	unwired := sampleEphemeralResourceIR()
	unwired.ResultSchema = ir.ObjectSchemaIR{}
	notes := ephemeralResourceDocsNotes(unwired)
	if len(notes) != 1 || !strings.Contains(notes[0], "This ephemeral resource is not yet wired") {
		t.Fatalf("ephemeralResourceDocsNotes(scaffold) = %v, want the not-wired note", notes)
	}
}

// TestListResourceDocsNotes_TFVersion locks in the list-resource note: every
// documented list resource states the Terraform 1.14 / `terraform query`
// requirement.
func TestListResourceDocsNotes_TFVersion(t *testing.T) {
	notes := listResourceDocsNotes(sampleListResourceIR())
	if len(notes) != 1 || !strings.Contains(notes[0], "requires Terraform 1.14 or later") {
		t.Fatalf("listResourceDocsNotes() = %v, want the TF-version note", notes)
	}

	got := renderDocs(t, ListResourceDocsFile(sampleListResourceIR()))
	if !strings.Contains(got, "requires Terraform 1.14 or later") {
		t.Errorf("list resource docs missing the TF-version note\ncontent:\n%s", got)
	}
}

// TestFunctionDocsNotes_NotWired locks in that every function page carries the
// not-wired note: eidos never wires provider-defined functions (F4).
func TestFunctionDocsNotes_NotWired(t *testing.T) {
	got := renderDocs(t, FunctionDocsFile(sampleFunctionIR(), "mycloud"))
	if !strings.Contains(got, "This function is not yet wired") {
		t.Errorf("function docs missing the not-wired note\ncontent:\n%s", got)
	}
}

// TestMinTerraformVersionForIR locks in §3.10: the README's stated minimum
// Terraform version rises to the highest CLI version the provider's construct
// mix requires instead of always claiming 1.0.
func TestMinTerraformVersionForIR(t *testing.T) {
	cases := []struct {
		name     string
		provider *ir.ProviderIR
		want     string
	}{
		{name: "nil provider", provider: nil, want: DefaultTerraformVersion},
		{
			name:     "resources only",
			provider: &ir.ProviderIR{Resources: []ir.ResourceIR{{Name: "pet"}}},
			want:     DefaultTerraformVersion,
		},
		{
			name: "ephemerals raise to 1.10",
			provider: &ir.ProviderIR{
				Resources:          []ir.ResourceIR{{Name: "pet"}},
				EphemeralResources: []ir.EphemeralResourceIR{{Name: "credential"}},
			},
			want: "1.10",
		},
		{
			name: "actions raise to 1.14",
			provider: &ir.ProviderIR{
				Resources: []ir.ResourceIR{{Name: "pet"}},
				Actions:   []ir.ActionIR{{Name: "reboot"}},
			},
			want: "1.14",
		},
		{
			name: "list resources raise to 1.14",
			provider: &ir.ProviderIR{
				Resources:     []ir.ResourceIR{{Name: "pet"}},
				ListResources: []ir.ListResourceIR{{Name: "pets"}},
			},
			want: "1.14",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := minTerraformVersionForIR(tc.provider); got != tc.want {
				t.Errorf("minTerraformVersionForIR() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReadmeStatesTerraformVersion verifies the README renders the derived
// minimum Terraform version in its Requirements section.
func TestReadmeStatesTerraformVersion(t *testing.T) {
	got := renderDocs(t, Readme(BuildConfigFromIR(&ir.ProviderIR{
		Name:     "mycloud",
		TypeName: "mycloud",
		Actions:  []ir.ActionIR{{Name: "reboot"}},
	})))
	if !strings.Contains(got, "[Terraform](https://www.terraform.io/downloads.html) >= 1.14") {
		t.Errorf("README missing \">= 1.14\" requirement\ncontent:\n%s", got)
	}
}

// TestDocsUnmarkableSecretNote covers the admonition for secret-named
// attributes that the action/list schema kinds cannot mark Sensitive: names
// are listed, an empty attribute list renders no note (§3.6).
func TestDocsUnmarkableSecretNote(t *testing.T) {
	if got := docsUnmarkableSecretNote("action", nil); got != "" {
		t.Errorf("docsUnmarkableSecretNote(nil) = %q, want empty", got)
	}
	got := docsUnmarkableSecretNote("action", []string{"password", "api_key"})
	for _, want := range []string{"password", "api_key", "cannot mark attributes Sensitive", "Warning"} {
		if !strings.Contains(got, want) {
			t.Errorf("note missing %q:\n%s", want, got)
		}
	}
}

// TestActionDocsNotes_UnmarkableSecret verifies an action carrying a
// secret-named attribute (as recorded at transform time) documents the
// plain-text limitation on its doc page.
func TestActionDocsNotes_UnmarkableSecret(t *testing.T) {
	a := sampleActionIR()
	a.UnmarkableSensitiveAttrs = []string{"api_key"}
	notes := actionDocsNotes(a)
	found := false
	for _, n := range notes {
		if strings.Contains(n, "cannot mark attributes Sensitive") {
			found = true
		}
	}
	if !found {
		t.Errorf("actionDocsNotes() missing the unmarkable-secret note: %v", notes)
	}
}

// TestListResourceDocsNotes_UnmarkableSecret verifies a list resource carrying
// a secret-named identity attribute documents the plain-text limitation on
// its doc page.
func TestListResourceDocsNotes_UnmarkableSecret(t *testing.T) {
	lr := sampleListResourceIR()
	lr.UnmarkableSensitiveAttrs = []string{"token"}
	notes := listResourceDocsNotes(lr)
	found := false
	for _, n := range notes {
		if strings.Contains(n, "cannot mark attributes Sensitive") {
			found = true
		}
	}
	if !found {
		t.Errorf("listResourceDocsNotes() missing the unmarkable-secret note: %v", notes)
	}
}
