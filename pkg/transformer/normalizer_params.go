package transformer

import (
	"fmt"
	"sort"
	"strings"
)

// OpenAPIParameter is a version-agnostic OpenAPI operation parameter used before the
// document is transformed into the Terraform-oriented IR.
type OpenAPIParameter struct {
	Ref         string
	Name        string
	In          string
	Description string
	Required    bool
	Schema      *Schema
	Deprecated  bool
}

// parameterKey returns a stable identifier for deduplicating parameters. OpenAPI
// allows the same name in different locations (for example a query "id" and a
// path "id"), so the key combines location and name.
func parameterKey(p OpenAPIParameter) string {
	return p.In + ":" + p.Name
}

// refName extracts the final component from a JSON Pointer style reference such
// as "#/components/parameters/petId", returning "petId". It performs JSON
// Pointer unescaping (~1 → /, ~0 → ~) so escaped keys such as
// "#/components/parameters/my~1param" resolve to "my/param". If the reference
// does not contain a separator the original value is returned after unescaping.
func refName(ref string) string {
	name := ref
	if i := strings.LastIndex(ref, "/"); i != -1 {
		name = ref[i+1:]
	}
	name = strings.ReplaceAll(name, "~1", "/")
	name = strings.ReplaceAll(name, "~0", "~")
	return name
}

// ResolveOpenAPIParameterRefs replaces parameter $ref entries with the referenced
// parameter from components. A nil components map means no references can be
// resolved and any ref produces an error. The resolved parameters are shallow
// copies; their Ref field is cleared to show they are now inline. Schema fields
// are shared pointers; callers should not mutate them.
func ResolveOpenAPIParameterRefs(params []OpenAPIParameter, components map[string]*OpenAPIParameter) ([]OpenAPIParameter, error) {
	resolved := make([]OpenAPIParameter, 0, len(params))
	for i, p := range params {
		if p.Ref == "" {
			resolved = append(resolved, p)
			continue
		}
		if len(components) == 0 {
			return nil, fmt.Errorf("parameter reference %q at index %d cannot be resolved: no components provided", p.Ref, i)
		}
		key := refName(p.Ref)
		target, ok := components[key]
		if !ok {
			return nil, fmt.Errorf("parameter reference %q at index %d cannot be resolved", p.Ref, i)
		}
		clone := *target
		clone.Ref = ""
		resolved = append(resolved, clone)
	}
	return resolved, nil
}

// MergeOpenAPIParameters combines path-level and operation-level parameters. Operation-
// level parameters override path-level parameters with the same name and
// location. The returned slice is sorted by location then name for deterministic
// output.
func MergeOpenAPIParameters(pathParams, opParams []OpenAPIParameter) []OpenAPIParameter {
	merged := make(map[string]OpenAPIParameter, len(pathParams)+len(opParams))
	for _, p := range pathParams {
		merged[parameterKey(p)] = p
	}
	for _, p := range opParams {
		merged[parameterKey(p)] = p
	}
	result := make([]OpenAPIParameter, 0, len(merged))
	for _, p := range merged {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].In != result[j].In {
			return result[i].In < result[j].In
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// NormalizeOperationOpenAPIParameters resolves any parameter references in path-level
// and operation-level parameters and merges them into a single list. Operation-
// level parameters override path-level parameters with the same name and location.
func NormalizeOperationOpenAPIParameters(pathItem *PathItem, op *OpenAPIOperation, components map[string]*OpenAPIParameter) ([]OpenAPIParameter, error) {
	if pathItem == nil {
		pathItem = &PathItem{}
	}
	if op == nil {
		op = &OpenAPIOperation{}
	}
	pathResolved, err := ResolveOpenAPIParameterRefs(pathItem.OpenAPIParameters, components)
	if err != nil {
		return nil, fmt.Errorf("path item parameters: %w", err)
	}
	opResolved, err := ResolveOpenAPIParameterRefs(op.OpenAPIParameters, components)
	if err != nil {
		return nil, fmt.Errorf("operation parameters: %w", err)
	}
	return MergeOpenAPIParameters(pathResolved, opResolved), nil
}
