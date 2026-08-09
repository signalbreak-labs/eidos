package schema

import (
	"reflect"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestIsPrimitiveSchema(t *testing.T) {
	tests := []struct {
		name string
		s    ir.SchemaIR
		want bool
	}{
		{name: "string", s: ir.SchemaIR{Type: ir.TypeString}, want: true},
		{name: "int", s: ir.SchemaIR{Type: ir.TypeInt}, want: true},
		{name: "float", s: ir.SchemaIR{Type: ir.TypeFloat}, want: true},
		{name: "bool", s: ir.SchemaIR{Type: ir.TypeBool}, want: true},
		{name: "dynamic", s: ir.SchemaIR{Type: ir.TypeDynamic}, want: true},
		{name: "null is not primitive", s: ir.SchemaIR{Type: ir.TypeNull}, want: false},
		{name: "object is not primitive", s: ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("x", true)}}, want: false},
		{name: "empty is not primitive", s: ir.SchemaIR{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPrimitiveSchema(tt.s); got != tt.want {
				t.Errorf("IsPrimitiveSchema(%+v) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestIsObjectLike(t *testing.T) {
	if !IsObjectLike(ir.SchemaIR{Attributes: []ir.AttributeIR{stringAttr("x", true)}}) {
		t.Error("schema with attributes should be object-like")
	}
	if !IsObjectLike(ir.SchemaIR{Blocks: []ir.BlockIR{{Name: "b"}}}) {
		t.Error("schema with blocks should be object-like")
	}
	if IsObjectLike(ir.SchemaIR{}) {
		t.Error("empty schema should not be object-like")
	}
	if IsObjectLike(ir.SchemaIR{Type: ir.TypeString}) {
		t.Error("primitive schema should not be object-like")
	}
}

func TestSkipAttrForModel(t *testing.T) {
	if SkipAttrForModel(ir.AttributeIR{Name: "anything", Schema: ir.SchemaIR{Type: ir.TypeString}}) {
		t.Error("SkipAttrForModel is a no-op hook and must return false")
	}
}

func TestResolveFieldNames(t *testing.T) {
	t.Run("no collisions passes through", func(t *testing.T) {
		attrs := []ir.AttributeIR{stringAttr("name", true), stringAttr("age", true)}
		got := ResolveFieldNames(attrs)
		want := map[string]string{"name": "Name", "age": "Age"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveFieldNames = %v, want %v", got, want)
		}
	})
	t.Run("H-7 collision gets numeric suffix", func(t *testing.T) {
		// "foo_bar" and "fooBar" both normalize to FooBar; the second gets
		// "FooBar2".
		attrs := []ir.AttributeIR{stringAttr("foo_bar", true), stringAttr("fooBar", true)}
		got := ResolveFieldNames(attrs)
		want := map[string]string{"foo_bar": "FooBar", "fooBar": "FooBar2"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveFieldNames = %v, want %v", got, want)
		}
	})
	t.Run("triple collision increments", func(t *testing.T) {
		attrs := []ir.AttributeIR{stringAttr("a_b", true), stringAttr("aB", true), stringAttr("a-b", true)}
		got := ResolveFieldNames(attrs)
		if got["a_b"] != "AB" || got["aB"] != "AB2" || got["a-b"] != "AB3" {
			t.Errorf("ResolveFieldNames = %v, want AB/AB2/AB3", got)
		}
	})
	t.Run("real name colliding with suffix is skipped over", func(t *testing.T) {
		// A real attribute "foo_bar_2" claims FooBar2 first; the later
		// colliding "fooBar" must keep incrementing past it to FooBar3.
		attrs := []ir.AttributeIR{
			stringAttr("foo_bar", true),
			stringAttr("foo_bar_2", true),
			stringAttr("fooBar", true),
		}
		got := ResolveFieldNames(attrs)
		if got["foo_bar"] != "FooBar" || got["foo_bar_2"] != "FooBar2" || got["fooBar"] != "FooBar3" {
			t.Errorf("ResolveFieldNames = %v, want FooBar/FooBar2/FooBar3", got)
		}
	})
}

func TestResolvedFieldName(t *testing.T) {
	scope := map[string]string{"foo_bar": "FooBar", "fooBar": "FooBar2"}
	if got := resolvedFieldName(scope, ir.AttributeIR{Name: "foo_bar"}); got != "FooBar" {
		t.Errorf("resolvedFieldName(foo_bar) = %q, want FooBar", got)
	}
	// Fallback for names absent from the scope.
	if got := resolvedFieldName(scope, ir.AttributeIR{Name: "plain_name"}); got != "PlainName" {
		t.Errorf("resolvedFieldName(plain_name) = %q, want PlainName", got)
	}
}
