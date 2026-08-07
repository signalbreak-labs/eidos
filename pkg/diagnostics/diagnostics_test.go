package diagnostics

import (
	"testing"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{Error, "error"},
		{Warning, "warning"},
		{Info, "info"},
		{Severity(99), "severity(99)"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.sev.String(); got != tc.want {
				t.Errorf("Severity(%d).String() = %q, want %q", tc.sev, got, tc.want)
			}
		})
	}
}

func TestSourceLocationString(t *testing.T) {
	tests := []struct {
		name string
		loc  SourceLocation
		want string
	}{
		{"file and line", SourceLocation{File: "spec.yaml", Line: 42}, "spec.yaml:42"},
		{"file line and column", SourceLocation{File: "spec.yaml", Line: 42, Column: 5}, "spec.yaml:42:5"},
		{"only file", SourceLocation{File: "spec.yaml", Line: 0}, "spec.yaml"},
		{"negative line", SourceLocation{File: "x", Line: -1}, "x"},
		{"empty file with line", SourceLocation{Line: 42}, ""},
		{"empty", SourceLocation{}, ""},
		{"zero column omitted", SourceLocation{File: "spec.yaml", Line: 42, Column: 0}, "spec.yaml:42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.loc.String(); got != tc.want {
				t.Errorf("SourceLocation.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSourceLocationIsEmpty(t *testing.T) {
	empty := SourceLocation{}
	if !empty.IsEmpty() {
		t.Error("expected empty SourceLocation")
	}
	withLineOnly := SourceLocation{Line: 1}
	if !withLineOnly.IsEmpty() {
		t.Error("expected empty SourceLocation with only a line")
	}
	withPathOnly := SourceLocation{Path: "/info"}
	if !withPathOnly.IsEmpty() {
		t.Error("expected empty SourceLocation with only a path")
	}
	withFile := SourceLocation{File: "x"}
	if withFile.IsEmpty() {
		t.Error("expected non-empty SourceLocation with file")
	}
	withFileAndLine := SourceLocation{File: "x", Line: 1}
	if withFileAndLine.IsEmpty() {
		t.Error("expected non-empty SourceLocation with file and line")
	}
}

func TestSourceLocationHasPosition(t *testing.T) {
	empty := SourceLocation{}
	if empty.HasPosition() {
		t.Error("expected no position for empty SourceLocation")
	}
	fileOnly := SourceLocation{File: "x"}
	if fileOnly.HasPosition() {
		t.Error("expected no position for file-only SourceLocation")
	}
	withLine := SourceLocation{Line: 1}
	if !withLine.HasPosition() {
		t.Error("expected position with line")
	}
	withColumn := SourceLocation{Column: 1}
	if !withColumn.HasPosition() {
		t.Error("expected position with column")
	}
	withAll := SourceLocation{File: "x", Line: 2, Column: 3}
	if !withAll.HasPosition() {
		t.Error("expected position with line and column")
	}
}

func TestLocPtr(t *testing.T) {
	loc := SourceLocation{File: "spec.yaml", Line: 10, Column: 5}
	ptr := LocPtr(loc)
	if ptr == nil {
		t.Fatal("LocPtr returned nil")
	}
	if *ptr != loc {
		t.Fatalf("LocPtr returned wrong value: got %+v, want %+v", *ptr, loc)
	}
	// Mutating the original must not affect the pointer.
	loc.Line = 99
	if ptr.Line != 10 {
		t.Fatalf("LocPtr copy was affected by original mutation, line=%d", ptr.Line)
	}
}

func TestDiagnosticWithSourceLocation(t *testing.T) {
	loc := SourceLocation{File: "spec.yaml", Line: 10}

	d := Diagnostic{Summary: "missing file"}
	d = d.WithSourceLocation(loc)
	if d.SourceLocation == nil || d.SourceLocation.File != "spec.yaml" || d.SourceLocation.Line != 10 {
		t.Errorf("expected location attached, got %+v", d.SourceLocation)
	}

	fileOnly := Diagnostic{Summary: "file only", SourceLocation: &SourceLocation{File: "old.yaml"}}
	fileOnly = fileOnly.WithSourceLocation(loc)
	if fileOnly.SourceLocation == nil || fileOnly.SourceLocation.Line != 10 {
		t.Errorf("file-only diagnostic should be overwritten with a concrete location, got %+v", fileOnly.SourceLocation)
	}

	concrete := Diagnostic{Summary: "concrete", SourceLocation: &SourceLocation{File: "old.yaml", Line: 5}}
	concrete = concrete.WithSourceLocation(loc)
	if concrete.SourceLocation == nil || concrete.SourceLocation.Line != 5 {
		t.Errorf("concrete location should be preserved, got %+v", concrete.SourceLocation)
	}
}

func TestDiagnosticString(t *testing.T) {
	tests := []struct {
		name string
		d    Diagnostic
		want string
	}{
		{
			name: "full diagnostic",
			d: Diagnostic{
				Severity:       Error,
				Summary:        "invalid field",
				Detail:         "field 'name' must be a string",
				SourceLocation: &SourceLocation{File: "spec.yaml", Line: 42, Column: 5},
			},
			want: "spec.yaml:42:5: error: invalid field: field 'name' must be a string",
		},
		{
			name: "without source location",
			d: Diagnostic{
				Severity: Warning,
				Summary:  "deprecated field",
				Detail:   "field 'id' is deprecated",
			},
			want: "warning: deprecated field: field 'id' is deprecated",
		},
		{
			name: "summary only",
			d: Diagnostic{
				Severity: Info,
				Summary:  "using default value",
			},
			want: "info: using default value",
		},
		{
			name: "detail only",
			d: Diagnostic{
				Severity: Error,
				Detail:   "something went wrong",
			},
			want: "error: something went wrong",
		},
		{
			name: "empty source location",
			d: Diagnostic{
				Severity:       Info,
				Summary:        "note",
				SourceLocation: &SourceLocation{},
			},
			want: "info: note",
		},
		{
			name: "file-only source location",
			d: Diagnostic{
				Severity:       Error,
				Summary:        "msg",
				SourceLocation: &SourceLocation{File: "x"},
			},
			want: "x: error: msg",
		},
		{
			name: "file-only source location with detail",
			d: Diagnostic{
				Severity:       Error,
				Summary:        "msg",
				Detail:         "detail text",
				SourceLocation: &SourceLocation{File: "x"},
			},
			want: "x: error: msg: detail text",
		},
		{
			name: "unknown severity",
			d: Diagnostic{
				Severity: Severity(99),
				Summary:  "odd",
			},
			want: "severity(99): odd",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.d.String(); got != tc.want {
				t.Errorf("Diagnostic.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiagnosticsHasErrors(t *testing.T) {
	empty := Diagnostics{}
	if empty.HasErrors() {
		t.Error("empty Diagnostics should not report errors")
	}
	errs := Diagnostics{Diagnostic{Severity: Error, Summary: "oops"}}
	if !errs.HasErrors() {
		t.Error("Diagnostics with Error severity should report errors")
	}
	warns := Diagnostics{Diagnostic{Severity: Warning, Summary: "careful"}}
	if warns.HasErrors() {
		t.Error("Diagnostics with only Warning severity should not report errors")
	}
}

func TestDiagnosticsAppend(t *testing.T) {
	assertReceiverUnchanged := func(t *testing.T, ds Diagnostics) {
		t.Helper()
		// The receiver must remain unchanged because the original slice had no spare
		// capacity, forcing append to allocate a new backing array.
		if len(ds) != 1 {
			t.Errorf("expected original slice length 1, got %d", len(ds))
		}
		if ds[0].Summary != "first" {
			t.Errorf("expected original diagnostic summary 'first', got %q", ds[0].Summary)
		}
	}

	t.Run("single element", func(t *testing.T) {
		ds := make(Diagnostics, 1)
		ds[0] = Diagnostic{Severity: Error, Summary: "first"}

		got := ds.Append(Diagnostic{Severity: Warning, Summary: "second"})

		if len(got) != 2 {
			t.Fatalf("expected 2 diagnostics, got %d", len(got))
		}
		if got[1].Summary != "second" {
			t.Errorf("expected appended diagnostic summary 'second', got %q", got[1].Summary)
		}

		assertReceiverUnchanged(t, ds)
	})

	t.Run("multiple elements", func(t *testing.T) {
		ds := make(Diagnostics, 1)
		ds[0] = Diagnostic{Severity: Error, Summary: "first"}

		got := ds.Append(
			Diagnostic{Severity: Warning, Summary: "second"},
			Diagnostic{Severity: Info, Summary: "third"},
		)

		if len(got) != 3 {
			t.Fatalf("expected 3 diagnostics, got %d", len(got))
		}
		if got[1].Summary != "second" || got[2].Summary != "third" {
			t.Errorf("expected appended diagnostics in order, got %q and %q", got[1].Summary, got[2].Summary)
		}

		assertReceiverUnchanged(t, ds)
	})

	t.Run("no elements", func(t *testing.T) {
		ds := make(Diagnostics, 2)
		ds[0] = Diagnostic{Severity: Error, Summary: "first"}
		ds[1] = Diagnostic{Severity: Warning, Summary: "second"}

		got := ds.Append()

		if len(got) != len(ds) {
			t.Fatalf("expected %d diagnostics, got %d", len(ds), len(got))
		}
		for i := range ds {
			if got[i].Summary != ds[i].Summary {
				t.Errorf("expected diagnostic[%d] summary %q, got %q", i, ds[i].Summary, got[i].Summary)
			}
		}
	})
}

func TestDiagnosticsNil(t *testing.T) {
	var ds Diagnostics
	if ds.HasErrors() {
		t.Error("nil Diagnostics should not report errors")
	}
	ds = ds.Append(Diagnostic{Severity: Error, Summary: "oops"})
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(ds))
	}
	if ds[0].Summary != "oops" {
		t.Errorf("expected summary 'oops', got %q", ds[0].Summary)
	}
}
