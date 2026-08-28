package generator

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// TestFunctionFile_Render verifies that FunctionFile emits the expected
// function struct, Metadata, Definition, and Run methods.
func TestFunctionFile_Render(t *testing.T) {
	fn := sampleFunctionIR()

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"package provider",
		"var _ function.Function = (*ConcatTagsFunction)(nil)",
		"type ConcatTagsFunction struct",
		"SourceOperation string",
		"func (f *ConcatTagsFunction) Metadata",
		"func (f *ConcatTagsFunction) Definition",
		"func (f *ConcatTagsFunction) Run",
		"resp.Name = \"concat_tags\"",
		"function.Definition",
		"function.StringParameter",
		"function.StringReturn",
		"Summary: \"concat_tags\"",
		"Description:",
		"\"Joins a list of tags with a separator.\"",
		"Parameters: []function.Parameter",
		"Return:",
		"function.StringReturn{}",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated function file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestFunctionFile_SchemaValidation generates a minimal provider with a function
// into a temporary Go module and runs the Terraform plugin-framework function
// definition validation to confirm the generated function_<name>.go compiles
// and its definition is valid.
func TestFunctionFile_SchemaValidation(t *testing.T) {
	skipIfNetworkRestricted(t)
	fn := sampleFunctionIR()

	providerIR := ir.ProviderIR{
		Name:        "mycloud",
		TypeName:    "mycloud",
		Description: "Generated provider for MyCloud.",
		ConfigSchema: ir.ObjectSchemaIR{
			Attributes: []ir.AttributeIR{
				{
					Name:        "api_key",
					Description: "API key authentication",
					Required:    true,
					Sensitive:   true,
					Schema:      ir.SchemaIR{Type: ir.TypeString},
				},
			},
		},
		Functions: []ir.FunctionIR{fn},
	}

	tmp := generateFunctionModule(t, providerIR, []ir.FunctionIR{fn})
	writeFunctionDefinitionValidationTest(t, tmp)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./internal/provider", "-v")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("expected test to pass, got:\n%s", out)
	}
}

// TestFunctionFile_NestedDefinition verifies that object, list, set, and map
// parameters and returns render using the function package.
func TestFunctionFile_NestedDefinition(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "transform",
		TypeName: "transform",
		Arguments: []ir.AttributeIR{
			{
				Name: "input",
				Schema: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
			{
				Name: "tags",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
		},
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind:        ir.Map,
				ElementType: ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"function.ObjectParameter",
		"function.ListParameter",
		"function.MapReturn",
		"AttributeTypes: map[string]attr.Type",
		"ElementType: types.StringType",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated function definition missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestFunctionFile_Variadic verifies that a variadic function emits a
// VariadicParameter in its definition.
func TestFunctionFile_Variadic(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "join",
		TypeName: "join",
		Variadic: true,
		Arguments: []ir.AttributeIR{
			{Name: "parts", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "VariadicParameter:") {
		t.Errorf("generated variadic function missing VariadicParameter\ncontent:\n%s", got)
	}
}

// TestFunctionFile_VariadicExcludesLastFromParameters verifies that when a
// function has multiple arguments and Variadic is true, the last argument is
// emitted only as VariadicParameter and not duplicated in the positional
// Parameters slice.
func TestFunctionFile_VariadicExcludesLastFromParameters(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "format",
		TypeName: "format",
		Variadic: true,
		Arguments: []ir.AttributeIR{
			{Name: "pattern", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "args", Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	def := findFunctionDefinitionLiteral(t, got)
	paramNames := functionDefinitionParameterNames(t, def)
	if sliceContains(paramNames, "args") {
		t.Errorf("variadic argument \"args\" must not appear in Parameters slice; got %v", paramNames)
	}
	if !sliceContains(paramNames, "pattern") {
		t.Errorf("non-variadic argument \"pattern\" missing from Parameters slice; got %v", paramNames)
	}

	variadicName := functionDefinitionVariadicName(t, def)
	if variadicName != "args" {
		t.Errorf("VariadicParameter.Name = %q, want %q", variadicName, "args")
	}
}

// TestFunctionFile_NestedParameterDescriptions verifies that descriptions are
// preserved for collection and object function parameters.
func TestFunctionFile_NestedParameterDescriptions(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "describe",
		TypeName: "describe",
		Arguments: []ir.AttributeIR{
			{
				Name:        "tags",
				Description: "List of tags to describe.",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
			{
				Name:        "config",
				Description: "Configuration object.",
				Schema: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "value", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
				},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	wantSubstrings := []string{
		"function.ListParameter",
		"function.ObjectParameter",
		"Description:",
		"\"List of tags to describe.\"",
		"\"Configuration object.\"",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("generated function definition missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestFunctionFiles_Multiple verifies that FunctionFiles emits one file per
// function with deterministic, unique paths.
func TestFunctionFiles_Multiple(t *testing.T) {
	functions := []ir.FunctionIR{
		{Name: "concat_tags", TypeName: "concat_tags"},
		{Name: "format_arn", TypeName: "format_arn"},
	}

	files := FunctionFiles(functions)
	if len(files) != len(functions) {
		t.Fatalf("FunctionFiles() returned %d files, want %d", len(files), len(functions))
	}

	if files[0].Path != "internal/provider/function_concat_tags.go" {
		t.Errorf("file[0].Path = %q, want %q", files[0].Path, "internal/provider/function_concat_tags.go")
	}
	if files[1].Path != "internal/provider/function_format_arn.go" {
		t.Errorf("file[1].Path = %q, want %q", files[1].Path, "internal/provider/function_format_arn.go")
	}

	tmp := t.TempDir()
	h := Harness{OutputDir: tmp}
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	gotPaths := collectPaths(t, tmp)
	wantPaths := []string{
		"internal/provider/function_concat_tags.go",
		"internal/provider/function_format_arn.go",
	}
	if diff := sliceDiff(wantPaths, gotPaths); diff != "" {
		t.Errorf("emitted paths mismatch:\n%s", diff)
	}
}

// TestFunctionName verifies that the function name prefers FunctionIR.TypeName
// and falls back to the trimmed function name.
func TestFunctionName(t *testing.T) {
	cases := []struct {
		name     string
		fn       ir.FunctionIR
		wantName string
	}{
		{
			name:     "prefers type name",
			fn:       ir.FunctionIR{Name: "concat_tags", TypeName: "concat_tags"},
			wantName: "concat_tags",
		},
		{
			name:     "falls back to name",
			fn:       ir.FunctionIR{Name: "concat_tags"},
			wantName: "concat_tags",
		},
		{
			name:     "trims whitespace fallback",
			fn:       ir.FunctionIR{Name: "  concat_tags  "},
			wantName: "concat_tags",
		},
		{
			name:     "trims whitespace type name",
			fn:       ir.FunctionIR{Name: "concat_tags", TypeName: "  concat_tags  "},
			wantName: "concat_tags",
		},
		{
			name:     "empty type name and whitespace name yields empty string",
			fn:       ir.FunctionIR{Name: "   ", TypeName: "   "},
			wantName: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := functionName(tc.fn); got != tc.wantName {
				t.Errorf("functionName() = %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestFunctionFile_ReturnDefaultsToDynamic verifies that a function with no
// return type information still emits a default DynamicReturn so the framework
// definition validation does not fail with an undefined Return field, and that
// the default is honest: an unrecognized return shape is surfaced as Dynamic
// (accepts any value) rather than silently mislabeled as String (N-27).
func TestFunctionFile_ReturnDefaultsToDynamic(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "noop",
		TypeName: "noop",
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Return:") {
		t.Errorf("generated function missing Return field\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "function.DynamicReturn{}") {
		t.Errorf("expected default DynamicReturn for empty ReturnType\ncontent:\n%s", got)
	}
	if strings.Contains(got, "function.StringReturn{}") {
		t.Errorf("empty ReturnType must not silently degrade to StringReturn\ncontent:\n%s", got)
	}
}

// TestFunctionFile_UnionAndUnknownDegradeToDynamic locks in the N-27 fail-loud
// fix: a function parameter or return that is a union, an unknown collection
// kind, or an unknown primitive type must render as Dynamic (honest about the
// untyped shape) rather than silently degrading to String, which would make the
// documented signature differ from what Terraform sees.
func TestFunctionFile_UnionAndUnknownDegradeToDynamic(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "flex",
		TypeName: "flex",
		Arguments: []ir.AttributeIR{
			{Name: "choice", Schema: ir.SchemaIR{Union: &ir.UnionType{Kind: ir.AnyOf, Variants: []ir.SchemaIR{{Type: ir.TypeString}, {Type: ir.TypeInt}}}}},
			{Name: "mystery_collection", Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: "matrix", ElementType: ir.SchemaIR{Type: ir.TypeString}}}},
			{Name: "mystery_primitive", Schema: ir.SchemaIR{Type: "hyper"}},
		},
		ReturnType: ir.SchemaIR{Union: &ir.UnionType{Kind: ir.AnyOf, Variants: []ir.SchemaIR{{Type: ir.TypeBool}}}},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`function.DynamicParameter{Name: "choice"`,
		`function.DynamicParameter{Name: "mystery_collection"`,
		`function.DynamicParameter{Name: "mystery_primitive"`,
		"function.DynamicReturn{}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated function missing %q\ncontent:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		`function.StringParameter{Name: "choice"`,
		`function.StringParameter{Name: "mystery_collection"`,
		`function.StringParameter{Name: "mystery_primitive"`,
		"function.StringReturn{}",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("generated function must not degrade to %q\ncontent:\n%s", unwanted, got)
		}
	}
}

// TestFunctionFile_PrimitiveOnlyOmitsTypesImport verifies the §6 latent
// unused-import fix: a function whose parameters and return are all primitive
// never references the terraform-plugin-framework types package (plain
// primitives render as function.StringParameter/Int64Return/etc. directly),
// so the generated file must not import types or it would not compile.
func TestFunctionFile_PrimitiveOnlyOmitsTypesImport(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "noop",
		TypeName: "noop",
		Arguments: []ir.AttributeIR{
			{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeString}},
			{Name: "b", Schema: ir.SchemaIR{Type: ir.TypeInt}},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/types"`) {
		t.Errorf("primitive-only function must not import types (unused import)\ncontent:\n%s", got)
	}
}

// TestFunctionFile_CollectionParameterImportsTypes verifies a function with a
// collection parameter references types via functionAttrType and so imports
// the types package, the positive complement to the primitive-only case.
func TestFunctionFile_CollectionParameterImportsTypes(t *testing.T) {
	fn := sampleFunctionIR() // has a List-of-string `tags` parameter

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `"github.com/hashicorp/terraform-plugin-framework/types"`) {
		t.Errorf("function with a collection parameter must import types\ncontent:\n%s", got)
	}
	// The list parameter's primitive element type renders via functionAttrType
	// as types.StringType, proving the collection path references types.
	if !strings.Contains(got, "types.StringType") {
		t.Errorf("expected types.StringType for the list parameter element\ncontent:\n%s", got)
	}
}

// TestFunctionNeedsTypesImport verifies the gate directly: primitive-only
// functions return false; collection and object parameters/returns return
// true, matching the exact render decision for functionAttrType.
func TestFunctionNeedsTypesImport(t *testing.T) {
	if functionNeedsTypesImport(ir.FunctionIR{
		Arguments:  []ir.AttributeIR{{Name: "a", Schema: ir.SchemaIR{Type: ir.TypeString}}},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}) {
		t.Fatalf("primitive-only function must not need types import")
	}
	// Collection parameter references types via functionAttrType.
	if !functionNeedsTypesImport(ir.FunctionIR{
		Arguments: []ir.AttributeIR{{
			Name:   "tags",
			Schema: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
		}},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}) {
		t.Fatalf("function with a collection parameter must need types import")
	}
	// Object parameter references types via functionAttributeTypesMap.
	if !functionNeedsTypesImport(ir.FunctionIR{
		Arguments: []ir.AttributeIR{{
			Name:   "obj",
			Schema: ir.SchemaIR{Attributes: []ir.AttributeIR{{Name: "x", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
		}},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}) {
		t.Fatalf("function with an object parameter must need types import")
	}
	// Collection return references types.
	if !functionNeedsTypesImport(ir.FunctionIR{
		ReturnType: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
	}) {
		t.Fatalf("function with a collection return must need types import")
	}
}

// TestFunctionFile_ObjectParameterIgnoresBlocks verifies that nested block
// attributes are not flattened into the top-level AttributeTypes map of a
// function object parameter. Direct attributes are still emitted.
func TestFunctionFile_ObjectParameterIgnoresBlocks(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "with_block",
		TypeName: "with_block",
		Arguments: []ir.AttributeIR{
			{
				Name: "obj",
				Schema: ir.SchemaIR{
					Attributes: []ir.AttributeIR{
						{Name: "direct", Schema: ir.SchemaIR{Type: ir.TypeString}},
					},
					Blocks: []ir.BlockIR{
						{
							Name: "nested",
							Schema: ir.ObjectSchemaIR{
								Attributes: []ir.AttributeIR{
									{Name: "nested_value", Schema: ir.SchemaIR{Type: ir.TypeString}},
								},
							},
						},
					},
				},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `"direct": types.StringType`) {
		t.Errorf("direct object attribute missing from generated parameter\ncontent:\n%s", got)
	}
	if strings.Contains(got, "nested_value") {
		t.Errorf("nested block attribute must not be flattened into object parameter\ncontent:\n%s", got)
	}
}

// TestFunctionFile_Descriptions verifies that plain-text descriptions are
// emitted into Description and markdown descriptions are emitted into
// MarkdownDescription for both the function definition and its parameters.
func TestFunctionFile_Descriptions(t *testing.T) {
	fn := ir.FunctionIR{
		Name:                "describe",
		TypeName:            "describe",
		Description:         "Plain text summary.",
		MarkdownDescription: "**Markdown** summary.",
		Arguments: []ir.AttributeIR{
			{
				Name:                "value",
				Description:         "Plain text parameter.",
				MarkdownDescription: "**Markdown** parameter.",
				Schema:              ir.SchemaIR{Type: ir.TypeString},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"Description:",
		"MarkdownDescription:",
		"\"Plain text summary.\"",
		"\"**Markdown** summary.\"",
		"\"Plain text parameter.\"",
		"\"**Markdown** parameter.\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated function file missing %q\ncontent:\n%s", want, got)
		}
	}
}

// TestFunctionFile_DeprecatedParameter verifies that a deprecated function
// argument emits DeprecationMessage on the generated function.Parameter (M-10).
func TestFunctionFile_DeprecatedParameter(t *testing.T) {
	fn := ir.FunctionIR{
		Name:        "lookup",
		TypeName:    "lookup",
		Description: "Looks up a value.",
		Arguments: []ir.AttributeIR{
			{
				Name:               "legacy_key",
				Description:        "Legacy lookup key.",
				Deprecated:         true,
				DeprecationMessage: "Use key instead.",
				Schema:             ir.SchemaIR{Type: ir.TypeString},
			},
			{
				Name:        "key",
				Description: "Current lookup key.",
				Schema:      ir.SchemaIR{Type: ir.TypeString},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"Name: \"legacy_key\"",
		"DeprecationMessage:",
		"\"Use key instead.\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated function file missing %q\ncontent:\n%s", want, got)
		}
	}

	// The non-deprecated parameter must not carry a DeprecationMessage.
	if strings.Contains(got, "Name: \"key\"\n\t\t\tDeprecationMessage:") {
		t.Errorf("non-deprecated parameter unexpectedly emitted DeprecationMessage\ncontent:\n%s", got)
	}
}

// sampleFunctionIR returns a FunctionIR used for render and validation tests.
func sampleFunctionIR() ir.FunctionIR {
	return ir.FunctionIR{
		Name:        "concat_tags",
		TypeName:    "concat_tags",
		Description: "Joins a list of tags with a separator.",
		Arguments: []ir.AttributeIR{
			{
				Name:        "separator",
				Description: "Delimiter placed between tags.",
				Schema:      ir.SchemaIR{Type: ir.TypeString},
			},
			{
				Name:        "tags",
				Description: "Tags to join.",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Type: ir.TypeString},
					},
				},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}
}

// generateFunctionModule writes the generated go.mod, provider.go, and function
// files into a temporary module directory and returns the module root.
func generateFunctionModule(t *testing.T, providerIR ir.ProviderIR, functions []ir.FunctionIR) string {
	t.Helper()
	tmp := t.TempDir()

	cfg := BuildConfig{
		ProviderName: providerIR.Name,
		Namespace:    providerIR.Name,
	}

	h := Harness{OutputDir: tmp}
	pf, err := ProviderFile(providerIR)
	if err != nil {
		t.Fatalf("ProviderFile() error = %v", err)
	}
	files := append(BuildFiles(cfg), pf)
	files = append(files, FunctionFiles(functions)...)
	if err := h.Generate(files); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	return tmp
}

// writeFunctionDefinitionValidationTest writes a small test file that imports
// the generated function and validates its definition implementation.
func writeFunctionDefinitionValidationTest(t *testing.T, moduleRoot string) {
	t.Helper()
	testPath := filepath.Join(moduleRoot, "internal", "provider", "function_schema_validate_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o750); err != nil {
		t.Fatalf("create provider test dir: %v", err)
	}

	content := `package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/function"
)

func TestFunctionDefinitionValidation(t *testing.T) {
	p := New()
	pwf, ok := p.(interface {
		Functions(context.Context) []func() function.Function
	})
	if !ok {
		t.Fatalf("provider does not implement provider.ProviderWithFunctions")
	}

	for _, ff := range pwf.Functions(context.Background()) {
		fn := ff()
		var mdResp function.MetadataResponse
		fn.Metadata(context.Background(), function.MetadataRequest{}, &mdResp)

		var defResp function.DefinitionResponse
		fn.Definition(context.Background(), function.DefinitionRequest{}, &defResp)

		var validateReq function.DefinitionValidateRequest
		validateReq.FuncName = mdResp.Name
		var validateResp function.DefinitionValidateResponse
		defResp.Definition.ValidateImplementation(context.Background(), validateReq, &validateResp)

		if validateResp.Diagnostics.HasError() {
			t.Fatalf("function definition validation failed for %s: %s", mdResp.Name, validateResp.Diagnostics)
		}
	}
}
`
	if err := os.WriteFile(testPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write function definition validation test: %v", err)
	}
}

// findFunctionDefinitionLiteral parses the generated function source and returns
// the function.Definition composite literal assigned to resp.Definition.
func findFunctionDefinitionLiteral(t *testing.T, src string) *ast.CompositeLit {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "function.go", src, 0)
	if err != nil {
		t.Fatalf("parse generated source: %v", err)
	}

	var def *ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "function" || sel.Sel.Name != "Definition" {
			return true
		}
		def = cl
		return false
	})

	if def == nil {
		t.Fatal("function.Definition composite literal not found")
	}
	return def
}

// stringValue extracts the raw string value from an AST string literal.
func stringValue(t *testing.T, expr ast.Expr) string {
	t.Helper()
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		t.Fatalf("expected string literal, got %T", expr)
	}
	v, err := strconv.Unquote(bl.Value)
	if err != nil {
		t.Fatalf("unquote string literal %q: %v", bl.Value, err)
	}
	return v
}

// functionDefinitionParameterNames returns the Name values for every element in
// the Definition.Parameters slice.
func functionDefinitionParameterNames(t *testing.T, def *ast.CompositeLit) []string {
	t.Helper()
	for _, elt := range def.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Parameters" {
			continue
		}
		params, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("Parameters field is not a composite literal")
		}
		var names []string
		for _, item := range params.Elts {
			itemCL, ok := item.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, attr := range itemCL.Elts {
				attrKV, ok := attr.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				attrKey, ok := attrKV.Key.(*ast.Ident)
				if !ok || attrKey.Name != "Name" {
					continue
				}
				names = append(names, stringValue(t, attrKV.Value))
			}
		}
		return names
	}
	return nil
}

// functionDefinitionVariadicName returns the Name value from the Definition.VariadicParameter.
func functionDefinitionVariadicName(t *testing.T, def *ast.CompositeLit) string {
	t.Helper()
	for _, elt := range def.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "VariadicParameter" {
			continue
		}
		param, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("VariadicParameter field is not a composite literal")
		}
		for _, attr := range param.Elts {
			attrKV, ok := attr.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			attrKey, ok := attrKV.Key.(*ast.Ident)
			if !ok || attrKey.Name != "Name" {
				continue
			}
			return stringValue(t, attrKV.Value)
		}
		t.Fatalf("VariadicParameter has no Name field")
	}
	t.Fatalf("VariadicParameter field not found")
	return ""
}

// compile-time interface checks.
var _ = ir.FunctionIR{}
var _ = time.Second

// TestFunctionFile_SetAndMapCollectionParameters verifies functionAttrType
// renders Set and Map collection parameters (the branches beyond List).
func TestFunctionFile_SetAndMapCollectionParameters(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "summarize",
		TypeName: "summarize",
		Arguments: []ir.AttributeIR{
			{
				Name: "unique_tags",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
			{
				Name: "counts",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{Kind: ir.Map, ElementType: ir.SchemaIR{Type: ir.TypeInt}},
				},
			},
		},
		ReturnType: ir.SchemaIR{Type: ir.TypeString},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "function.SetParameter") {
		t.Errorf("set parameter must render function.SetParameter\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "function.MapParameter") {
		t.Errorf("map parameter must render function.MapParameter\ncontent:\n%s", got)
	}
	if !strings.Contains(got, "types.Int64Type") {
		t.Errorf("map element must render types.Int64Type\ncontent:\n%s", got)
	}
}

// TestFunctionAttrType_NestedCollectionBranches covers the collection, union,
// object, dynamic, and unknown-type branches of functionAttrType, which are
// only reachable through nested collection elements (parameters and returns
// handle their own top-level collection kind).
func TestFunctionAttrType_NestedCollectionBranches(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "analyze",
		TypeName: "analyze",
		Arguments: []ir.AttributeIR{
			{
				Name: "matrix",
				Schema: ir.SchemaIR{
					Collection: &ir.CollectionType{
						Kind:        ir.List,
						ElementType: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}},
					},
				},
			},
		},
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{
					Collection: &ir.CollectionType{Kind: ir.Set, ElementType: ir.SchemaIR{Type: ir.TypeString}},
				},
			},
		},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	// Nested List-of-List parameter: the inner element renders via functionAttrType.
	if !strings.Contains(got, "function.ListParameter{Name: \"matrix\", ElementType: types.ListType{ElemType: types.StringType}}") {
		t.Errorf("nested list parameter not rendered as expected\ncontent:\n%s", got)
	}
	// Nested List-of-Set return: the Set element renders via functionAttrType.
	if !strings.Contains(got, "function.ListReturn{ElementType: types.SetType{ElemType: types.StringType}}") {
		t.Errorf("nested set return not rendered as expected\ncontent:\n%s", got)
	}
}

// TestFunctionAttrType_UnionObjectDynamicUnknown covers the union, object,
// dynamic, and unknown-type branches of functionAttrType via collection
// elements.
func TestFunctionAttrType_UnionObjectDynamicUnknown(t *testing.T) {
	fn := ir.FunctionIR{
		Name:     "classify",
		TypeName: "classify",
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{
					Union: &ir.UnionType{
						Kind:     ir.OneOf,
						Variants: []ir.SchemaIR{{Type: ir.TypeString}, {Type: ir.TypeInt}},
					},
				},
			},
		},
	}

	file := FunctionFile(fn)
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "types.DynamicType") {
		t.Errorf("union element must render types.DynamicType\ncontent:\n%s", got)
	}

	// Object element: List of object renders ObjectType with AttrTypes.
	fn2 := ir.FunctionIR{
		Name:     "describe",
		TypeName: "describe",
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind: ir.List,
				ElementType: ir.SchemaIR{
					Attributes: []ir.AttributeIR{{Name: "label", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}}},
				},
			},
		},
	}
	file2 := FunctionFile(fn2)
	var buf2 bytes.Buffer
	if err := file2.Render(&buf2); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got2 := buf2.String()
	if !strings.Contains(got2, "types.ObjectType{AttrTypes: map[string]attr.Type{\"label\": types.StringType}}") {
		t.Errorf("object element must render types.ObjectType\ncontent:\n%s", got2)
	}

	// Dynamic element: List of dynamic renders DynamicType.
	fn3 := ir.FunctionIR{
		Name:     "probe",
		TypeName: "probe",
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{Type: ir.TypeDynamic},
			},
		},
	}
	file3 := FunctionFile(fn3)
	var buf3 bytes.Buffer
	if err := file3.Render(&buf3); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got3 := buf3.String()
	if !strings.Contains(got3, "types.DynamicType") {
		t.Errorf("dynamic element must render types.DynamicType\ncontent:\n%s", got3)
	}

	// Unknown element type: renders DynamicType (honest fallback, N-27).
	fn4 := ir.FunctionIR{
		Name:     "opaque",
		TypeName: "opaque",
		ReturnType: ir.SchemaIR{
			Collection: &ir.CollectionType{
				Kind:        ir.List,
				ElementType: ir.SchemaIR{},
			},
		},
	}
	file4 := FunctionFile(fn4)
	var buf4 bytes.Buffer
	if err := file4.Render(&buf4); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got4 := buf4.String()
	if !strings.Contains(got4, "types.DynamicType") {
		t.Errorf("unknown element must render types.DynamicType\ncontent:\n%s", got4)
	}
}
