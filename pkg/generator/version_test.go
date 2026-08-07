package generator

import (
	"bytes"
	"go/format"
	"go/token"
	"regexp"
	"testing"
)

// TestVersionVarDecl verifies that versionVarDecl emits the package-level
// build-time metadata variables with the expected names and default values.
// GoReleaser overrides these at link time via -X ldflags.
func TestVersionVarDecl(t *testing.T) {
	decl := versionVarDecl()
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), decl); err != nil {
		t.Fatalf("format decl: %v", err)
	}
	got := buf.String()

	wantPatterns := []*regexp.Regexp{
		regexp.MustCompile(`var\s*\(`),
		regexp.MustCompile(`version\s+string\s*=\s*"dev"`),
		regexp.MustCompile(`commit\s+string\s*=\s*"none"`),
		regexp.MustCompile(`date\s+string\s*=\s*"unknown"`),
	}
	for _, want := range wantPatterns {
		if !want.MatchString(got) {
			t.Errorf("versionVarDecl() missing pattern %q\ngot:\n%s", want.String(), got)
		}
	}
}
