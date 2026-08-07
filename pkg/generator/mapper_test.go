package generator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestValueMappersFile_Render verifies that ValueMappersFile emits the expected
// type, FromValue, ToValue, and helper functions.
func TestValueMappersFile_Render(t *testing.T) {
	r := sampleModelResourceIR()
	providerImport := "example.com/roundtrip/internal/provider"

	file := ValueMappersFile([]ir.ResourceIR{r}, providerImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package protocol",
		"provider \"example.com/roundtrip/internal/provider\"",
		"func PetModelType() tftypes.Type",
		"func PetModelFromValue(v tftypes.Value) (provider.PetModel, error)",
		"func PetModelToValue(m provider.PetModel) (tftypes.Value, error)",
		"func decodeString(v tftypes.Value, out *string) error",
		"func decodeInt64(v tftypes.Value, out *int64) error",
		"func decodeFloat64(v tftypes.Value, out *float64) error",
		"func decodeBool(v tftypes.Value, out *bool) error",
		"tftypes.Object",
		"AttributeTypes",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated mapper file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestValueMappersFile_NameCollisions is a regression test for H-7: a resource
// with nested object attributes that collide after PascalCase normalization
// (owner_bar and ownerBar) must emit DISTINCT Type/FromValue/ToValue functions
// in value_mappers.go. Previously both normalized to OwnerBar, producing
// duplicate ThingModelOwnerBarType/FromValue/ToValue declarations that do not
// compile. It also asserts the top-level colliding string fields (foo_bar and
// fooBar) decode/encode against distinct struct fields (FooBar and FooBar2).
func TestValueMappersFile_NameCollisions(t *testing.T) {
	r := ir.ResourceIR{
		Name: "thing",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "foo_bar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{Name: "fooBar", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{
					Name:     "owner_bar",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "x", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "ownerBar",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "y", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
			},
		},
	}

	file := ValueMappersFile([]ir.ResourceIR{r}, "example.com/collide/internal/provider")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Distinct nested mapper function names for the colliding object attributes.
	for _, want := range []string{
		"ThingModelOwnerBarType",
		"ThingModelOwnerBarFromValue",
		"ThingModelOwnerBarToValue",
		"ThingModelOwnerBar2Type",
		"ThingModelOwnerBar2FromValue",
		"ThingModelOwnerBar2ToValue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mapper file missing distinct function %q\ncontent:\n%s", want, got)
		}
	}

	// Top-level colliding string fields must decode/encode distinct struct
	// fields (FooBar and FooBar2), not the same field twice.
	if !strings.Contains(got, "FooBar2") {
		t.Errorf("mapper file missing disambiguated field reference FooBar2\ncontent:\n%s", got)
	}
}

// TestValueMappersRoundTrip_Primitive compiles the generated model and mapper
// files in a temporary module and verifies ToValue/FromValue round-trip for
// primitive fields.
func TestValueMappersRoundTrip_Primitive(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := sampleModelResourceIR()
	tmp := generateMapperModule(t, r)
	writePrimitiveRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestValueMappersRoundTrip_Nested compiles the generated model and mapper
// files with nested objects and collections and verifies round-trip.
func TestValueMappersRoundTrip_Nested(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := nestedResourceIR()
	tmp := generateMapperModule(t, r)
	writeNestedRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// generateMapperModule creates a temporary Go module containing the generated
// model file and value mapper file for a single resource.
func generateMapperModule(t *testing.T, r ir.ResourceIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: "roundtrip",
		Namespace:    "test",
		ModulePath:   "example.com/roundtrip",
	}

	h := Harness{OutputDir: tmp}
	files := []File{
		GoMod(cfg),
		ModelFile(r),
		ValueMappersFile([]ir.ResourceIR{r}, cfg.ModulePath+"/internal/provider"),
	}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// skipIfNetworkRestricted skips network-bound round-trip tests when the local
// Go environment is configured to avoid remote module fetches. The check is a
// best-effort heuristic: it catches the most common offline/vendor modes but
// may not detect firewalled or otherwise restricted networks, in which case
// go mod tidy may still fail with a network error.
func skipIfNetworkRestricted(t *testing.T) {
	t.Helper()
	if goflags := os.Getenv("GOFLAGS"); strings.Contains(goflags, "-mod=vendor") {
		t.Skipf("GOFLAGS=%q contains -mod=vendor; skipping network-bound round-trip test (set GOPROXY or unset -mod=vendor to run)", goflags)
	}
	if proxy := os.Getenv("GOPROXY"); strings.TrimSpace(proxy) == "off" {
		t.Skipf("GOPROXY=%q; skipping network-bound round-trip test (set a reachable proxy to run)", proxy)
	}
}

// writePrimitiveRoundTripTest writes a root-package test that exercises the
// generated mapper functions for primitive fields.
func writePrimitiveRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_test.go")
	content := `package roundtrip_test

import (
	"reflect"
	"testing"

	"example.com/roundtrip/internal/provider"
	"example.com/roundtrip/internal/protocol"
)

func TestPrimitiveRoundTrip(t *testing.T) {
	name := "spot"
	var age int64 = 3
	var weight float64 = 12.5
	original := provider.PetModel{
		Name:   &name,
		Age:    age,
		Weight: &weight,
		Happy:  true,
	}

	v, err := protocol.PetModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}

	got, err := protocol.PetModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}

	if !reflect.DeepEqual(got, original) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write round-trip test: %v", err)
	}
}

// writeNestedRoundTripTest writes a root-package test that exercises nested
// object and collection fields.
func writeNestedRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_nested_test.go")
	content := `package roundtrip_test

import (
	"reflect"
	"testing"

	"example.com/roundtrip/internal/provider"
	"example.com/roundtrip/internal/protocol"
)

func TestNestedRoundTrip(t *testing.T) {
	ownerName := "alice"
	tagValue1 := "fluffy"
	tagValue2 := "friendly"
	settingEnabled := true

	original := provider.PetModel{
		Owner: provider.PetModelOwner{Name: &ownerName},
		Tags: []provider.PetModelTagsElem{
			{Value: &tagValue1},
			{Value: &tagValue2},
		},
		Settings: map[string]provider.PetModelSettingsMapElem{
			"grooming": {Enabled: settingEnabled},
		},
	}

	v, err := protocol.PetModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}

	got, err := protocol.PetModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}

	if !reflect.DeepEqual(got, original) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write nested round-trip test: %v", err)
	}
}

// TestValueMappersRoundTrip_Set compiles and round-trips a model with Set-type
// collections of primitives and nested objects.
func TestValueMappersRoundTrip_Set(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := setResourceIR()
	tmp := generateMapperModule(t, r)
	writeSetRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestValueMappersRoundTrip_Dynamic compiles and round-trips a model with a
// dynamic-typed attribute.
func TestValueMappersRoundTrip_Dynamic(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := dynamicResourceIR()
	tmp := generateMapperModule(t, r)
	writeDynamicRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestValueMappersRoundTrip_RequiredAbsent compiles a model with required
// primitive, object, collection, and dynamic attributes and asserts that
// FromValue returns the documented "missing required attribute" error when any
// required attribute is absent from the value map.
func TestValueMappersRoundTrip_RequiredAbsent(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := requiredAbsentResourceIR()
	tmp := generateMapperModule(t, r)
	writeRequiredAbsentRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestValueMappersRoundTrip_NilCollections verifies round-trip behavior for nil
// optional collections, nil optional object pointers, and zero-value required
// collections.
func TestValueMappersRoundTrip_NilCollections(t *testing.T) {
	skipIfNetworkRestricted(t)
	r := nilCollectionsResourceIR()
	tmp := generateMapperModule(t, r)
	writeNilCollectionsRoundTripTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidy.Dir = tmp
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", ".", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// writeSetRoundTripTest writes a root-package test that exercises Set-type
// collection round-trips.
func writeSetRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_set_test.go")
	content := `package roundtrip_test

import (
	"reflect"
	"sort"
	"testing"

	"example.com/roundtrip/internal/provider"
	"example.com/roundtrip/internal/protocol"
)

func TestSetRoundTrip(t *testing.T) {
	original := provider.PetModel{
		Labels: []string{"fluffy", "friendly"},
		Members: []provider.PetModelMembersElem{
			{Name: "alice"},
			{Name: "bob"},
		},
	}

	v, err := protocol.PetModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}

	got, err := protocol.PetModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}

	// Set iteration order is undefined, so compare as sets rather than ordered slices.
	if !reflect.DeepEqual(sortSet(got.Labels), sortSet(original.Labels)) {
		t.Errorf("labels round-trip mismatch:\n got: %+v\nwant: %+v", got.Labels, original.Labels)
	}
	if !reflect.DeepEqual(sortMembers(got.Members), sortMembers(original.Members)) {
		t.Errorf("members round-trip mismatch:\n got: %+v\nwant: %+v", got.Members, original.Members)
	}
}

func sortSet(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func sortMembers(m []provider.PetModelMembersElem) []provider.PetModelMembersElem {
	out := append([]provider.PetModelMembersElem(nil), m...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write set round-trip test: %v", err)
	}
}

// writeDynamicRoundTripTest writes a root-package test that exercises a
// dynamic-typed attribute round-trip.
func writeDynamicRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_dynamic_test.go")
	content := `package roundtrip_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"example.com/roundtrip/internal/provider"
	"example.com/roundtrip/internal/protocol"
)

func TestDynamicRoundTrip(t *testing.T) {
	metadata := tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"region": tftypes.String,
		},
	}, map[string]tftypes.Value{
		"region": tftypes.NewValue(tftypes.String, "us-east-1"),
	})

	original := provider.PetModel{Metadata: metadata}

	v, err := protocol.PetModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}

	got, err := protocol.PetModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}

	if !metadata.Equal(got.Metadata) {
		t.Errorf("dynamic round-trip mismatch:\n got: %+v\nwant: %+v", got.Metadata, metadata)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write dynamic round-trip test: %v", err)
	}
}

// writeNilCollectionsRoundTripTest writes a root-package test that verifies
// nil optional collections and nil optional object pointers round-trip, and
// that empty required list/map collections round-trip as empty collections.
func writeNilCollectionsRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_nil_collections_test.go")
	content := `package roundtrip_test

import (
	"reflect"
	"testing"

	"example.com/roundtrip/internal/provider"
	"example.com/roundtrip/internal/protocol"
)

func TestNilCollectionsRoundTrip(t *testing.T) {
	original := provider.PetModel{
		Tags:             nil,
		Settings:         nil,
		Owner:            nil,
		RequiredTags:     []string{},
		RequiredSettings: map[string]string{},
	}

	v, err := protocol.PetModelToValue(original)
	if err != nil {
		t.Fatalf("ToValue error: %v", err)
	}

	got, err := protocol.PetModelFromValue(v)
	if err != nil {
		t.Fatalf("FromValue error: %v", err)
	}

	if got.Tags != nil {
		t.Errorf("Tags expected nil, got %+v", got.Tags)
	}
	if got.Settings != nil {
		t.Errorf("Settings expected nil, got %+v", got.Settings)
	}
	if got.Owner != nil {
		t.Errorf("Owner expected nil, got %+v", got.Owner)
	}
	if got.RequiredTags == nil || !reflect.DeepEqual(got.RequiredTags, []string{}) {
		t.Errorf("RequiredTags expected empty non-nil slice, got %+v", got.RequiredTags)
	}
	if !reflect.DeepEqual(got.RequiredSettings, map[string]string{}) {
		t.Errorf("RequiredSettings expected empty map, got %+v", got.RequiredSettings)
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write nil-collections round-trip test: %v", err)
	}
}

// nestedResourceIR returns a ResourceIR with nested objects and collections.
func nestedResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "owner",
					Required: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "tags",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.List,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "value", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
				{
					Name:     "settings",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Map,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "enabled", Required: true, Schema: ir.SchemaIR{Type: ir.TypeBool}},
								},
							},
						},
					},
				},
			},
		},
	}
}

// setResourceIR returns a ResourceIR with Set-type collections.
func setResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "labels",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Set,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "members",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind: ir.Set,
							ElementType: ir.SchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
	}
}

// dynamicResourceIR returns a ResourceIR with a dynamic-typed attribute.
func dynamicResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "metadata",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
				},
			},
		},
	}
}

// writeRequiredAbsentRoundTripTest writes a root-package test that exercises
// the "missing required attribute" error path for each supported attribute kind.
func writeRequiredAbsentRoundTripTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "roundtrip_required_absent_test.go")
	content := `package roundtrip_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"example.com/roundtrip/internal/protocol"
)

func TestRequiredAbsentRoundTrip(t *testing.T) {
	base := map[string]tftypes.Value{
		"name": tftypes.NewValue(tftypes.String, "spot"),
		"owner": tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{"name": tftypes.String},
		}, map[string]tftypes.Value{
			"name": tftypes.NewValue(tftypes.String, "alice"),
		}),
		"labels": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "a"),
		}),
		"metadata": tftypes.NewValue(tftypes.String, "dynamic"),
	}

	cases := []struct {
		name string
		drop string
	}{
		{"primitive", "name"},
		{"object", "owner"},
		{"collection", "labels"},
		{"dynamic", "metadata"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vals := make(map[string]tftypes.Value, len(base))
			for k, v := range base {
				vals[k] = v
			}
			delete(vals, tc.drop)

			v := tftypes.NewValue(tftypes.DynamicPseudoType, vals)
			_, err := protocol.PetModelFromValue(v)
			want := fmt.Sprintf("missing required attribute %q", tc.drop)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", want)
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got %v", want, err)
			}
		})
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write required-absent round-trip test: %v", err)
	}
}

// nilCollectionsResourceIR returns a ResourceIR with optional collections and
// an optional nested object for nil-handling tests.
func nilCollectionsResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "tags",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "settings",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "owner",
					Optional: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Optional: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "required_tags",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "required_settings",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.Map,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
			},
		},
	}
}

// requiredAbsentResourceIR returns a ResourceIR with required primitive,
// object, list, and dynamic attributes for testing the missing-required
// attribute error path.
func requiredAbsentResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "name",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeString},
				},
				{
					Name:     "owner",
					Required: true,
					Schema: ir.SchemaIR{
						Attributes: []ir.AttributeIR{
							{Name: "name", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
						},
					},
				},
				{
					Name:     "labels",
					Required: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeString},
						},
					},
				},
				{
					Name:     "metadata",
					Required: true,
					Schema:   ir.SchemaIR{Type: ir.TypeDynamic},
				},
			},
		},
	}
}

// TestValueMappersFile_DynamicElementCollection asserts a List whose element
// type is dynamic is decoded by copying the raw tftypes.Value elements (the
// model field is []tftypes.Value), not by decoding to []string (G4).
func TestValueMappersFile_DynamicElementCollection(t *testing.T) {
	r := ir.ResourceIR{
		Name:     "widget",
		TypeName: "mycloud_widget",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "payloads",
					Optional: true,
					Schema: ir.SchemaIR{
						Collection: &ir.CollectionType{
							Kind:        ir.List,
							ElementType: ir.SchemaIR{Type: ir.TypeDynamic},
						},
					},
				},
			},
		},
	}

	file := ValueMappersFile([]ir.ResourceIR{r}, "example.com/t/internal/provider")
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "m.Payloads = elems") {
		t.Errorf("expected dynamic-element collection to copy raw elements (m.Payloads = elems), got:\n%s", got)
	}
	if strings.Contains(got, "make([]string, len(elems))") {
		t.Errorf("dynamic-element collection must not decode to []string:\n%s", got)
	}
}
