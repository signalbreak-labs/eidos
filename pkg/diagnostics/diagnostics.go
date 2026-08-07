// Package diagnostics collects, formats, and reports errors and warnings
// encountered during code generation.
package diagnostics

import (
	"fmt"
	"strings"
)

// Severity indicates the importance of a Diagnostic.
//
// It is an int-based type with iota constants. Compare Severity against the
// typed constants (Error, Warning, Info), not against string literals or the
// zero value.
type Severity int

const (
	// Unset is the zero value of Severity. A Diagnostic constructed without an
	// explicit severity has Unset severity and is treated as not-an-error by
	// HasErrors, so a zero-value Diagnostic{} is not a silent blocking footgun
	// (L-21). Real diagnostics always set one of Error/Warning/Info.
	Unset Severity = iota
	// Error severity means the diagnostic describes a blocking problem.
	Error
	// Warning severity means the diagnostic describes a non-breaking problem.
	Warning
	// Info severity means the diagnostic describes supplemental information.
	Info
)

// String returns the lower-case name of the severity.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Info:
		return "info"
	case Unset:
		return "unset"
	default:
		return fmt.Sprintf("severity(%d)", s)
	}
}

// SourceLocation identifies a position in a source file.
type SourceLocation struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Path   string `json:"path,omitempty"`
}

// IsEmpty reports whether the location has no file information.
// A source location without a file is considered empty regardless of its line,
// column, or path.
func (sl SourceLocation) IsEmpty() bool {
	return sl.File == ""
}

// HasPosition reports whether the location carries a concrete source position.
// It returns true when either Line or Column is greater than zero, which is
// useful for callers that want to distinguish "has a position" from "has a
// file".
func (sl SourceLocation) HasPosition() bool {
	return sl.Line > 0 || sl.Column > 0
}

// LocPtr returns a pointer to a copy of loc. It is a small helper that makes the
// "take the address of this value" intent explicit at call sites, and avoids
// the common mistake of taking the address of a loop variable or parameter
// that may be reused.
func LocPtr(loc SourceLocation) *SourceLocation {
	return &loc
}

// String returns a "file:line" or "file:line:column" representation of the
// source location. The path is not included in the string form.
//
// If the file is empty, it returns an empty string.
// If the line is less than 1, it returns only the file.
// If the column is greater than zero, it is appended after the line.
func (sl SourceLocation) String() string {
	if sl.IsEmpty() {
		return ""
	}
	if sl.Line < 1 {
		return sl.File
	}
	if sl.Column > 0 {
		return fmt.Sprintf("%s:%d:%d", sl.File, sl.Line, sl.Column)
	}
	return fmt.Sprintf("%s:%d", sl.File, sl.Line)
}

// Diagnostic is a single report containing severity, summary, detail, and an
// optional source location.
type Diagnostic struct {
	Severity       Severity
	Summary        string
	Detail         string
	SourceLocation *SourceLocation
}

// String returns a formatted diagnostic string including source location,
// severity, summary, and detail when present.
//
// Parts are joined with ": ". Callers should treat the result as human-readable
// text and not rely on it being machine-parseable.
func (d Diagnostic) String() string {
	var parts []string

	if d.SourceLocation != nil {
		if s := d.SourceLocation.String(); s != "" {
			parts = append(parts, s)
		}
	}

	parts = append(parts, d.Severity.String())

	if d.Summary != "" {
		parts = append(parts, d.Summary)
	}

	result := strings.Join(parts, ": ")
	if d.Detail != "" {
		if result != "" {
			result += ": " + d.Detail
		} else {
			result = d.Detail
		}
	}

	return result
}

// Diagnostics is a collection of Diagnostic values.
type Diagnostics []Diagnostic

// HasErrors reports whether any diagnostic in the collection has Error severity.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

// Append returns a new Diagnostics slice with the provided diagnostics added.
// The receiver is never modified. The returned slice follows standard Go
// append semantics and may share the backing array with the receiver when the
// receiver has spare capacity, in which case mutating an appended element
// through the original slice could be visible. Treat the returned value as the
// new collection and avoid mutating appended diagnostics through the receiver.
func (ds Diagnostics) Append(more ...Diagnostic) Diagnostics {
	return append(ds, more...)
}
