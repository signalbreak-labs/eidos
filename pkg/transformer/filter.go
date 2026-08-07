package transformer

import (
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

// FilterResult holds the outcome of applying a ResourceGenerationConfig to a
// set of construct names.
type FilterResult struct {
	// Included is the ordered list of names that passed the filter.
	Included []string
	// Packages maps each included name to its target sub-package. An empty
	// string means the root internal/provider package.
	Packages map[string]string
}

// FilterResources applies cfg to names and returns the filtered set plus any
// package assignments.
func FilterResources(names []string, cfg config.ResourceGenerationConfig) FilterResult {
	included := make([]string, 0, len(names))
	packages := make(map[string]string, len(names))

	for _, name := range names {
		if ShouldInclude(name, cfg) {
			included = append(included, name)
			packages[name] = PackageFor(name, cfg)
		}
	}

	return FilterResult{Included: included, Packages: packages}
}

// ShouldInclude applies the allow-list and deny-list in cfg to name.
//
// Excludes take precedence: if name matches any exclude pattern it is dropped.
// When include is non-empty, name must match at least one include pattern.
// When include is empty, all non-excluded names pass.
func ShouldInclude(name string, cfg config.ResourceGenerationConfig) bool {
	for _, p := range cfg.Exclude {
		if MatchName(p, name) {
			return false
		}
	}
	if len(cfg.Include) == 0 {
		return true
	}
	for _, p := range cfg.Include {
		if MatchName(p, name) {
			return true
		}
	}
	return false
}

// PackageFor returns the target sub-package for name. The first matching
// package rule wins; if no rule matches, cfg.Package is returned. An empty
// result keeps the construct in the root internal/provider package.
func PackageFor(name string, cfg config.ResourceGenerationConfig) string {
	for _, rule := range cfg.Packages {
		if matchesPackageRule(name, rule) {
			return strings.TrimSpace(rule.Name)
		}
	}
	return strings.TrimSpace(cfg.Package)
}

func matchesPackageRule(name string, rule config.PackageRuleConfig) bool {
	for _, p := range rule.Exclude {
		if MatchName(p, name) {
			return false
		}
	}
	if len(rule.Include) == 0 {
		return true
	}
	for _, p := range rule.Include {
		if MatchName(p, name) {
			return true
		}
	}
	return false
}

// FilterSpecOperations removes operations whose operation ID matches a skip
// pattern (or fails to match every include pattern) from spec.Paths. Paths left
// with no operations are removed. The spec is mutated in place; callers must
// compute the path-operation map after filtering so CRUD grouping and the
// per-operation pass both see the filtered set. Returns the number of
// operations dropped.
func FilterSpecOperations(spec *parser.Spec, skip, include []string) int {
	if spec == nil || (len(skip) == 0 && len(include) == 0) {
		return 0
	}
	dropped := 0
	for path, pi := range spec.Paths {
		if pi == nil {
			continue
		}
		slots := map[string]**parser.Operation{
			"get": &pi.Get, "put": &pi.Put, "post": &pi.Post,
			"delete": &pi.Delete, "patch": &pi.Patch,
			"options": &pi.Options, "head": &pi.Head, "trace": &pi.Trace,
		}
		for _, opPtr := range slots {
			op := *opPtr
			if op == nil || strings.TrimSpace(op.OperationID) == "" {
				continue
			}
			if operationExcluded(op.OperationID, skip, include) {
				*opPtr = nil
				dropped++
			}
		}
		if pi.Get == nil && pi.Put == nil && pi.Post == nil && pi.Delete == nil &&
			pi.Patch == nil && pi.Options == nil && pi.Head == nil && pi.Trace == nil {
			delete(spec.Paths, path)
		}
	}
	return dropped
}

// operationExcluded reports whether opID should be dropped given the skip and
// include patterns. Excludes take precedence; when include is non-empty the
// operation must match at least one include pattern to be kept.
func operationExcluded(opID string, skip, include []string) bool {
	for _, p := range skip {
		if MatchName(p, opID) {
			return true
		}
	}
	if len(include) == 0 {
		return false
	}
	for _, p := range include {
		if MatchName(p, opID) {
			return false
		}
	}
	return true
}

// MatchName reports whether name matches pattern using glob-style wildcards.
// It supports '*' (zero or more characters) and '?' (exactly one character).
// All other characters are matched literally and the comparison is
// case-sensitive.
func MatchName(pattern, name string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return name == ""
	}
	return matchRunes([]rune(pattern), []rune(name))
}

// matchRunes implements glob matching with '*' (any run, including empty) and
// '?' (single rune). It uses the iterative star-backtracking algorithm
// (O(len(pattern)*len(name)) time, O(1) space) rather than the recursive
// backtracking approach, which is exponential on pathological patterns such as
// "*a*a*a*a*a*b" against a long non-matching name (L-95). Patterns come from
// the user's own generator.yaml, so this is self-DoS only, but the iterative
// form avoids the worst case without changing matching semantics.
func matchRunes(pattern, name []rune) bool {
	pi, ni := 0, 0
	star, match := -1, 0
	for ni < len(name) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == name[ni]):
			pi++
			ni++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			match = ni
			pi++
		case star != -1:
			// Backtrack: let the most recent '*' consume one more rune and retry.
			pi = star + 1
			match++
			ni = match
		default:
			return false
		}
	}
	// Consume any trailing '*' that can match the empty tail of the name.
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
