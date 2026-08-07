package transformer

import (
	"reflect"
	"testing"
)

func TestNormalizeOperationSecurityInheritsGlobal(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	got := NormalizeOperationSecurity(global, nil, nil)
	want := []SecurityRequirement{{"api_key": {}}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global inheritance mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationSecurityPathOverridesGlobal(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	pathItem := []SecurityRequirement{{"oauth2": {"read"}}}
	got := NormalizeOperationSecurity(global, pathItem, nil)
	want := []SecurityRequirement{{"oauth2": {"read"}}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("path override mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationSecurityOperationOverridesPath(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	pathItem := []SecurityRequirement{{"oauth2": {"read"}}}
	operation := []SecurityRequirement{{"basic": {}}}
	got := NormalizeOperationSecurity(global, pathItem, operation)
	want := []SecurityRequirement{{"basic": {}}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation override mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestNormalizeOperationSecurityOperationEmptyMeansNone(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	operation := []SecurityRequirement{}
	got := NormalizeOperationSecurity(global, nil, operation)
	if got != nil {
		t.Fatalf("explicit empty operation security should yield nil, got %#v", got)
	}
}

func TestNormalizeOperationSecurityPathEmptyOverridesGlobal(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	pathItem := []SecurityRequirement{}
	got := NormalizeOperationSecurity(global, pathItem, nil)
	if got != nil {
		t.Fatalf("explicit empty path security should yield nil, got %#v", got)
	}
}

func TestNormalizeOperationSecurityMultipleRequirements(t *testing.T) {
	global := []SecurityRequirement{
		{"api_key": {}},
		{"oauth2": {"read", "write"}},
	}
	got := NormalizeOperationSecurity(global, nil, nil)

	if len(got) != 2 {
		t.Fatalf("expected 2 requirements, got %#v", got)
	}
	if !reflect.DeepEqual(got, global) {
		t.Fatalf("multiple requirements mismatch:\ngot:  %#v\nwant: %#v", got, global)
	}
}

func TestNormalizeOperationSecurityReturnsCopy(t *testing.T) {
	global := []SecurityRequirement{{"api_key": {}}}
	got := NormalizeOperationSecurity(global, nil, nil)
	got[0]["api_key"] = []string{"unexpected"}

	if reflect.DeepEqual(global[0]["api_key"], []string{"unexpected"}) {
		t.Fatalf("NormalizeOperationSecurity returned a slice that shares backing array with input")
	}
}

func TestNormalizeOperationSecurityAllNil(t *testing.T) {
	got := NormalizeOperationSecurity(nil, nil, nil)
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
