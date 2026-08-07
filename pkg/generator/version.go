package generator

import (
	"go/ast"

	"github.com/signalbreak-labs/eidos/pkg/generator/astgen"
)

// versionVarDecl returns the package-level variable declaration for the
// provider's build-time metadata. GoReleaser overrides these values at link
// time using -X ldflags, for example:
//
//	-X main.version={{ .Version }} -X main.commit={{ .Commit }} -X main.date={{ .CommitDate }}
//
// The default values make the provider usable when built without GoReleaser
// (such as during local development), while still allowing exact version
// injection for registry releases.
func versionVarDecl() ast.Decl {
	return astgen.VarGroup(
		[2]string{"version", "dev"},
		[2]string{"commit", "none"},
		[2]string{"date", "unknown"},
	)
}
