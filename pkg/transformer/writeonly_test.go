package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestApplyWriteOnlyAttributesNil(t *testing.T) {
	// Should not panic.
	ApplyWriteOnlyAttributes(nil)
}

func TestApplyWriteOnlyAttributesEmpty(t *testing.T) {
	obj := &ir.ObjectSchemaIR{}
	ApplyWriteOnlyAttributes(obj)
	if len(obj.Attributes) != 0 || len(obj.Blocks) != 0 {
		t.Errorf("expected empty object schema to remain empty, got %+v", obj)
	}
}

func TestApplyWriteOnlyAttributesSimple(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "password",
				Required: true,
				Schema:   ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	if len(obj.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d: %+v", len(obj.Attributes), obj.Attributes)
	}

	wo := obj.Attributes[0]
	if wo.Name != "password_wo" {
		t.Errorf("write-only attribute name = %q, want %q", wo.Name, "password_wo")
	}
	if !wo.WriteOnly {
		t.Errorf("write-only attribute WriteOnly = false, want true")
	}
	if !wo.Schema.WriteOnly {
		t.Errorf("write-only schema WriteOnly = false, want true")
	}
	if !wo.Sensitive {
		t.Errorf("write-only attribute Sensitive = false, want true")
	}
	if !wo.Schema.Sensitive {
		t.Errorf("write-only schema Sensitive = false, want true")
	}
	if !wo.Required {
		t.Errorf("write-only attribute Required was changed")
	}

	companion := obj.Attributes[1]
	if companion.Name != "password_wo_version" {
		t.Errorf("companion name = %q, want %q", companion.Name, "password_wo_version")
	}
	if companion.Schema.Type != ir.TypeInt {
		t.Errorf("companion type = %q, want %q", companion.Schema.Type, ir.TypeInt)
	}
	if !companion.Optional {
		t.Errorf("companion Optional = false, want true")
	}
	if companion.Required {
		t.Errorf("companion Required = true, want false")
	}
}

func TestApplyWriteOnlyAttributesNonWriteOnlyUnchanged(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "name",
				Optional: true,
				Schema:   ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	if len(obj.Attributes) != 1 {
		t.Fatalf("expected 1 attribute, got %d", len(obj.Attributes))
	}
	if obj.Attributes[0].Name != "name" {
		t.Errorf("non-write-only attribute was renamed to %q", obj.Attributes[0].Name)
	}
	if obj.Attributes[0].WriteOnly {
		t.Errorf("non-write-only attribute marked WriteOnly")
	}
}

func TestApplyWriteOnlyAttributesAlreadySuffixed(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "password_wo",
				Optional: true,
				Schema:   ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	if len(obj.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(obj.Attributes))
	}
	if obj.Attributes[0].Name != "password_wo" {
		t.Errorf("already-suffixed attribute renamed to %q", obj.Attributes[0].Name)
	}
	if obj.Attributes[1].Name != "password_wo_version" {
		t.Errorf("companion name = %q, want %q", obj.Attributes[1].Name, "password_wo_version")
	}
}

func TestApplyWriteOnlyAttributesExistingCompanionNotDuplicated(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:     "password_wo",
				Optional: true,
				Schema:   ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
			},
			{
				Name:     "password_wo_version",
				Optional: true,
				Schema:   ir.SchemaIR{Type: ir.TypeInt},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	if len(obj.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d: %+v", len(obj.Attributes), obj.Attributes)
	}
}

func TestApplyWriteOnlyAttributesMultiple(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:   "password",
				Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
			},
			{
				Name:   "token",
				Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
			},
			{
				Name:   "name",
				Schema: ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	names := make([]string, len(obj.Attributes))
	for i, attr := range obj.Attributes {
		names[i] = attr.Name
	}

	want := []string{"password_wo", "password_wo_version", "token_wo", "token_wo_version", "name"}
	if len(names) != len(want) {
		t.Fatalf("expected %d attributes, got %d: %v", len(want), len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("attribute[%d].Name = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestApplyWriteOnlyAttributesNestedObject(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name: "credentials",
				Schema: ir.SchemaIR{
					Type: ir.TypeString,
					Attributes: []ir.AttributeIR{
						{
							Name:   "password",
							Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
						},
					},
				},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	nested := obj.Attributes[0].Schema.Attributes
	if len(nested) != 2 {
		t.Fatalf("expected 2 nested attributes, got %d: %+v", len(nested), nested)
	}
	if nested[0].Name != "password_wo" {
		t.Errorf("nested write-only name = %q, want %q", nested[0].Name, "password_wo")
	}
	if nested[1].Name != "password_wo_version" {
		t.Errorf("nested companion name = %q, want %q", nested[1].Name, "password_wo_version")
	}
}

func TestApplyWriteOnlyAttributesBlock(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Blocks: []ir.BlockIR{
			{
				Name: "credentials",
				Schema: ir.ObjectSchemaIR{
					Attributes: []ir.AttributeIR{
						{
							Name:   "password",
							Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
						},
					},
				},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	attrs := obj.Blocks[0].Schema.Attributes
	if len(attrs) != 2 {
		t.Fatalf("expected 2 block attributes, got %d: %+v", len(attrs), attrs)
	}
	if attrs[0].Name != "password_wo" {
		t.Errorf("block write-only name = %q, want %q", attrs[0].Name, "password_wo")
	}
	if attrs[1].Name != "password_wo_version" {
		t.Errorf("block companion name = %q, want %q", attrs[1].Name, "password_wo_version")
	}
}

func TestApplyWriteOnlyAttributesCollectionElement(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name: "secrets",
				Schema: ir.SchemaIR{
					Type: ir.TypeString,
					Collection: &ir.CollectionType{
						Kind: ir.List,
						ElementType: ir.SchemaIR{
							Type: ir.TypeString,
							Attributes: []ir.AttributeIR{
								{
									Name:   "value",
									Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
								},
							},
						},
					},
				},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	elemAttrs := obj.Attributes[0].Schema.Collection.ElementType.Attributes
	if len(elemAttrs) != 2 {
		t.Fatalf("expected 2 element attributes, got %d: %+v", len(elemAttrs), elemAttrs)
	}
	if elemAttrs[0].Name != "value_wo" {
		t.Errorf("collection element write-only name = %q, want %q", elemAttrs[0].Name, "value_wo")
	}
	if elemAttrs[1].Name != "value_wo_version" {
		t.Errorf("collection element companion name = %q, want %q", elemAttrs[1].Name, "value_wo_version")
	}
}

func TestApplyWriteOnlyAttributesUnionVariant(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name: "config",
				Schema: ir.SchemaIR{
					Union: &ir.UnionType{
						Kind: ir.OneOf,
						Variants: []ir.SchemaIR{
							{
								Type: ir.TypeString,
								Attributes: []ir.AttributeIR{
									{
										Name:   "password",
										Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	variantAttrs := obj.Attributes[0].Schema.Union.Variants[0].Attributes
	if len(variantAttrs) != 2 {
		t.Fatalf("expected 2 variant attributes, got %d: %+v", len(variantAttrs), variantAttrs)
	}
	if variantAttrs[0].Name != "password_wo" {
		t.Errorf("union variant write-only name = %q, want %q", variantAttrs[0].Name, "password_wo")
	}
	if variantAttrs[1].Name != "password_wo_version" {
		t.Errorf("union variant companion name = %q, want %q", variantAttrs[1].Name, "password_wo_version")
	}
}

func TestApplyWriteOnlyAttributesAttributeLevelFlag(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{
				Name:      "password",
				WriteOnly: true,
				Schema:    ir.SchemaIR{Type: ir.TypeString},
			},
		},
	}

	ApplyWriteOnlyAttributes(obj)

	if len(obj.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(obj.Attributes))
	}
	if obj.Attributes[0].Name != "password_wo" {
		t.Errorf("attribute name = %q, want %q", obj.Attributes[0].Name, "password_wo")
	}
	if !obj.Attributes[0].Schema.WriteOnly {
		t.Errorf("schema-level WriteOnly was not propagated")
	}
}

// TestApplyWriteOnlyAttributesRenameConflictDiagnostic locks in the L-112 fix:
// when a write-only attribute cannot be renamed to its _wo form because that
// name is already in use, the attribute keeps its original name (still
// WriteOnly), its companion becomes <name>_version instead of <name>_wo_version,
// and a warning diagnostic is emitted rather than silently violating the
// documented naming convention.
func TestApplyWriteOnlyAttributesRenameConflictDiagnostic(t *testing.T) {
	obj := &ir.ObjectSchemaIR{
		Attributes: []ir.AttributeIR{
			{Name: "password", WriteOnly: true, Schema: ir.SchemaIR{Type: ir.TypeString, WriteOnly: true}},
			{Name: "password_wo", Schema: ir.SchemaIR{Type: ir.TypeString}},
		},
	}

	var diags diagnostics.Diagnostics
	ApplyWriteOnlyAttributesWithDiagnostics(obj, &diags)

	// The write-only "password" keeps its name because "password_wo" is taken.
	if obj.Attributes[0].Name != "password" {
		t.Errorf("write-only attr name = %q, want %q (rename blocked)", obj.Attributes[0].Name, "password")
	}
	if !obj.Attributes[0].WriteOnly {
		t.Errorf("write-only attr should still be marked WriteOnly")
	}

	// The companion is password_version, not password_wo_version.
	var hasCompanion, hasWOSuffixCompanion bool
	for _, a := range obj.Attributes {
		if a.Name == "password_version" {
			hasCompanion = true
		}
		if a.Name == "password_wo_version" {
			hasWOSuffixCompanion = true
		}
	}
	if !hasCompanion {
		t.Errorf("expected a password_version companion attribute, got %v", obj.Attributes)
	}
	if hasWOSuffixCompanion {
		t.Errorf("did not expect password_wo_version companion when rename was blocked, got %v", obj.Attributes)
	}

	var warned bool
	for _, d := range diags {
		if d.Severity == diagnostics.Warning && strings.Contains(d.Detail, "password_wo") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a warning diagnostic about the blocked _wo rename, got %v", diags)
	}
}
