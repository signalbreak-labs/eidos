package diagnostics

// WithSourceLocation returns a copy of d with loc attached when d does not
// already carry a concrete source location. The original diagnostic is
// returned unchanged if it has a file with a positive line number.
func (d Diagnostic) WithSourceLocation(loc SourceLocation) Diagnostic {
	if !d.HasSourceLocation() {
		locCopy := loc
		d.SourceLocation = &locCopy
	}
	return d
}

// HasSourceLocation reports whether the diagnostic carries a source location
// with both a file name and a positive line number.
func (d Diagnostic) HasSourceLocation() bool {
	if d.SourceLocation == nil {
		return false
	}
	return d.SourceLocation.File != "" && d.SourceLocation.Line > 0
}

// EnsureSourceLocation returns a new Diagnostics slice where every diagnostic
// lacking a concrete source location is annotated with loc. Existing source
// locations are preserved.
func (ds Diagnostics) EnsureSourceLocation(loc SourceLocation) Diagnostics {
	if len(ds) == 0 {
		return ds
	}
	out := make(Diagnostics, len(ds))
	for i, d := range ds {
		out[i] = d.WithSourceLocation(loc)
	}
	return out
}
