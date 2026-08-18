package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// wiredFormDataResourceIR returns a wired resource whose Create and Update
// send formData parameters as application/x-www-form-urlencoded instead of a
// JSON body. It exercises the §2 formData wiring: the create/update body
// resolves the formData parameters against the resource schema, builds a
// url.Values, encodes it, and sends it with the form-encoded Content-Type.
// name is a Required formData parameter (always sent); tag is optional. The
// Read/Update/Delete paths carry the {id} placeholder resolved to the Computed
// id attribute.
func wiredFormDataResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	formParams := []ir.ParamIR{
		{Name: "name", In: "formData", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "tag", In: "formData", Required: false, Schema: ir.SchemaIR{Type: ir.TypeString}},
	}
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:         "POST",
		PathTemplate:   "/pets",
		SuccessCodes:   []int{201},
		FormDataParams: formParams,
	}
	update := ir.OperationMappingIR{
		Method:         "PUT",
		PathTemplate:   "/pets/{id}",
		SuccessCodes:   []int{200},
		FormDataParams: formParams,
	}
	r.CRUDMapping.Update = &update
	return r
}

// TestWiredFormDataCreate_Render asserts the generated wired Create body builds
// a url.Values from the formData parameters, encodes it, sends it with the
// application/x-www-form-urlencoded Content-Type via strings.NewReader, and
// imports net/url + strings (and not bytes/encoding/json).
func TestWiredFormDataCreate_Render(t *testing.T) {
	r := wiredFormDataResourceIR()
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true (formData params resolve)")
	}
	if !plan.needsFormBody {
		t.Fatalf("plan.needsFormBody = false, want true (create carries formData)")
	}
	if plan.needsJSONBody {
		t.Fatalf("plan.needsJSONBody = true, want false (create+update both form-encoded)")
	}

	files := ResourceFiles([]ir.ResourceIR{r}, testClientImport)
	if len(files) != 1 {
		t.Fatalf("ResourceFiles() returned %d files, want 1", len(files))
	}
	var buf bytes.Buffer
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		// Form body built from url.Values.
		`form := url.Values{}`,
		`form.Set("name"`,
		`form.Set("tag"`,
		`payload := form.Encode()`,
		// Form body reader.
		`strings.NewReader(payload)`,
		// Content-Type set to form-encoded on the create request.
		`"application/x-www-form-urlencoded"`,
		// No JSON body machinery for the create/update.
		// `modelToJSONMap` must NOT appear (no JSON body in this resource).
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated formData Create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "modelToJSONMap") {
		t.Errorf("formData Create body must not build a JSON body\n--- body ---\n%s", got)
	}
	// Imports: net/url and strings are required; bytes is not (no JSON request
	// body). encoding/json IS still imported because the wired Read decodes the
	// JSON response with json.NewDecoder.
	if !strings.Contains(got, `"net/url"`) {
		t.Errorf("formData resource must import net/url\n--- body ---\n%s", got)
	}
	if strings.Contains(got, `"bytes"`) {
		t.Errorf("formData-only resource must not import bytes\n--- body ---\n%s", got)
	}
}

// TestWiredFormDataCreate_Compiles generates a full provider module with a
// formData-wired resource and compiles it, proving the form-encoded body,
// Content-Type, and net/url/strings imports are syntactically valid.
func TestWiredFormDataCreate_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{wiredFormDataResourceIR()}
	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for formData resource: %v\n%s", err, out)
	}
}

// TestWiredFormDataMixedImports asserts that a resource with a JSON Update and a
// formData Create imports both bytes/encoding/json (for the JSON update) and
// net/url (for the form create) — proving the two body types coexist in one
// file.
func TestWiredFormDataMixedImports(t *testing.T) {
	r := wiredFormDataResourceIR()
	// Make the Update a JSON body (no formData) while the Create stays
	// form-encoded. BodySchema marks the request body present (M-11).
	r.CRUDMapping.Update = &ir.OperationMappingIR{
		Method:       "PUT",
		PathTemplate: "/pets/{id}",
		SuccessCodes: []int{200},
		BodySchema:   &ir.SchemaIR{},
	}
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true")
	}
	if !plan.needsFormBody {
		t.Fatalf("plan.needsFormBody = false, want true (form create)")
	}
	if !plan.needsJSONBody {
		t.Fatalf("plan.needsJSONBody = false, want true (JSON update)")
	}

	files := ResourceFiles([]ir.ResourceIR{r}, testClientImport)
	var buf bytes.Buffer
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		`"bytes"`,
		`"encoding/json"`,
		`"net/url"`,
		`form := url.Values{}`,
		`modelToJSONMap`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mixed body resource missing %q\n--- body ---\n%s", want, got)
		}
	}
}

// TestPlanOperation_FormDataNotResolvableStaysScaffolded asserts that a
// formData parameter which does not resolve to a primitive schema attribute (a
// required param with no matching attribute, or a non-primitive file param)
// disables wiring for the operation, keeping the resource honestly scaffolded
// rather than wiring a partial form body.
func TestPlanOperation_FormDataNotResolvableStaysScaffolded(t *testing.T) {
	t.Run("required unmapped formData", func(t *testing.T) {
		r := sampleResourceIR()
		r.CRUDMapping.Create = ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/pets",
			FormDataParams: []ir.ParamIR{
				{Name: "unknown", In: "formData", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			},
		}
		if planResourceWiring(r).wired {
			t.Fatalf("create with a required unmapped formData param must not wire")
		}
	})

	t.Run("non-primitive formData", func(t *testing.T) {
		r := sampleResourceIR()
		// Add a Dynamic-typed "blob" attribute that matches the formData param
		// by name, so the failure is specifically the non-primitive type, not a
		// name miss.
		r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
			Name: "blob", Schema: ir.SchemaIR{Type: ir.TypeDynamic}, Optional: true,
		})
		r.CRUDMapping.Create = ir.OperationMappingIR{
			Method:       "POST",
			PathTemplate: "/pets",
			FormDataParams: []ir.ParamIR{
				{Name: "blob", In: "formData", Required: false, Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			},
		}
		if planResourceWiring(r).wired {
			t.Fatalf("create with a non-primitive formData param must not wire")
		}
	})
}
