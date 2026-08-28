package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/diagnostics"
)

// Version identifies the OpenAPI spec version detected by the parser.
type Version string

const (
	// VersionUnknown means no supported version could be detected.
	VersionUnknown Version = ""
	// Version2_0 is Swagger / OpenAPI 2.0 (swagger: "2.0").
	Version2_0 Version = "2.0"
	// Version3_0 is OpenAPI 3.0.x (openapi: "3.0.x").
	Version3_0 Version = "3.0"
	// Version3_1 is OpenAPI 3.1.x (openapi: "3.1.x").
	Version3_1 Version = "3.1"
)

// Severity classifies a diagnostic message.
//
// This is an alias for diagnostics.Severity so that parser-produced
// diagnostics share a single severity model with the rest of the project.
type Severity = diagnostics.Severity

const (
	// SeverityError blocks further processing.
	SeverityError Severity = diagnostics.Error
	// SeverityWarning allows processing to continue but should be reported.
	SeverityWarning Severity = diagnostics.Warning
	// SeverityInfo is a non-actionable observation.
	SeverityInfo Severity = diagnostics.Info
)

// Diagnostic is a source-located message produced during parsing.
//
// This is an alias for diagnostics.Diagnostic so that callers can collect
// parser diagnostics alongside diagnostics from other stages without manual
// conversion.
type Diagnostic = diagnostics.Diagnostic

// DetectVersion examines the top-level openapi/swagger field of a raw document
// and returns the detected version plus any diagnostics.
//
// Supported inputs:
//   - swagger: "2.0"  -> Version2_0
//   - openapi: "3.0.x" -> Version3_0
//   - openapi: "3.1.x" -> Version3_1
//
// The returned version is a normalized family identifier (e.g., "3.0" for
// any OpenAPI 3.0.x patch release). The exact patch number is intentionally
// discarded; callers that need the original version string should read it
// directly from the source document.
func DetectVersion(root Node) (Version, []Diagnostic) {
	if root == nil {
		return VersionUnknown, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Missing OpenAPI version",
			Detail:         "The document root is empty. It must contain either 'swagger: \"2.0\"' or 'openapi: \"3.0.x\" / \"3.1.x\"'.",
			SourceLocation: &SourceLocation{File: "<input>", Line: 1},
		}}
	}

	m, ok := root.(*MapNode)
	if !ok {
		rootLoc := nodeLoc(root)
		return VersionUnknown, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Invalid OpenAPI document",
			Detail:         "The document root must be a JSON object or YAML mapping.",
			SourceLocation: &rootLoc,
		}}
	}

	rootLoc := m.SourceLocation
	openAPI := findMapEntry(m, "openapi")
	swagger := findMapEntry(m, "swagger")

	if openAPI != nil && swagger != nil {
		return VersionUnknown, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Ambiguous OpenAPI version fields",
			Detail:         "Document contains both 'openapi' and 'swagger'. Provide exactly one version field.",
			SourceLocation: &rootLoc,
		}}
	}

	if openAPI == nil && swagger == nil {
		return VersionUnknown, []Diagnostic{{
			Severity:       SeverityError,
			Summary:        "Missing OpenAPI version",
			Detail:         "Document is missing both 'openapi' and 'swagger'. Add 'swagger: \"2.0\"' or 'openapi: \"3.0.x\" / \"3.1.x\"'.",
			SourceLocation: &rootLoc,
		}}
	}

	if swagger != nil {
		return detectSwaggerVersion(*swagger, rootLoc)
	}
	return detectOpenAPIVersion(*openAPI, rootLoc)
}

func detectSwaggerVersion(entry MapEntry, rootLoc SourceLocation) (Version, []Diagnostic) {
	val, loc, diag := scalarString(entry, rootLoc)
	if diag != nil {
		return VersionUnknown, []Diagnostic{*diag}
	}
	if val == "2.0" {
		return Version2_0, nil
	}
	return VersionUnknown, []Diagnostic{{
		Severity:       SeverityError,
		Summary:        "Unsupported Swagger version",
		Detail:         fmt.Sprintf("Expected 'swagger: \"2.0\"', got 'swagger: %q'.", val),
		SourceLocation: &loc,
	}}
}

func detectOpenAPIVersion(entry MapEntry, rootLoc SourceLocation) (Version, []Diagnostic) {
	val, loc, diag := scalarString(entry, rootLoc)
	if diag != nil {
		return VersionUnknown, []Diagnostic{*diag}
	}

	parts := strings.Split(val, ".")
	if len(parts) < 2 {
		return unsupportedOpenAPIVersion(val, loc)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return unsupportedOpenAPIVersion(val, loc)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return unsupportedOpenAPIVersion(val, loc)
	}

	if major != 3 || minor < 0 {
		return unsupportedOpenAPIVersion(val, loc)
	}

	// Validate patch if present. The OpenAPI spec uses the `major.minor.patch`
	// form; a missing patch (e.g. "3.0") is accepted as a deliberate leniency
	// (the spec's SHOULD) and is exercised by version tests, but a version
	// with more than three segments (e.g. "3.0.0.1") is rejected so trailing
	// garbage is not silently ignored (L-91: the prior check validated only
	// the first three segments and dropped any extras).
	if len(parts) > 3 {
		return unsupportedOpenAPIVersion(val, loc)
	}
	if len(parts) == 3 {
		if _, err := strconv.Atoi(parts[2]); err != nil {
			return unsupportedOpenAPIVersion(val, loc)
		}
	}

	switch minor {
	case 0:
		return Version3_0, nil
	case 1:
		return Version3_1, nil
	default:
		return unsupportedOpenAPIVersion(val, loc)
	}
}

func unsupportedOpenAPIVersion(val string, loc SourceLocation) (Version, []Diagnostic) {
	return VersionUnknown, []Diagnostic{{
		Severity:       SeverityError,
		Summary:        "Unsupported OpenAPI version",
		Detail:         fmt.Sprintf("Expected 'openapi: \"3.0.x\"' or 'openapi: \"3.1.x\"', got 'openapi: %q'.", val),
		SourceLocation: &loc,
	}}
}

// scalarString extracts the string value of a version field. It relies on the
// lexer to preserve `openapi`/`swagger` values as string scalars (including
// unquoted YAML values), so only a genuine type mismatch produces a diagnostic.
func scalarString(entry MapEntry, rootLoc SourceLocation) (string, SourceLocation, *Diagnostic) {
	if entry.Value == nil {
		return "", rootLoc, &Diagnostic{
			Severity:       SeverityError,
			Summary:        "Missing version value",
			Detail:         "The version field has no value.",
			SourceLocation: &entry.Key.SourceLocation,
		}
	}
	loc := nodeLoc(entry.Value)
	s, ok := entry.Value.(*ScalarNode)
	if !ok {
		return "", loc, &Diagnostic{
			Severity:       SeverityError,
			Summary:        "Invalid version value",
			Detail:         "The version field must be a string scalar.",
			SourceLocation: &loc,
		}
	}
	str, ok := s.Value.(string)
	if !ok {
		return "", loc, &Diagnostic{
			Severity:       SeverityError,
			Summary:        "Invalid version value",
			Detail:         fmt.Sprintf("The version field must be a string, got %T.", s.Value),
			SourceLocation: &loc,
		}
	}
	return str, loc, nil
}

func nodeLoc(n Node) SourceLocation {
	if n == nil {
		return SourceLocation{}
	}
	return n.GetSourceLocation()
}

func findMapEntry(m *MapNode, key string) *MapEntry {
	// Iterate in reverse so a duplicated key resolves to the last occurrence,
	// matching the converters' last-wins map assignment (H-2). Previously this
	// returned the first occurrence while conversion kept the last, so $ref
	// resolution and the converted Spec could disagree about one document.
	for i := len(m.Entries) - 1; i >= 0; i-- {
		if m.Entries[i].Key != nil && m.Entries[i].Key.Value == key {
			return &m.Entries[i]
		}
	}
	return nil
}
