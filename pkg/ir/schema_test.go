package ir

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNewDefaultValues(t *testing.T) {
	cases := []struct {
		name string
		got  *any
		want any
	}{
		{"int", NewDefaultInt(42), any(int64(42))},
		{"int64", NewDefaultInt64(42), any(int64(42))},
		{"int64-big-precision", NewDefaultInt64(9007199254740993), any(int64(9007199254740993))},
		{"float64", NewDefaultFloat64(3.14), any(3.14)},
		{"string", NewDefaultString("hello"), any("hello")},
		{"bool", NewDefaultBool(true), any(true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatal("expected non-nil pointer")
			}
			if !reflect.DeepEqual(*tc.got, tc.want) {
				t.Errorf("got %v, want %v", *tc.got, tc.want)
			}
		})
	}
}

func TestObjectSchemaIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ObjectSchemaIR{
		Attributes: []AttributeIR{
			{
				Name:        "id",
				Schema:      SchemaIR{Type: TypeString, Required: true},
				Required:    true,
				Description: "Unique identifier.",
			},
			{
				Name:   "name",
				Schema: SchemaIR{Type: TypeString},
			},
		},
		Blocks: []BlockIR{
			{
				Name:        "metadata",
				NestingMode: NestingSingle,
			},
		},
	})

	assertJSONRoundTrip(t, ObjectSchemaIR{})
}

func TestAttributeIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, AttributeIR{
		Name:                "status",
		Schema:              SchemaIR{Type: TypeString, EnumValues: []any{"pending", "running", "done"}},
		Description:         "The resource status.",
		MarkdownDescription: "The resource **status**.",
		Required:            true,
		Validators: []ValidatorIR{
			{Type: "stringvalidator.OneOf", Args: []string{"pending", "running", "done"}},
		},
	})

	assertJSONRoundTrip(t, AttributeIR{
		Name:               "count",
		Schema:             SchemaIR{Type: TypeInt, Minimum: floatPtr(0), Maximum: floatPtr(100)},
		Optional:           true,
		Computed:           false,
		Sensitive:          false,
		WriteOnly:          true,
		Deprecated:         true,
		DeprecationMessage: "Use limit instead.",
		Default:            NewDefaultFloat64(10),
		PlanModifiers: []PlanModifierIR{
			{Type: "int64planmodifier.RequiresReplaceIfConfigured"},
		},
	})

	// A collection attribute carries its element type on Collection.ElementType,
	// not on the outer schema's Type; setting both is a state Validate() rejects,
	// so the outer Type is omitted here (L-65).
	assertJSONRoundTrip(t, AttributeIR{
		Name:     "tags",
		Schema:   SchemaIR{Collection: &CollectionType{Kind: Set, ElementType: SchemaIR{Type: TypeString}}},
		Optional: true,
	})
}

func TestBlockIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, BlockIR{
		Name: "metadata",
		Schema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "created_at", Schema: SchemaIR{Type: TypeString, Computed: true}},
			},
		},
		NestingMode: NestingSingle,
		Description: "Read-only metadata block.",
	})

	assertJSONRoundTrip(t, BlockIR{
		Name: "endpoints",
		Schema: ObjectSchemaIR{
			Attributes: []AttributeIR{
				{Name: "url", Schema: SchemaIR{Type: TypeString}},
			},
		},
		NestingMode:        NestingList,
		MinItems:           int64Ptr(1),
		MaxItems:           int64Ptr(10),
		Deprecated:         true,
		DeprecationMessage: "Use individual endpoint resources instead.",
	})

	assertJSONRoundTrip(t, BlockIR{
		Name:        "labels",
		NestingMode: NestingSet,
	})
}

func TestBlockNestingModeRoundTrip(t *testing.T) {
	for _, mode := range []BlockNestingMode{NestingSingle, NestingList, NestingSet} {
		assertJSONRoundTrip(t, mode)
	}
}

func TestDefaultIntRoundTrip(t *testing.T) {
	// Integer defaults are stored as int64 to preserve precision above 2^53.
	// encoding/json decodes numbers into float64 when unmarshalling into `any`,
	// so a DeepEqual round-trip cannot hold; instead assert the JSON output
	// carries the exact integer and that the decoded value is numerically
	// equal.
	big := int64(9007199254740993) // 2^53 + 1, not representable as float64
	for _, tc := range []struct {
		name string
		got  *any
	}{
		{"int64", NewDefaultInt64(42)},
		{"int64-big", NewDefaultInt64(big)},
		{"int", NewDefaultInt(7)},
		{"int-big", NewDefaultInt(9007199254740993)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := SchemaIR{Type: TypeInt, Default: tc.got}
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded SchemaIR
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			// The exact integer must survive the marshal step for large values
			// that float64 cannot represent — this is the M-53 guarantee: the
			// in-memory int64 default is emitted to JSON without precision loss.
			if tc.name == "int64-big" || tc.name == "int-big" {
				if !strings.Contains(string(data), "9007199254740993") {
					t.Fatalf("precision lost in JSON output: %s", string(data))
				}
				// Decoding into `any` necessarily yields float64 (an
				// encoding/json limitation), so the big value cannot survive
				// that step; stop here.
				return
			}
			// For small values the decoded value must be numerically equal to
			// the original.
			if decoded.Default == nil {
				t.Fatalf("decoded Default is nil")
			}
			orig, _ := (*tc.got).(int64)
			switch d := (*decoded.Default).(type) {
			case float64:
				if int64(d) != orig {
					t.Fatalf("decoded %v != original %v", d, orig)
				}
			case int64:
				if d != orig {
					t.Fatalf("decoded %v != original %v", d, orig)
				}
			default:
				t.Fatalf("decoded Default has unexpected type %T", d)
			}
		})
	}
}
