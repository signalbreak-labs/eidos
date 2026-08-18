package naming

import (
	"reflect"
	"testing"
)

func TestSplitIdentifier(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "single word", in: "MyCloud", want: []string{"MyCloud"}},
		{name: "underscore", in: "foo_bar", want: []string{"foo", "bar"}},
		{name: "dash", in: "foo-bar", want: []string{"foo", "bar"}},
		{name: "dot", in: "foo.bar.baz", want: []string{"foo", "bar", "baz"}},
		{name: "spaces", in: "  foo bar  ", want: []string{"foo", "bar"}},
		{name: "mixed separators", in: "foo-bar_baz.qux", want: []string{"foo", "bar", "baz", "qux"}},
		{name: "leading and trailing", in: "__foo__", want: []string{"foo"}},
		{name: "consecutive separators", in: "a--b", want: []string{"a", "b"}},
		// Documented behavior: camelCase and digit/letter boundaries are NOT
		// split (SnakeCase("MyCloud") == "mycloud", "foo2bar" stays one word).
		{name: "camelCase not split", in: "fooBar", want: []string{"fooBar"}},
		{name: "digits kept", in: "id123", want: []string{"id123"}},
		{name: "all separators", in: "---", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitIdentifier(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitIdentifier(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPascalCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "already pascal", in: "MyCloud", want: "MyCloud"},
		{name: "lower single", in: "foo", want: "Foo"},
		{name: "snake", in: "foo_bar", want: "FooBar"},
		{name: "kebab", in: "foo-bar-baz", want: "FooBarBaz"},
		{name: "dots", in: "a.b", want: "AB"},
		{name: "mixed", in: "api_v2-client", want: "ApiV2Client"},
		{name: "digits", in: "2fa", want: "2fa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PascalCase(tt.in); got != tt.want {
				t.Errorf("PascalCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeGoIdentifier(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "X"},
		{name: "valid", in: "Foo", want: "Foo"},
		{name: "leading digit", in: "2fa", want: "X2fa"},
		{name: "non-letter rune", in: "_foo", want: "X_foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeGoIdentifier(tt.in); got != tt.want {
				t.Errorf("SanitizeGoIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGoTypeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "pet", want: "Pet"},
		{name: "snake", in: "reboot_server", want: "RebootServer"},
		{name: "hostile leading digit", in: "2fa", want: "X2fa"},
		{name: "hostile separators only", in: "---", want: "X"},
		{name: "empty", in: "", want: "X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GoTypeName(tt.in); got != tt.want {
				t.Errorf("GoTypeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGoFieldName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "display_name", want: "DisplayName"},
		{name: "hostile leading digit", in: "2fa", want: "X2fa"},
		{name: "hostile separators only", in: "---", want: "X"},
		{name: "empty", in: "", want: "X"},
		{name: "already camel", in: "fooBar", want: "FooBar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GoFieldName(tt.in); got != tt.want {
				t.Errorf("GoFieldName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCamelCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "pascal", in: "FooBar", want: "fooBar"},
		{name: "snake", in: "foo_bar", want: "fooBar"},
		{name: "single", in: "Foo", want: "foo"},
		{name: "acronym first", in: "APIClient", want: "aPIClient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CamelCase(tt.in); got != tt.want {
				t.Errorf("CamelCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSnakeCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		// H-7 regression: camelCase is deliberately NOT split, so "MyCloud"
		// stays a single word and lowercases whole (fooBar also stays one word).
		{name: "pascal not split", in: "MyCloud", want: "mycloud"},
		{name: "camel not split", in: "fooBar", want: "foobar"},
		{name: "snake already", in: "foo_bar", want: "foo_bar"},
		{name: "kebab", in: "foo-bar-baz", want: "foo_bar_baz"},
		{name: "mixed case snake", in: "Foo_Bar", want: "foo_bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SnakeCase(tt.in); got != tt.want {
				t.Errorf("SnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
