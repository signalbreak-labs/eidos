package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/transformer"
)

// discriminatedUnionDataSourceIR returns a data source whose read returns a
// top-level discriminated oneOf (Pet = oneOf[Cat, Dog] with discriminator
// petType), surfaced as the Computed wrapper attribute the transformer
// synthesizes (D1). It exercises the dynamic-union rendering (D2): the wrapper
// renders as a SingleNestedAttribute merging all variant fields plus the
// discriminator, with a DiscriminatorValidator.
func discriminatedUnionDataSourceIR() ir.DataSourceIR {
	return ir.DataSourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{Name: "id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
				{
					Name:     "pet",
					Computed: true,
					Schema: ir.SchemaIR{
						Name:     "Pet",
						Computed: true,
						Union: &ir.UnionType{
							Kind: ir.OneOf,
							Variants: []ir.SchemaIR{
								{
									Name: "Cat",
									Attributes: []ir.AttributeIR{
										{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
										{Name: "lives_remaining", Schema: ir.SchemaIR{Type: ir.TypeInt}},
									},
								},
								{
									Name: "Dog",
									Attributes: []ir.AttributeIR{
										{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}},
										{Name: "bark_volume", Schema: ir.SchemaIR{Type: ir.TypeInt}},
									},
								},
							},
							Discriminator: &ir.DiscriminatorIR{
								PropertyName: "petType",
								Mapping: map[string]string{
									"cat": "#/components/schemas/Cat",
									"dog": "#/components/schemas/Dog",
								},
							},
						},
					},
				},
			},
		},
		ReadMapping: ir.OperationMappingIR{
			Method:       "GET",
			PathTemplate: "/pets/{id}",
			SuccessCodes: []int{200},
		},
	}
}

// TestTopLevelOneOf_DynamicUnion_Render asserts a discriminated top-level
// oneOf renders via the dynamic-union strategy: a SingleNestedAttribute
// merging all variant fields plus the snake_cased discriminator attribute
// (with the variant keys as enum values), a DiscriminatorValidator call, the
// schema/validator import, and a types.Object model field. The parent is
// Computed, so every merged child is Computed (never Required).
func TestTopLevelOneOf_DynamicUnion_Render(t *testing.T) {
	ds := discriminatedUnionDataSourceIR()

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`"pet": schema.SingleNestedAttribute{`,
		`"pet_type": schema.StringAttribute{Computed: true`,
		`"lives_remaining": schema.Int64Attribute{Computed: true`,
		`"bark_volume": schema.Int64Attribute{Computed: true`,
		// Discriminator validator with the snake_cased property name and the
		// sorted allowed variant keys.
		`DiscriminatorValidator("pet_type", "cat", "dog")`,
		// The schema/validator package is imported for the validator slice.
		`"github.com/hashicorp/terraform-plugin-framework/schema/validator"`,
		// The model field is an object, matching the SingleNestedAttribute.
		`Pet types.Object`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated dynamic-union data source missing %q\n--- body ---\n%s", want, got)
		}
	}
	// The discriminator's allowed variant keys are enforced by the validator
	// call (EnumValues→stringvalidator rendering does not exist in the
	// codebase; the DiscriminatorValidator covers the allowed-keys check).
	if !strings.Contains(got, `DiscriminatorValidator("pet_type", "cat", "dog")`) {
		t.Errorf("discriminator validator must list the variant keys\n--- body ---\n%s", got)
	}
	// The union itself never falls back to Dynamic here.
	if strings.Contains(got, `"pet": schema.DynamicAttribute{`) {
		t.Errorf("discriminated union must not render as DynamicAttribute\n--- body ---\n%s", got)
	}
	// No Required child inside the Computed parent.
	if strings.Contains(got, `"pet_type": schema.StringAttribute{Required: true}`) {
		t.Errorf("Computed parent must not have a Required discriminator child\n--- body ---\n%s", got)
	}
}

// TestNestedUnionElement_RendersDynamic asserts a collection element that is
// itself a union renders as a Dynamic element (nested unions stay Dynamic by
// design), instead of tripping the schema renderer.
func TestNestedUnionElement_RendersDynamic(t *testing.T) {
	ds := listDataSourceIR(nil)
	ds.Schema.Attributes[1].Schema.Collection.ElementType = ir.SchemaIR{
		Union: &ir.UnionType{
			Kind: ir.AnyOf,
			Variants: []ir.SchemaIR{
				{Attributes: []ir.AttributeIR{{Name: "name", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
			},
		},
	}

	file := DataSourceFile(ds, testClientImport)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	// A collection whose element is a non-discriminated union resolves to a
	// Dynamic element, which the framework cannot hold in a collection; the
	// whole attribute renders as a DynamicAttribute (G12).
	if !strings.Contains(buf.String(), "schema.DynamicAttribute") {
		t.Errorf("union collection element must render as a DynamicAttribute\n--- body ---\n%s", buf.String())
	}
}

// TestTopLevelOneOf_DynamicUnion_Compiles generates a full provider module
// with a discriminated-union data source (including validators.go) and
// compiles it, proving the SingleNestedAttribute, DiscriminatorValidator
// declaration and call, and types.Object model field type-check.
func TestTopLevelOneOf_DynamicUnion_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithDataSourceIR(discriminatedUnionDataSourceIR())
	tmp := t.TempDir()
	cfg := BuildConfig{ProviderName: p.Name, Namespace: p.Name}
	h := Harness{OutputDir: tmp}
	files := dataSourceModuleFiles(t, p, cfg)
	files = append(files, ValidatorsFile(p))
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

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
		t.Fatalf("go build ./... failed for dynamic-union data source: %v\n%s", err, out)
	}
}

// splitVariantResources returns the post-split resource set for a top-level
// discriminated oneOf, produced by transformer.ApplyOverrides with the
// split_resources strategy (D3).
func splitVariantResources(t *testing.T) []ir.ResourceIR {
	t.Helper()
	provider := &ir.ProviderIR{
		Name:      "mycloud",
		TypeName:  "mycloud",
		Resources: []ir.ResourceIR{polymorphicPetResourceIR()},
	}
	if err := transformer.ApplyOverrides(provider, testSplitConfig()); err != nil {
		t.Fatalf("ApplyOverrides() error = %v", err)
	}
	if len(provider.Resources) != 2 {
		t.Fatalf("split produced %d resources, want 2", len(provider.Resources))
	}
	return provider.Resources
}

// testSplitConfig returns a generator.yaml config selecting the
// split_resources polymorphism strategy.
func testSplitConfig() *config.Config {
	return &config.Config{Polymorphism: &config.PolymorphismConfig{
		Strategy: "split_resources",
	}}
}

// polymorphicPetResourceIR returns a managed resource whose schema is a
// top-level discriminated oneOf (Pet = oneOf[Cat, Dog]), as synthesized by
// ManagedResourceSchema: a single Computed wrapper attribute carrying the
// union.
func polymorphicPetResourceIR() ir.ResourceIR {
	return ir.ResourceIR{
		Name:     "pet",
		TypeName: "mycloud_pet",
		FullName: "mycloud_pet",
		Schema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:     "pet",
					Computed: true,
					Schema: ir.SchemaIR{
						Name:     "Pet",
						Computed: true,
						Union: &ir.UnionType{
							Kind: ir.OneOf,
							Variants: []ir.SchemaIR{
								{Name: "Cat", Attributes: []ir.AttributeIR{
									{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "lives_remaining", Schema: ir.SchemaIR{Type: ir.TypeInt}},
								}},
								{Name: "Dog", Attributes: []ir.AttributeIR{
									{Name: "petType", Schema: ir.SchemaIR{Type: ir.TypeString}},
									{Name: "bark_volume", Schema: ir.SchemaIR{Type: ir.TypeInt}},
								}},
							},
							Discriminator: &ir.DiscriminatorIR{PropertyName: "petType"},
						},
					},
				},
			},
		},
		CRUDMapping: ir.CRUDMappingIR{
			Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
			Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
			Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
		},
		SourceOperation: "getPet",
	}
}

// TestTopLevelOneOf_SplitResources_Render asserts the split-resources
// strategy yields one resource per variant with the discriminator attribute
// removed and the shared CRUD mapping intact.
func TestTopLevelOneOf_SplitResources_Render(t *testing.T) {
	resources := splitVariantResources(t)

	files := ResourceFiles(resources, testClientImport)
	if len(files) != 2 {
		t.Fatalf("ResourceFiles() returned %d files, want 2", len(files))
	}
	var joined string
	for _, f := range files {
		var buf bytes.Buffer
		if err := f.Render(&buf); err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		joined += buf.String()
	}
	for _, want := range []string{
		"CatResource",
		"DogResource",
		"lives_remaining",
		"bark_volume",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("split resources missing %q\n--- bodies ---\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "pet_type") {
		t.Errorf("variant resources must not carry the discriminator attribute\n--- bodies ---\n%s", joined)
	}
}

// TestTopLevelOneOf_SplitResources_Compiles generates a full provider module
// with the split variant resources and compiles it.
func TestTopLevelOneOf_SplitResources_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	resources := splitVariantResources(t)
	p := sampleProviderWithResourceIR()
	p.Resources = resources
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
		t.Fatalf("go build ./... failed for split-resources variants: %v\n%s", err, out)
	}
}
