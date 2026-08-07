package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// applySecurityToCRUD returns a sample resource whose Create carries the given
// per-operation security requirements, exercising the §1 per-operation AND/OR
// resolution. Read/Update/Delete declare no security (they inherit the global
// default). The resource stays fully wired because name resolves the /pets
// create body and {id} resolves via the id-attribute fallback.
func applySecurityToCRUD(reqs []map[string][]string) ir.ResourceIR {
	r := sampleResourceIR()
	r.CRUDMapping.Create = ir.OperationMappingIR{
		Method:               "POST",
		PathTemplate:         "/pets",
		SuccessCodes:         []int{201},
		SecurityRequirements: reqs,
	}
	return r
}

// TestPerOpSecurity_ANDResolvesSchemes asserts that an operation declaring
// exactly one security requirement (an AND set) emits a baked
// client.WithSchemes(<sorted names>...) request option on the wired create
// request, so only that requirement's scheme interceptors apply
// (REMAINING_GAPS §1).
func TestPerOpSecurity_ANDResolvesSchemes(t *testing.T) {
	r := applySecurityToCRUD([]map[string][]string{{
		"basic":  nil,
		"apiKey": nil,
	}})
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true")
	}
	if !plan.create.securitySchemesSet {
		t.Fatalf("create.securitySchemesSet = false, want true (single requirement)")
	}
	wantSchemes := []string{"apiKey", "basic"} // sorted at generation time
	if !equalStringSlices(plan.create.securitySchemes, wantSchemes) {
		t.Fatalf("create.securitySchemes = %v, want %v (sorted)", plan.create.securitySchemes, wantSchemes)
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
	// The baked WithSchemes call appears with the names sorted, as literal args.
	if !strings.Contains(got, `client.WithSchemes("apiKey", "basic")`) {
		t.Errorf("create body missing client.WithSchemes(\"apiKey\", \"basic\")\n--- body ---\n%s", got)
	}
	// Read/Delete declare no security → no WithSchemes on their requests.
	reads := strings.Count(got, "WithSchemes")
	if reads != 1 {
		t.Errorf("WithSchemes occurrences = %d, want 1 (only the create carries per-op security)", reads)
	}
}

// TestPerOpSecurity_NoSecurityInheritsGlobal asserts that an operation
// declaring no security emits no WithSchemes, so NewRequest applies every
// configured scheme interceptor (the global default).
func TestPerOpSecurity_NoSecurityInheritsGlobal(t *testing.T) {
	r := applySecurityToCRUD(nil)
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true")
	}
	if plan.create.securitySchemesSet {
		t.Fatalf("create.securitySchemesSet = true, want false (no per-op security → inherit global)")
	}
	files := ResourceFiles([]ir.ResourceIR{r}, testClientImport)
	var buf bytes.Buffer
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(buf.String(), "WithSchemes") {
		t.Errorf("create body with no per-op security must not emit WithSchemes\n--- body ---\n%s", buf.String())
	}
}

// TestPerOpSecurity_ORMoreThanOneInheritsGlobal asserts that an operation
// declaring more than one requirement (OR) is ambiguous for a non-interactive
// provider and inherits the global default (no WithSchemes); the transformer
// warns via warnPerOpORSecurity.
func TestPerOpSecurity_ORMoreThanOneInheritsGlobal(t *testing.T) {
	r := applySecurityToCRUD([]map[string][]string{
		{"apiKey": nil},
		{"basic": nil},
	})
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true")
	}
	if plan.create.securitySchemesSet {
		t.Fatalf("create.securitySchemesSet = true, want false (OR → inherit global, not select one)")
	}
	files := ResourceFiles([]ir.ResourceIR{r}, testClientImport)
	var buf bytes.Buffer
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(buf.String(), "WithSchemes") {
		t.Errorf("OR create body must not emit WithSchemes (ambiguous)\n--- body ---\n%s", buf.String())
	}
}

// TestPerOpSecurity_UnauthenticatedEmptyRequirement asserts that an operation
// declaring a single empty requirement (security: [{}]) emits
// client.WithSchemes() with no names, marking the operation unauthenticated so
// no scheme interceptor applies.
func TestPerOpSecurity_UnauthenticatedEmptyRequirement(t *testing.T) {
	r := applySecurityToCRUD([]map[string][]string{{}})
	plan := planResourceWiring(r)
	if !plan.wired {
		t.Fatalf("plan.wired = false, want true")
	}
	if !plan.create.securitySchemesSet {
		t.Fatalf("create.securitySchemesSet = false, want true (single empty requirement)")
	}
	if len(plan.create.securitySchemes) != 0 {
		t.Fatalf("create.securitySchemes = %v, want empty (unauthenticated)", plan.create.securitySchemes)
	}
	files := ResourceFiles([]ir.ResourceIR{r}, testClientImport)
	var buf bytes.Buffer
	if err := files[0].Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `client.WithSchemes()`) {
		t.Errorf("unauthenticated create body must emit client.WithSchemes()\n--- body ---\n%s", got)
	}
}

// TestPerOpSecurity_Compiles generates a full provider module with a per-op
// security-wired resource and compiles it, proving the baked WithSchemes call
// against the variadic NewRequest is syntactically valid.
func TestPerOpSecurity_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	p.Resources = []ir.ResourceIR{applySecurityToCRUD([]map[string][]string{{"apiKey": nil, "basic": nil}})}
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
		t.Fatalf("go build ./... failed for per-op security resource: %v\n%s", err, out)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
