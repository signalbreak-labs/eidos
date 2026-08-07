package transformer

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

func TestMatchName(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"pet", "pet", true},
		{"pet", "pets", false},
		{"pet*", "pets", true},
		{"pet*", "pet", true},
		{"*pet", "mypet", true},
		{"*pet", "pets", false},
		{"*pet*", "mypets", true},
		{"p?t", "pet", true},
		{"p?t", "pt", false},
		{"p?t", "pest", false},
		{"*", "anything", true},
		{"*", "", true},
		{"", "", true},
		{"", "x", false},
		{"admin_*", "admin_user", true},
		{"admin_*", "admin", false},
		{"admin_*", "user_admin", false},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+"_"+tc.name, func(t *testing.T) {
			if got := MatchName(tc.pattern, tc.name); got != tc.want {
				t.Errorf("MatchName(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}

// TestMatchNameNoExponentialBacktracking locks in the L-95 fix: a pathological
// pattern with many '*' separated by literals must complete in polynomial time
// against a long non-matching name. The recursive matcher was exponential
// here; the iterative star-backtracking matcher is O(len(pattern)*len(name)).
func TestMatchNameNoExponentialBacktracking(t *testing.T) {
	pattern := "*a*a*a*a*a*b"
	name := strings.Repeat("a", 2000) // long, non-matching (no 'b')
	if MatchName(pattern, name) {
		t.Errorf("MatchName(%q, %q...) = true, want false", pattern, name[:8])
	}
}

func TestShouldInclude(t *testing.T) {
	cases := []struct {
		name  string
		input string
		cfg   config.ResourceGenerationConfig
		want  bool
	}{
		{
			name:  "no filters passes everything",
			input: "pet",
			cfg:   config.ResourceGenerationConfig{},
			want:  true,
		},
		{
			name:  "allow-list match",
			input: "pet",
			cfg:   config.ResourceGenerationConfig{Include: []string{"pet", "owner"}},
			want:  true,
		},
		{
			name:  "allow-list miss",
			input: "pet",
			cfg:   config.ResourceGenerationConfig{Include: []string{"owner"}},
			want:  false,
		},
		{
			name:  "deny-list match",
			input: "admin_user",
			cfg:   config.ResourceGenerationConfig{Exclude: []string{"admin_*"}},
			want:  false,
		},
		{
			name:  "deny-list miss",
			input: "pet",
			cfg:   config.ResourceGenerationConfig{Exclude: []string{"admin_*"}},
			want:  true,
		},
		{
			name:  "deny-list overrides allow-list",
			input: "admin_user",
			cfg:   config.ResourceGenerationConfig{Include: []string{"*"}, Exclude: []string{"admin_*"}},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// L-113: input is an explicit field rather than picked by string-
			// matching the test name, so renaming a case cannot silently change
			// which input is exercised.
			if got := ShouldInclude(tc.input, tc.cfg); got != tc.want {
				t.Errorf("ShouldInclude(%q, %+v) = %v, want %v", tc.input, tc.cfg, got, tc.want)
			}
		})
	}
}

func TestFilterResources(t *testing.T) {
	names := []string{"pet", "owner", "admin_user", "admin_role"}
	cfg := config.ResourceGenerationConfig{
		Include: []string{"*"},
		Exclude: []string{"admin_*"},
		Package: "core",
		Packages: []config.PackageRuleConfig{
			{Name: "pets", Include: []string{"pet*"}},
			{Name: "people", Include: []string{"owner*"}},
		},
	}

	result := FilterResources(names, cfg)

	wantIncluded := []string{"pet", "owner"}
	if len(result.Included) != len(wantIncluded) {
		t.Fatalf("included = %v, want %v", result.Included, wantIncluded)
	}
	for i, w := range wantIncluded {
		if result.Included[i] != w {
			t.Errorf("included[%d] = %q, want %q", i, result.Included[i], w)
		}
	}

	if result.Packages["pet"] != "pets" {
		t.Errorf("pet package = %q, want pets", result.Packages["pet"])
	}
	if result.Packages["owner"] != "people" {
		t.Errorf("owner package = %q, want people", result.Packages["owner"])
	}
	for _, excluded := range []string{"admin_user", "admin_role"} {
		if _, ok := result.Packages[excluded]; ok {
			t.Errorf("excluded resource %q should not have a package", excluded)
		}
	}
}

func TestPackageFor_DefaultPackage(t *testing.T) {
	cfg := config.ResourceGenerationConfig{Package: "core"}
	if got := PackageFor("pet", cfg); got != "core" {
		t.Errorf("PackageFor(pet) = %q, want core", got)
	}
}

func TestPackageFor_RuleOverride(t *testing.T) {
	cfg := config.ResourceGenerationConfig{
		Package: "core",
		Packages: []config.PackageRuleConfig{
			{Name: "pets", Include: []string{"pet*"}},
		},
	}
	if got := PackageFor("pet", cfg); got != "pets" {
		t.Errorf("PackageFor(pet) = %q, want pets", got)
	}
	if got := PackageFor("owner", cfg); got != "core" {
		t.Errorf("PackageFor(owner) = %q, want core", got)
	}
}

func TestPackageFor_RuleExclude(t *testing.T) {
	cfg := config.ResourceGenerationConfig{
		Packages: []config.PackageRuleConfig{
			{Name: "pets", Include: []string{"pet*"}, Exclude: []string{"pet_store"}},
		},
	}
	if got := PackageFor("pet_store", cfg); got != "" {
		t.Errorf("PackageFor(pet_store) = %q, want empty", got)
	}
	if got := PackageFor("pet_food", cfg); got != "pets" {
		t.Errorf("PackageFor(pet_food) = %q, want pets", got)
	}
}

func TestFilterSpecOperations(t *testing.T) {
	spec := &parser.Spec{Paths: map[string]*parser.PathItem{
		"/pets": {
			Post: &parser.Operation{OperationID: "createPet"},
			Get:  &parser.Operation{OperationID: "listPets"},
		},
		"/pets/{id}": {
			Get:    &parser.Operation{OperationID: "getPet"},
			Delete: &parser.Operation{OperationID: "deletePet"},
		},
		"/admin/status": {
			Get: &parser.Operation{OperationID: "getAdminStatus"},
		},
	}}
	dropped := FilterSpecOperations(spec, []string{"getPet", "deletePet", "list*"}, nil)
	if dropped != 3 {
		t.Fatalf("expected 3 dropped, got %d", dropped)
	}
	if spec.Paths["/pets"].Post == nil || spec.Paths["/pets"].Get != nil {
		t.Fatalf("expected listPets dropped but create retained, got %+v", spec.Paths["/pets"])
	}
	if _, ok := spec.Paths["/pets/{id}"]; ok {
		t.Fatalf("expected /pets/{id} path removed (all ops dropped), got %+v", spec.Paths["/pets/{id}"])
	}
	if spec.Paths["/admin/status"] == nil {
		t.Fatalf("expected /admin/status retained")
	}
}
