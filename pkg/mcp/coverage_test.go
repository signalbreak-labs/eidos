package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

const petStoreSpec = `openapi: "3.0.0"
info:
  title: Pet Store
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPet
      responses:
        "201": {description: created}
  /pets/{id}:
    get:
      operationId: getPet
      responses:
        "200": {description: ok}
    delete:
      operationId: deletePet
      responses:
        "200": {description: ok}
`

// TestHandleGenerate_DryRun asserts the generate tool runs the pipeline and
// reports a wired pet resource without writing any files.
func TestHandleGenerate_DryRun(t *testing.T) {
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || !out.Valid {
		t.Fatalf("expected valid generate result, got %+v", out)
	}
	if out.FileCount != 0 || out.OutputDir != "" {
		t.Errorf("dry run should not write files, got file_count=%d output=%q", out.FileCount, out.OutputDir)
	}
	wired := false
	for _, r := range out.Resources {
		if r.Name == "pet" && r.Wired {
			wired = true
		}
	}
	if !wired {
		t.Errorf("expected wired pet resource, got %+v", out.Resources)
	}
}

// TestHandleGenerate_WriteMode asserts a non-empty output writes provider files
// and reports the file count and directory.
func TestHandleGenerate_WriteMode(t *testing.T) {
	outDir := t.TempDir()
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: petStoreSpec, Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || !out.Valid {
		t.Fatalf("expected valid generate result, got %+v", out)
	}
	if out.FileCount == 0 {
		t.Errorf("expected provider files written, got file_count=0")
	}
	if out.OutputDir != outDir {
		t.Errorf("output dir = %q, want %q", out.OutputDir, outDir)
	}
}

// TestHandleGenerate_InvalidSpec asserts an invalid spec reports valid=false and
// never triggers a write.
func TestHandleGenerate_InvalidSpec(t *testing.T) {
	outDir := t.TempDir()
	res, out, err := HandleGenerate(context.Background(), nil, GenerateArgs{Spec: "not a spec: [", Output: outDir})
	if err != nil {
		t.Fatalf("HandleGenerate error: %v", err)
	}
	if res == nil || out.Valid {
		t.Fatalf("expected invalid result, got %+v", out)
	}
	if out.FileCount != 0 {
		t.Errorf("invalid spec must not write files, got file_count=%d", out.FileCount)
	}
}

// TestSummarizeEntities_WiredFlags drives every summarizer through both wired
// and scaffolded (unwired) entities.
func TestSummarizeEntities_WiredFlags(t *testing.T) {
	ds := summarizeDataSources([]ir.DataSourceIR{
		{Name: "stats", TypeName: "stats", ReadMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/stats"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(ds) != 2 || !ds[0].Wired || ds[1].Wired {
		t.Errorf("data sources = %+v", ds)
	}

	actions := summarizeActions([]ir.ActionIR{
		{Name: "reboot", TypeName: "reboot", InvokeMapping: ir.OperationMappingIR{Method: "POST", PathTemplate: "/reboot"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(actions) != 2 || !actions[0].Wired || actions[1].Wired {
		t.Errorf("actions = %+v", actions)
	}

	ephs := summarizeEphemerals([]ir.EphemeralResourceIR{
		{Name: "session", TypeName: "session", OpenMapping: ir.OperationMappingIR{Method: "POST", PathTemplate: "/sessions"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(ephs) != 2 || !ephs[0].Wired || ephs[1].Wired {
		t.Errorf("ephemerals = %+v", ephs)
	}

	lists := summarizeLists([]ir.ListResourceIR{
		{Name: "pets", TypeName: "pets", ListMapping: ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets"}},
		{Name: "stub", TypeName: "stub"},
	})
	if len(lists) != 2 || !lists[0].Wired || lists[1].Wired {
		t.Errorf("lists = %+v", lists)
	}

	fns := summarizeFunctions([]ir.FunctionIR{
		{Name: "now", TypeName: "now", SourceOperation: "getNow"},
		{Name: "stub", TypeName: "stub"},
	})
	if len(fns) != 2 || !fns[0].Wired || fns[1].Wired {
		t.Errorf("functions = %+v", fns)
	}
}

// TestSchemaIssues_AttributeShapes drives every attribute-level issue branch of
// the framework-validity walk: root attribute checks, object recursion, the
// nested-dynamic collection branch, and union variant recursion.
func TestSchemaIssues_AttributeShapes(t *testing.T) {
	obj := ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
		{Name: "bad-name", Schema: ir.SchemaIR{Type: ir.TypeString}}, // invalid identifier
		{Name: "count", Schema: ir.SchemaIR{Type: ir.TypeString}},    // reserved root name
		{Name: "both", Required: true, Computed: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
		{Name: "ok", Schema: ir.SchemaIR{Type: ir.TypeString}}, // clean
		{Name: "list", Schema: ir.SchemaIR{Collection: &ir.CollectionType{ // nested dynamic via collection
			Kind: ir.List,
			ElementType: ir.SchemaIR{Attributes: []ir.AttributeIR{
				{Name: "deep", Schema: ir.SchemaIR{Type: ir.TypeDynamic}},
			}},
		}}},
		{Name: "union", Schema: ir.SchemaIR{Union: &ir.UnionType{Variants: []ir.SchemaIR{ // union variant recursion
			{Attributes: []ir.AttributeIR{{Name: "variant-field", Schema: ir.SchemaIR{Type: ir.TypeString}}}},
		}}}},
	}}
	issues := schemaIssues("resource pet", obj)
	kinds := map[string]bool{}
	for _, i := range issues {
		kinds[i.Kind] = true
	}
	for _, want := range []string{"invalid-attribute-name", "reserved-root-name", "computed-and-required", "nested-dynamic"} {
		if !kinds[want] {
			t.Errorf("expected issue kind %q, got %+v", want, kinds)
		}
	}
}

// TestCollectionSchemaIssues covers the dynamic-element and nested-collection
// branches plus the clean pass.
func TestCollectionSchemaIssues(t *testing.T) {
	dyn := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeDynamic}}}, "items")
	if len(dyn) != 1 || dyn[0].Kind != "dynamic-element-collection" {
		t.Errorf("dynamic element = %+v", dyn)
	}
	nested := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{
		Kind: ir.List, ElementType: ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.Set}},
	}}, "objs")
	if len(nested) != 1 || nested[0].Kind != "nested-collection" {
		t.Errorf("nested collection = %+v", nested)
	}
	clean := collectionSchemaIssues("resource pet", ir.SchemaIR{Collection: &ir.CollectionType{Kind: ir.List, ElementType: ir.SchemaIR{Type: ir.TypeString}}}, "tags")
	if len(clean) != 0 {
		t.Errorf("clean collection = %+v, want no issues", clean)
	}
}

// TestErrorToolResult asserts the error result carries a JSON body flagging
// valid=false with an error diagnostic.
func TestErrorToolResult(t *testing.T) {
	res := errorToolResult(errors.New("boom"))
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected non-empty error result")
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want text content", res.Content[0])
	}
	body := text.Text
	if !strings.Contains(body, "boom") || !strings.Contains(body, "valid") {
		t.Errorf("error result body = %q, want error detail + valid=false", body)
	}
}

// TestRecoverTool_PanicPath asserts recoverTool swallows a panic in a deferred
// call so the handler never propagates.
func TestRecoverTool_PanicPath(t *testing.T) {
	func() {
		defer recoverTool("eidos/test")
		panic("kaboom")
	}()
}

// TestWriteProvider_WritesFiles asserts writeProvider emits files into dir.
func TestWriteProvider_WritesFiles(t *testing.T) {
	pir := &ir.ProviderIR{
		Name:    "petstore",
		Version: "1.0.0",
		Resources: []ir.ResourceIR{{
			Name:     "pet",
			TypeName: "pet",
			Schema: ir.ObjectSchemaIR{Attributes: []ir.AttributeIR{
				{Name: "id", Required: true, Schema: ir.SchemaIR{Type: ir.TypeString}},
			}},
			CRUDMapping: ir.CRUDMappingIR{
				Create: ir.OperationMappingIR{Method: "POST", PathTemplate: "/pets"},
				Read:   ir.OperationMappingIR{Method: "GET", PathTemplate: "/pets/{id}"},
				Delete: ir.OperationMappingIR{Method: "DELETE", PathTemplate: "/pets/{id}"},
			},
		}},
	}
	entries, err := writeProvider(t.TempDir(), pir)
	if err != nil {
		t.Fatalf("writeProvider error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected provider files to be planned")
	}
}
