package generator

import (
	"fmt"
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ConstructFilter describes an allow-list/deny-list pair used to select which
// named constructs (resources, data sources, actions, etc.) are included in a
// generated provider. Patterns use filepath.Match syntax; an empty include list
// keeps all constructs that are not explicitly excluded.
type ConstructFilter struct {
	Include []string
	Exclude []string
}

// ProviderFilter carries per-family construct filters parsed from
// generator.yaml. A nil or zero-value filter keeps every construct.
type ProviderFilter struct {
	Resources          ConstructFilter
	DataSources        ConstructFilter
	Actions            ConstructFilter
	EphemeralResources ConstructFilter
	ListResources      ConstructFilter
	Functions          ConstructFilter
}

// ProviderFilterFromConfig builds a ProviderFilter from the generation section
// of a loaded generator.yaml config.
func ProviderFilterFromConfig(cfg config.GenerationConfig) ProviderFilter {
	return ProviderFilter{
		Resources:          constructFilterFromConfig(cfg.Resources),
		DataSources:        constructFilterFromConfig(cfg.DataSources),
		Actions:            constructFilterFromConfig(cfg.Actions),
		EphemeralResources: constructFilterFromConfig(cfg.EphemeralResources),
		ListResources:      constructFilterFromConfig(cfg.ListResources),
		Functions:          constructFilterFromConfig(cfg.Functions),
	}
}

func constructFilterFromConfig(cfg config.ResourceGenerationConfig) ConstructFilter {
	return ConstructFilter{
		Include: cfg.Include,
		Exclude: cfg.Exclude,
	}
}

// Validate checks every include/exclude pattern in the filter is a syntactically
// valid filepath.Match pattern. family names the construct family in the error
// message. Without this check, a malformed pattern such as an unmatched "[" is
// treated by matchPattern as a non-match and silently filters out an entire
// construct family with no diagnostic (M-57).
func (f ConstructFilter) Validate(family string) error {
	for _, p := range f.Include {
		if err := validateFilterPattern(p); err != nil {
			return fmt.Errorf("invalid %s include pattern %q: %w", family, p, err)
		}
	}
	for _, p := range f.Exclude {
		if err := validateFilterPattern(p); err != nil {
			return fmt.Errorf("invalid %s exclude pattern %q: %w", family, p, err)
		}
	}
	return nil
}

// Validate checks every construct family's patterns. It returns the first
// invalid pattern found, naming the family and pattern (M-57).
func (f ProviderFilter) Validate() error {
	families := []struct {
		name string
		cf   ConstructFilter
	}{
		{"resources", f.Resources},
		{"data_sources", f.DataSources},
		{"actions", f.Actions},
		{"ephemeral_resources", f.EphemeralResources},
		{"list_resources", f.ListResources},
		{"functions", f.Functions},
	}
	for _, fam := range families {
		if err := fam.cf.Validate(fam.name); err != nil {
			return err
		}
	}
	return nil
}

// validateFilterPattern reports whether p is a syntactically valid filepath.Match
// pattern. An empty pattern is allowed (matchPattern treats it as a non-match).
func validateFilterPattern(p string) error {
	if p == "" {
		return nil
	}
	_, err := filepath.Match(p, "")
	return err
}

// FilterProviderIR returns a copy of the provider IR with constructs removed
// according to the supplied filter. The original IR is not modified.
func FilterProviderIR(provider *ir.ProviderIR, filter ProviderFilter) *ir.ProviderIR {
	if provider == nil {
		return nil
	}
	filtered := *provider
	filtered.Resources = filterResources(provider.Resources, filter.Resources)
	filtered.DataSources = filterDataSources(provider.DataSources, filter.DataSources)
	filtered.Actions = filterActions(provider.Actions, filter.Actions)
	filtered.EphemeralResources = filterEphemeralResources(provider.EphemeralResources, filter.EphemeralResources)
	filtered.ListResources = filterListResources(provider.ListResources, filter.ListResources)
	filtered.Functions = filterFunctions(provider.Functions, filter.Functions)
	return &filtered
}

func filterResources(items []ir.ResourceIR, f ConstructFilter) []ir.ResourceIR {
	out := make([]ir.ResourceIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func filterDataSources(items []ir.DataSourceIR, f ConstructFilter) []ir.DataSourceIR {
	out := make([]ir.DataSourceIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func filterActions(items []ir.ActionIR, f ConstructFilter) []ir.ActionIR {
	out := make([]ir.ActionIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func filterEphemeralResources(items []ir.EphemeralResourceIR, f ConstructFilter) []ir.EphemeralResourceIR {
	out := make([]ir.EphemeralResourceIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func filterListResources(items []ir.ListResourceIR, f ConstructFilter) []ir.ListResourceIR {
	out := make([]ir.ListResourceIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

func filterFunctions(items []ir.FunctionIR, f ConstructFilter) []ir.FunctionIR {
	out := make([]ir.FunctionIR, 0, len(items))
	for _, item := range items {
		if f.matches(item.Name) {
			out = append(out, item)
		}
	}
	return out
}

// matches reports whether a construct name passes the include/exclude rules.
func (f ConstructFilter) matches(name string) bool {
	if len(f.Include) > 0 {
		included := false
		for _, p := range f.Include {
			if matchPattern(p, name) {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}
	for _, p := range f.Exclude {
		if matchPattern(p, name) {
			return false
		}
	}
	return true
}

// matchPattern reports whether name matches the filepath.Match pattern. A
// malformed pattern returns false rather than panicking; invalid patterns are
// surfaced earlier by ProviderFilter.Validate so a typo does not silently
// filter out an entire construct family (M-57). This fallback is defensive for
// any caller that bypasses Validate.
func matchPattern(pattern, name string) bool {
	if pattern == "" {
		return false
	}
	matched, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	return matched
}
