package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func sizeTestProvider() *ir.ProviderIR {
	long := strings.Repeat("x", 4096)
	element := ir.SchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "inner", Description: long, Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}
	return &ir.ProviderIR{
		Name: "sizetest",
		Resources: []ir.ResourceIR{
			{
				TypeName: "sizetest_big",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "label", Description: long, Schema: ir.SchemaIR{Type: ir.TypeString}},
						{Name: "nested", Schema: ir.SchemaIR{
							Collection: &ir.CollectionType{Kind: ir.List, ElementType: element},
						}},
					},
					Blocks: []ir.BlockIR{
						{Name: "opts", Description: long, Schema: ir.ObjectSchemaIR{
							Attributes: []ir.AttributeIR{{Name: "flag", Schema: ir.SchemaIR{Type: ir.TypeBool}}},
						}},
					},
				},
			},
		},
	}
}

func TestEstimateProviderSchemaBytes_CountsDescriptionsEverywhere(t *testing.T) {
	p := sizeTestProvider()
	estimate := EstimateProviderSchemaBytes(p)
	// Three 4KiB descriptions (attribute, nested collection element attribute,
	// block) must be counted, plus the per-attribute overhead for every
	// attribute. A flat lower bound keeps the assertion about coverage, not
	// about the exact overhead model.
	if estimate < 3*4096 {
		t.Errorf("estimate %d does not count all three 4KiB descriptions", estimate)
	}
	if EstimateProviderSchemaBytes(nil) != 0 {
		t.Error("nil provider must estimate to 0")
	}
}

func TestCheckProviderSchemaSize_Thresholds(t *testing.T) {
	p := sizeTestProvider()
	estimate := EstimateProviderSchemaBytes(p)

	// Default limits are far above a small provider: clean.
	diags := CheckProviderSchemaSize(p, nil)
	if diags.HasErrors() {
		t.Errorf("default limits must not flag a small provider: %v", diags)
	}

	// Warning below the fail cap, at or above the warn threshold.
	warn := int(estimate / 2)
	fail := int(estimate * 2)
	diags = CheckProviderSchemaSize(p, &config.LimitsConfig{MaxSchemaBytes: fail, WarnSchemaBytes: warn})
	if diags.HasErrors() {
		t.Fatalf("estimate under the fail cap must not error: %v", diags)
	}
	if len(diags) != 1 || diags[0].Severity != diagnostics.Warning {
		t.Fatalf("expected one warning diagnostic, got %v", diags)
	}
	if !strings.Contains(diags[0].Detail, "sizetest_big") {
		t.Errorf("warning must name the largest construct, got %q", diags[0].Detail)
	}

	// At or above the fail cap: one error.
	diags = CheckProviderSchemaSize(p, &config.LimitsConfig{MaxSchemaBytes: int(estimate)})
	if len(diags) != 1 || diags[0].Severity != diagnostics.Error {
		t.Fatalf("expected one error diagnostic, got %v", diags)
	}
	if !strings.Contains(diags[0].Detail, "terraform init") {
		t.Errorf("error must name the init failure the cap prevents, got %q", diags[0].Detail)
	}

	// Negative cap disables the check entirely.
	diags = CheckProviderSchemaSize(p, &config.LimitsConfig{MaxSchemaBytes: -1})
	if len(diags) != 0 {
		t.Errorf("negative cap must disable the check, got %v", diags)
	}
}

func TestApplyDescriptionLimit_TruncatesOnRuneBoundary(t *testing.T) {
	p := sizeTestProvider()
	// max must be large enough to fit an ellipsis.
	if n := ApplyDescriptionLimit(p, 64); n != 3 {
		t.Fatalf("expected 3 truncated descriptions (attribute, nested attribute, block), got %d", n)
	}
	check := func(label, got string) {
		t.Helper()
		if len(got) != 64 {
			t.Errorf("%s length = %d, want capped at 64 bytes", label, len(got))
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("%s must end with the ellipsis marker, got %q", label, got)
		}
	}
	check("attribute", p.Resources[0].Schema.Attributes[0].Description)
	check("nested", p.Resources[0].Schema.Attributes[1].Schema.Collection.ElementType.Attributes[0].Description)
	check("block", p.Resources[0].Schema.Blocks[0].Description)
	// Untouched fields stay untouched.
	if p.Resources[0].Schema.Blocks[0].Schema.Attributes[0].Name != "flag" {
		t.Errorf("nested block attribute must survive truncation, got %+v", p.Resources[0].Schema.Blocks[0].Schema.Attributes[0])
	}

	// Truncation is idempotent: a second application finds nothing to cut.
	if n := ApplyDescriptionLimit(p, 64); n != 0 {
		t.Errorf("second truncation pass must be a no-op, truncated %d", n)
	}

	// max <= 0 is a no-op.
	p2 := sizeTestProvider()
	if n := ApplyDescriptionLimit(p2, 0); n != 0 {
		t.Errorf("max 0 must disable truncation, truncated %d", n)
	}
	if n := ApplyDescriptionLimit(p2, -1); n != 0 {
		t.Errorf("negative max must disable truncation, truncated %d", n)
	}
}

func TestTruncateDescription_MultibyteRunes(t *testing.T) {
	s := strings.Repeat("é", 100) // 2 bytes per rune, 200 bytes total
	if !truncateDescription(&s, 51) {
		t.Fatal("a 200-byte string over a 51-byte cap must truncate")
	}
	if len(s) != 51 {
		t.Errorf("truncated length = %d, want 51 (48 bytes of runes + 3-byte ellipsis)", len(s))
	}
	for _, r := range s {
		if r == 0xFFFD {
			t.Fatalf("truncation split a rune: %q", s)
		}
	}
	if !strings.HasSuffix(s, "…") {
		t.Errorf("truncated string must end with the ellipsis, got %q", s)
	}

	// Cap too small for an ellipsis: hard cut, never exceeding the cap.
	s2 := strings.Repeat("a", 10)
	if !truncateDescription(&s2, 2) {
		t.Fatal("a 10-byte string over a 2-byte cap must truncate")
	}
	if s2 != "aa" {
		t.Errorf("hard cut = %q, want %q", s2, "aa")
	}

	// Under the cap: untouched.
	s3 := "short"
	if truncateDescription(&s3, 100) {
		t.Error("a string under the cap must not be reported truncated")
	}
	if s3 != "short" {
		t.Errorf("string under the cap must be untouched, got %q", s3)
	}
}

func TestRun_SchemaOverHardCapFails(t *testing.T) {
	p := sizeTestProvider()
	estimate := EstimateProviderSchemaBytes(p)
	_, err := Run(p, Options{
		Mode:           ModeRecord,
		CollectOptions: DefaultCollectOptions(),
		Limits:         &config.LimitsConfig{MaxSchemaBytes: int(estimate)},
	})
	if err == nil {
		t.Fatal("a provider over the schema hard cap must fail Run")
	}
	if !strings.Contains(err.Error(), "Provider schema exceeds the Terraform size limit") {
		t.Errorf("error %q must name the size limit", err)
	}

	// The description lever brings it back under the cap in the same run: the
	// three 4KiB descriptions dominate the estimate.
	limits := &config.LimitsConfig{MaxSchemaBytes: int(estimate), MaxDescriptionBytes: 128}
	entries, err := Run(p, Options{
		Mode:           ModeRecord,
		CollectOptions: DefaultCollectOptions(),
		Limits:         limits,
	})
	if err != nil {
		t.Fatalf("description truncation must bring the provider back under the cap: %v", err)
	}
	if len(entries) == 0 {
		t.Error("record mode must return a non-empty plan")
	}
}

func TestHarnessGenerate_DocsFileOverLimit(t *testing.T) {
	tmp := t.TempDir()

	// A docs markdown file over the cap must fail loud: the Registry would
	// otherwise truncate it silently at publish time.
	big := &Harness{
		OutputDir:        filepath.Join(tmp, "big"),
		MaxDocsFileBytes: 100,
	}
	err := big.Generate([]File{Template("docs/resources/huge.md", strings.Repeat("x", 200), nil)})
	if err == nil {
		t.Fatal("an oversize docs file must fail generation")
	}
	if !strings.Contains(err.Error(), "docs/resources/huge.md") || !strings.Contains(err.Error(), "Registry") {
		t.Errorf("error %q must name the file and the Registry truncation", err)
	}

	// A non-docs file of the same size is not subject to the docs cap.
	ok := &Harness{
		OutputDir:        filepath.Join(tmp, "ok"),
		MaxDocsFileBytes: 100,
	}
	if err := ok.Generate([]File{Template("internal/provider/big.go", strings.Repeat("x", 200), nil)}); err != nil {
		t.Fatalf("non-docs files must not be subject to the docs cap: %v", err)
	}
	// Sanity: the file was actually written.
	if _, statErr := os.Stat(filepath.Join(tmp, "ok", "internal", "provider", "big.go")); statErr != nil {
		t.Fatalf("expected the non-docs file to be written: %v", statErr)
	}

	// A docs file under the cap writes normally.
	under := &Harness{
		OutputDir:        filepath.Join(tmp, "under"),
		MaxDocsFileBytes: 1000,
	}
	if err := under.Generate([]File{Template("docs/resources/huge.md", strings.Repeat("x", 200), nil)}); err != nil {
		t.Fatalf("a docs file under the cap must write: %v", err)
	}
}

func TestEffectiveDocsFileLimit(t *testing.T) {
	if got := EffectiveDocsFileLimit(nil); got != DocsFileLimitDefault {
		t.Errorf("nil limits = %d, want the %d default", got, DocsFileLimitDefault)
	}
	if got := EffectiveDocsFileLimit(&config.LimitsConfig{}); got != DocsFileLimitDefault {
		t.Errorf("empty limits = %d, want the %d default", got, DocsFileLimitDefault)
	}
	if got := EffectiveDocsFileLimit(&config.LimitsConfig{MaxDocsFileBytes: 12345}); got != 12345 {
		t.Errorf("explicit limit = %d, want 12345", got)
	}
	if got := EffectiveDocsFileLimit(&config.LimitsConfig{MaxDocsFileBytes: -1}); got != 0 {
		t.Errorf("negative limit = %d, want 0 (disabled)", got)
	}
}
