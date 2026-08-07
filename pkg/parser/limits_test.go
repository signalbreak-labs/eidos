package parser

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDefaultLimits(t *testing.T) {
	l := DefaultLimits()
	if l.MaxDepth <= 0 {
		t.Fatalf("expected positive MaxDepth, got %d", l.MaxDepth)
	}
	if l.MaxMemoryBytes <= 0 {
		t.Fatalf("expected positive MaxMemoryBytes, got %d", l.MaxMemoryBytes)
	}
}

func TestBudgetDepthLimit(t *testing.T) {
	b := NewBudget(Limits{MaxDepth: 3})
	for i := 0; i < 3; i++ {
		if err := b.Enter(1); err != nil {
			t.Fatalf("unexpected depth error at frame %d: %v", i, err)
		}
	}
	if err := b.Enter(1); err == nil {
		t.Fatalf("expected depth limit error")
	}
}

func TestBudgetMemoryLimit(t *testing.T) {
	b := NewBudget(Limits{MaxMemoryBytes: 100})
	if err := b.Account(50); err != nil {
		t.Fatalf("unexpected memory error: %v", err)
	}
	if err := b.Account(60); err == nil {
		t.Fatalf("expected memory budget error")
	}
}

// generateNestedSchema returns an OpenAPI 3.0 document with a schema nested
// depth levels deep. The nesting is non-circular so conversion should succeed.
func generateNestedSchema(depth int) []byte {
	var sb strings.Builder
	sb.WriteString(`openapi: "3.0.3"
info:
  title: Nested
  version: "1.0"
paths: {}
components:
  schemas:
    Root:
`)
	indent := strings.Repeat("      ", 1)
	for i := 0; i < depth; i++ {
		sb.WriteString(indent)
		sb.WriteString("type: object\n")
		sb.WriteString(indent)
		sb.WriteString("properties:\n")
		sb.WriteString(indent)
		sb.WriteString("  child:\n")
		indent += "        "
	}
	sb.WriteString(indent)
	sb.WriteString("type: string\n")
	return []byte(sb.String())
}

func TestConvertV30DeeplyNestedSchema(t *testing.T) {
	data := generateNestedSchema(200)
	root, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV30(root)
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error diagnostic: %v", d)
		}
	}
	if spec == nil || spec.Components == nil || spec.Components.Schemas["Root"] == nil {
		t.Fatalf("Root schema missing")
	}
}

func TestConvertV30DepthLimit(t *testing.T) {
	data := generateNestedSchema(20)
	root, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	_, diags, err := ConvertV30(root, WithLimits(Limits{MaxDepth: 5, MaxMemoryBytes: 0}))
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !hasDiagSummary(diags, "Resource limit reached during schema scan") {
		t.Fatalf("expected stopped-scan diagnostic, got %v", diags)
	}
}

func TestConvertV30MemoryBudget(t *testing.T) {
	data := generateNestedSchema(20)
	root, err := LoadFile("deep.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	_, diags, err := ConvertV30(root, WithLimits(Limits{MaxDepth: 0, MaxMemoryBytes: 1}))
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !hasDiagSummary(diags, "Resource limit reached during schema scan") {
		t.Fatalf("expected stopped-scan diagnostic, got %v", diags)
	}
}

func hasDiagSummary(diags []Diagnostic, summary string) bool {
	for _, d := range diags {
		if d.Summary == summary {
			return true
		}
	}
	return false
}

// generateWideSpec returns an OpenAPI 3.0 document with many top-level schemas.
func generateWideSpec(count int) []byte {
	var sb strings.Builder
	sb.WriteString(`openapi: "3.0.3"
info:
  title: Wide
  version: "1.0"
paths: {}
components:
  schemas:
`)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "    Schema%d:\n      type: object\n", i)
	}
	return []byte(sb.String())
}

func TestConvertV30MemoryBudgetWide(t *testing.T) {
	data := generateWideSpec(1000)
	root, err := LoadFile("wide.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	_, diags, err := ConvertV30(root, WithLimits(Limits{MaxDepth: 0, MaxMemoryBytes: 1024}))
	if err != nil {
		t.Fatalf("ConvertV30: %v", err)
	}
	if !hasDiagSummary(diags, "Resource limit reached during schema scan") {
		t.Fatalf("expected stopped-scan diagnostic, got %v", diags)
	}
}

func TestConvertV31CircularRefsProduceOpaque(t *testing.T) {
	data := []byte(`openapi: "3.1.0"
info:
  title: Test
  version: "1.0"
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        next:
          $ref: '#/components/schemas/Node'
`)
	root, err := LoadFile("cycle31.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV31(root, WithLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("ConvertV31: %v", err)
	}
	if !hasDiagSummary(diags, "Circular schema reference") {
		t.Fatalf("expected circular ref diagnostic, got %v", diags)
	}
	if !spec.Components.Schemas["Node"].Properties["next"].Opaque {
		t.Fatalf("expected Node.next to be Opaque")
	}
}

func TestConvertV2CircularRefsProduceOpaqueWithLimits(t *testing.T) {
	data := []byte(`swagger: "2.0"
info:
  title: Test
  version: "1.0"
paths: {}
definitions:
  A:
    type: object
    properties:
      b:
        $ref: '#/definitions/B'
  B:
    type: object
    properties:
      a:
        $ref: '#/definitions/A'
`)
	root, err := LoadFile("cycle2.yaml", data)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	spec, diags, err := ConvertV2(root, WithLimits(DefaultLimits()))
	if err != nil {
		t.Fatalf("ConvertV2: %v", err)
	}
	if !hasDiagSummary(diags, "Circular schema reference") {
		t.Fatalf("expected circular ref diagnostic, got %v", diags)
	}
	if !spec.Components.Schemas["A"].Properties["b"].Opaque {
		t.Fatalf("expected A.b to be Opaque")
	}
}

func TestLoadJSONDepthLimit(t *testing.T) {
	depth := maxNestingDepth + 10
	data := []byte(strings.Repeat("[", depth) + strings.Repeat("]", depth))
	_, err := LoadFile("deep.json", data)
	if err == nil {
		t.Fatalf("expected depth-limit error for deeply nested JSON")
	}
	if !errors.Is(err, ErrMaxNestingDepth) {
		t.Fatalf("expected ErrMaxNestingDepth, got %v", err)
	}
}

func TestLoadYAMLDepthLimit(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < maxNestingDepth+10; i++ {
		sb.WriteString(strings.Repeat("  ", i))
		sb.WriteString("key:\n")
	}
	_, err := LoadFile("deep.yaml", []byte(sb.String()))
	if err == nil {
		t.Fatalf("expected depth-limit error for deeply nested YAML")
	}
	if !errors.Is(err, ErrMaxNestingDepth) {
		t.Fatalf("expected ErrMaxNestingDepth, got %v", err)
	}
}

func TestLoadYAMLFlowCollectionDepthLimit(t *testing.T) {
	depth := maxNestingDepth + 10
	data := []byte("key: " + strings.Repeat("[", depth) + strings.Repeat("]", depth) + "\n")
	_, err := LoadFile("flow.yaml", data)
	if err == nil {
		t.Fatalf("expected depth-limit error for deeply nested YAML flow collection")
	}
	if !errors.Is(err, ErrMaxNestingDepth) {
		t.Fatalf("expected ErrMaxNestingDepth, got %v", err)
	}
}
