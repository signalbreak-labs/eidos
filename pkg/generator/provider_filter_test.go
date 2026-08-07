package generator

import (
	"strings"
	"testing"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

func TestFilterProviderIR_IncludeExclude(t *testing.T) {
	provider := &ir.ProviderIR{
		Name: "test",
		Resources: []ir.ResourceIR{
			{Name: "pet"},
			{Name: "owner"},
			{Name: "admin_user"},
		},
		DataSources: []ir.DataSourceIR{
			{Name: "pet"},
			{Name: "stats"},
		},
	}

	filter := ProviderFilter{
		Resources: ConstructFilter{
			Include: []string{"pet", "owner"},
			Exclude: []string{"admin_*"},
		},
		DataSources: ConstructFilter{
			Exclude: []string{"stats"},
		},
	}

	got := FilterProviderIR(provider, filter)
	if len(got.Resources) != 2 || got.Resources[0].Name != "pet" || got.Resources[1].Name != "owner" {
		t.Errorf("unexpected resources: %+v", got.Resources)
	}
	if len(got.DataSources) != 1 || got.DataSources[0].Name != "pet" {
		t.Errorf("unexpected data sources: %+v", got.DataSources)
	}

	// Original provider must be unchanged.
	if len(provider.Resources) != 3 {
		t.Errorf("original provider was mutated")
	}
}

func TestConstructFilter_EmptyKeepsAll(t *testing.T) {
	f := ConstructFilter{}
	for _, name := range []string{"a", "b", "c"} {
		if !f.matches(name) {
			t.Errorf("expected %q to match empty filter", name)
		}
	}
}

func TestConstructFilter_ExcludeOnly(t *testing.T) {
	f := ConstructFilter{Exclude: []string{"admin_*"}}
	if !f.matches("pet") {
		t.Error("expected pet to match")
	}
	if f.matches("admin_user") {
		t.Error("expected admin_user to be excluded")
	}
}

func TestConstructFilter_IncludeOnly(t *testing.T) {
	f := ConstructFilter{Include: []string{"pet*"}}
	if !f.matches("pet") {
		t.Error("expected pet to match")
	}
	if !f.matches("pets") {
		t.Error("expected pets to match")
	}
	if f.matches("owner") {
		t.Error("expected owner to be excluded")
	}
}

func TestFilterProviderIR_NilProvider(t *testing.T) {
	if FilterProviderIR(nil, ProviderFilter{}) != nil {
		t.Error("expected nil provider to remain nil")
	}
}

// TestProviderFilter_Validate_RejectsInvalidPattern locks in the M-57 fix: a
// malformed include/exclude pattern (an unmatched "[") is rejected by Validate
// with a diagnostic naming the family and pattern, instead of silently
// matching nothing and filtering out an entire construct family.
func TestProviderFilter_Validate_RejectsInvalidPattern(t *testing.T) {
	cases := []struct {
		name   string
		filter ProviderFilter
		want   string
	}{
		{
			name:   "invalid include in resources",
			filter: ProviderFilter{Resources: ConstructFilter{Include: []string{"["}}},
			want:   "invalid resources include pattern",
		},
		{
			name:   "invalid exclude in data_sources",
			filter: ProviderFilter{DataSources: ConstructFilter{Exclude: []string{"["}}},
			want:   "invalid data_sources exclude pattern",
		},
		{
			name:   "invalid include in actions",
			filter: ProviderFilter{Actions: ConstructFilter{Include: []string{"a["}}},
			want:   "invalid actions include pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.filter.Validate()
			if err == nil {
				t.Fatal("expected error for invalid pattern, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

// TestProviderFilter_Validate_AcceptsValidPatterns confirms valid patterns and
// empty patterns do not produce errors (M-57).
func TestProviderFilter_Validate_AcceptsValidPatterns(t *testing.T) {
	filter := ProviderFilter{
		Resources:   ConstructFilter{Include: []string{"pet*", "owner"}, Exclude: []string{"admin_*"}},
		DataSources: ConstructFilter{Include: []string{""}},
		Actions:     ConstructFilter{Exclude: []string{"a", "b*"}},
	}
	if err := filter.Validate(); err != nil {
		t.Fatalf("expected no error for valid patterns, got: %v", err)
	}
}

// TestConstructFilter_MatchesInvalidPatternIsFalse confirms matchPattern's
// defensive fallback: even without Validate, a malformed pattern returns
// false rather than panicking (M-57).
func TestConstructFilter_MatchesInvalidPatternIsFalse(t *testing.T) {
	f := ConstructFilter{Include: []string{"["}}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("matches panicked on invalid pattern: %v", rec)
		}
	}()
	if f.matches("pet") {
		t.Error("expected invalid include pattern to not match")
	}
}
