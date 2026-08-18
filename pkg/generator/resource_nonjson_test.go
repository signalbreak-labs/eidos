package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// wiredMultipartResourceIR returns a wired resource whose Create sends a
// multipart/form-data body: a binary file upload (format: binary, read from a
// configured path) plus a text field. It exercises the A2 multipart wiring:
// planOperation selects bodyMultipart from OperationMappingIR.MediaType, the
// body builder writes file and text parts via mime/multipart.NewWriter, and
// the request sets the dynamic Content-Type to writer.FormDataContentType().
func wiredMultipartResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	// Add a string "file" attribute holding the upload path; the binary
	// formData param resolves against it (format: binary marks it as a file
	// part). Optional: a binary upload path need not be set on every instance.
	r.Schema.Attributes = append(r.Schema.Attributes, ir.AttributeIR{
		Name:     "file",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})
	formParams := []ir.ParamIR{
		{Name: "name", In: "formData", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "file", In: "formData", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString, Format: "binary"}},
	}
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:         "POST",
		PathTemplate:   "/pets",
		SuccessCodes:   []int{201},
		MediaType:      "multipart/form-data",
		FormDataParams: formParams,
	}
	// Mirror the create: a multipart update (PUT /pets/{id}) so the whole
	// resource is multipart-only and the rendered file contains no JSON body
	// machinery (modelToJSONMap/json.Marshal), matching the formData test's
	// clean single-encoding shape.
	update := ir.OperationMappingIR{
		Method:         "PUT",
		PathTemplate:   "/pets/{id}",
		SuccessCodes:   []int{200},
		MediaType:      "multipart/form-data",
		FormDataParams: formParams,
	}
	r.CRUDMapping.Update = &update
	return r
}

// wiredXMLResourceIR returns a wired resource whose Create sends an
// application/xml body. It exercises the A2 XML wiring: planOperation selects
// bodyXML from OperationMappingIR.MediaType and the body builder encodes the
// model via modelToJSONMap + mapToXML wrapped in the resource's root element.
func wiredXMLResourceIR() ir.ResourceIR {
	r := sampleResourceIR()
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/pets",
		SuccessCodes: []int{201},
		MediaType:    "application/xml",
		// BodySchema is the presence marker for a request body (M-11): the
		// generator encodes a body only when it is non-nil, exactly as the
		// transformer emits it for a real body-bearing operation.
		BodySchema: &ir.SchemaIR{},
	}
	// Mirror the create: an XML update so the whole resource is XML-only and
	// the rendered file contains no JSON body machinery.
	update := ir.OperationMappingIR{
		Method:       "PUT",
		PathTemplate: "/pets/{id}",
		SuccessCodes: []int{200},
		MediaType:    "application/xml",
		BodySchema:   &ir.SchemaIR{},
	}
	r.CRUDMapping.Update = &update
	return r
}

// TestWiredMultipartFileUpload_Render asserts the generated wired Create body
// builds a multipart/form-data body via mime/multipart.NewWriter: it opens the
// configured upload path, copies it into a CreateFormFile part, writes a text
// field, and sets the dynamic Content-Type to writer.FormDataContentType(). It
// imports mime/multipart, os, path/filepath, and bytes (for the body buffer),
// and does not build a JSON body.
func TestWiredMultipartFileUpload_Render(t *testing.T) {
	r := wiredMultipartResourceIR()
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true (multipart formData params resolve)")
	}
	if !plan.needsMultipartBody {
		t.Fatalf("plan.needsMultipartBody = false, want true")
	}
	if !plan.needsMultipartFile {
		t.Fatalf("plan.needsMultipartFile = false, want true (binary file param present)")
	}
	if plan.needsJSONBody {
		t.Fatalf("plan.needsJSONBody = true, want false (multipart create is not JSON)")
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
		`var buf bytes.Buffer`,
		`formWriter := multipart.NewWriter(&buf)`,
		// Binary file part: open the configured path, create a file part.
		`os.Open(`,
		`formWriter.CreateFormFile("file"`,
		`filepath.Base(file.Name())`,
		`io.Copy(part, file)`,
		`file.Close()`,
		// Text field.
		`formWriter.WriteField("name"`,
		`formWriter.Close()`,
		// Dynamic Content-Type carries the generated boundary.
		`formWriter.FormDataContentType()`,
		// Body reader is the multipart buffer.
		`&buf`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated multipart Create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	for _, banned := range []string{
		`modelToJSONMap`, // multipart create does not build a JSON body
		`json.Marshal`,   // no JSON marshaling for the create body
	} {
		if strings.Contains(got, banned) {
			t.Errorf("multipart Create body must not contain %q\n--- body ---\n%s", banned, got)
		}
	}
	// Imports: mime/multipart, os, path/filepath, bytes are required for the
	// multipart body. encoding/json is still imported because the wired Read
	// decodes the JSON response with json.NewDecoder.
	for _, imp := range []string{
		`"mime/multipart"`,
		`"os"`,
		`"path/filepath"`,
		`"bytes"`,
	} {
		if !strings.Contains(got, imp) {
			t.Errorf("multipart resource must import %s\n--- body ---\n%s", imp, got)
		}
	}
}

// TestWiredMultipartFileUpload_Compiles generates a full provider module with a
// multipart-wired resource and compiles it, proving the multipart body, file
// part, dynamic Content-Type, and imports are syntactically valid.
func TestWiredMultipartFileUpload_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{wiredMultipartResourceIR()}
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
		t.Fatalf("go build ./... failed for multipart resource: %v\n%s", err, out)
	}
}

// TestWiredXMLBody_Render asserts the generated wired Create body encodes an
// application/xml body: it converts the model to a JSON-ready map, encodes it
// with mapToXML wrapped in the resource root element ("pet"), and sends it with
// the application/xml Content-Type via bytes.NewReader. It imports bytes (for
// the body reader) and does not json.Marshal the body.
func TestWiredXMLBody_Render(t *testing.T) {
	r := wiredXMLResourceIR()
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true (XML create resolves)")
	}
	if !plan.needsXMLBody {
		t.Fatalf("plan.needsXMLBody = false, want true")
	}
	if plan.needsJSONBody {
		t.Fatalf("plan.needsJSONBody = true, want false (XML create is not JSON)")
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
		// Build the model map, then encode as XML wrapped in the root element.
		`body, err := modelToJSONMap(&plan)`,
		`payload, err := mapToXML(body, "pet")`,
		`bytes.NewReader(payload)`,
		// XML Content-Type.
		`"application/xml"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated XML Create body missing %q\n--- body ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "json.Marshal") {
		t.Errorf("XML Create body must not json.Marshal the body\n--- body ---\n%s", got)
	}
	// The XML path needs bytes for the body reader.
	if !strings.Contains(got, `"bytes"`) {
		t.Errorf("XML resource must import bytes\n--- body ---\n%s", got)
	}
}

// TestJSONConvertFile_XMLGating asserts the N-37 contract: json_convert.go
// carries the XML serialization helpers (mapToXML and friends) and their
// supporting imports (bytes, encoding/xml, sort) only when at least one wired
// resource CRUD body serializes application/xml. JSON-only providers must not
// ship dead XML serialization code, and XML providers must keep the helpers so
// their wired bodies compile.
func TestJSONConvertFile_XMLGating(t *testing.T) {
	render := func(p *ir.ProviderIR) string {
		f := JSONConvertFile(p)
		var buf bytes.Buffer
		if err := f.Render(&buf); err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		return buf.String()
	}

	xmlImports := []string{`"bytes"`, `"encoding/xml"`, `"sort"`}
	xmlHelpers := []string{"mapToXML", "writeXMLElement", "writeXMLValue"}

	// A JSON-only provider (the sample resource has no XML body) omits them.
	jsonProvider := sampleProviderWithResourceIR()
	if AnyResourceXMLBody(jsonProvider.Resources) {
		t.Fatal("AnyResourceXMLBody(sample provider) = true, want false")
	}
	got := render(&jsonProvider)
	for _, want := range xmlImports {
		if strings.Contains(got, want) {
			t.Errorf("JSON-only json_convert.go must not import %s\n--- body ---\n%s", want, got)
		}
	}
	for _, want := range xmlHelpers {
		if strings.Contains(got, want) {
			t.Errorf("JSON-only json_convert.go must not emit %s\n--- body ---\n%s", want, got)
		}
	}

	// An XML-wired provider includes them.
	xmlProvider := sampleProviderWithResourceIR()
	xmlProvider.Resources = []ir.ResourceIR{wiredXMLResourceIR()}
	if !AnyResourceXMLBody(xmlProvider.Resources) {
		t.Fatal("AnyResourceXMLBody(xml provider) = false, want true")
	}
	got = render(&xmlProvider)
	for _, want := range xmlImports {
		if !strings.Contains(got, want) {
			t.Errorf("XML json_convert.go must import %s\n--- body ---\n%s", want, got)
		}
	}
	for _, want := range xmlHelpers {
		if !strings.Contains(got, want) {
			t.Errorf("XML json_convert.go must emit %s\n--- body ---\n%s", want, got)
		}
	}
}

// TestWiredXMLBody_Compiles generates a full provider module with an XML-wired
// resource and compiles it, proving the mapToXML body and Content-Type are
// syntactically valid. mapToXML lives in the shared json_convert.go helper
// (resourceModuleFiles includes it whenever a resource is wired).
func TestWiredXMLBody_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{wiredXMLResourceIR()}
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
		t.Fatalf("go build ./... failed for XML resource: %v\n%s", err, out)
	}
}

// TestPlanOperation_UnknownMediaTypeScaffolds asserts that a body-bearing
// operation whose request media type the generator cannot encode (here
// application/octet-stream) stays honestly scaffolded rather than silently
// emitting a JSON body. RequestBodyKind classifies it "unsupported" and
// planOperation returns not-wired; the transformer is the fail-loud signal
// (it warns), and the generator keeps the scaffold (A2).
func TestPlanOperation_UnknownMediaTypeScaffolds(t *testing.T) {
	if got := transformer.RequestBodyKind("application/octet-stream"); got != "unsupported" {
		t.Fatalf("RequestBodyKind(application/octet-stream) = %q, want %q", got, "unsupported")
	}
	r := sampleResourceIR()
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:       "POST",
		PathTemplate: "/pets",
		SuccessCodes: []int{201},
		MediaType:    "application/octet-stream",
		// A real body-bearing operation carries the BodySchema presence marker;
		// the generator must not silently encode an unsupported media type.
		BodySchema: &ir.SchemaIR{},
	}
	if planResourceWiring(r).wired {
		t.Fatalf("create with an unsupported media type must not wire (honest scaffold)")
	}
}

// TestBodilessCreateWiresWithoutBody asserts the M-11 fix: a body-bearing
// method (POST/PUT/PATCH) with NO request body (BodySchema nil, no formData
// params) wires as a bodiless request — it does NOT serialize the entire plan
// model as JSON to an endpoint expecting no body. The plan must not claim a
// JSON body and the rendered Create body must contain no JSON-body machinery
// and send a nil body reader.
func TestBodilessCreateWiresWithoutBody(t *testing.T) {
	r := sampleResourceIR()
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true (bodiless POST create resolves)")
	}
	if plan.needsJSONBody {
		t.Fatalf("plan.needsJSONBody = true, want false (no request body declared)")
	}
	if plan.create.hasBody {
		t.Fatalf("plan.create.hasBody = true, want false (no BodySchema, no formData)")
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
	for _, banned := range []string{
		`modelToJSONMap`, // bodiless create must not build a JSON body
		`json.Marshal`,   // no JSON marshaling for a bodiless create
	} {
		if strings.Contains(got, banned) {
			t.Errorf("bodiless Create body must not contain %q\n--- body ---\n%s", banned, got)
		}
	}
}

// TestRequestBodyKind_EmptyDefaultsJSON asserts that an absent media type
// defaults to JSON wiring, preserving pre-A2 behavior for synthetic IRs and
// operations whose request body content was not surfaced (A2).
func TestRequestBodyKind_EmptyDefaultsJSON(t *testing.T) {
	if got := transformer.RequestBodyKind(""); got != "json" {
		t.Fatalf("RequestBodyKind(\"\") = %q, want %q", got, "json")
	}
}
