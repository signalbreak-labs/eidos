package generator

import (
	"io"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ValidatorsFile returns the generated internal/provider/validators.go file
// containing custom Terraform-plugin-framework validators inferred from
// advanced OpenAPI/JSON Schema constraints in the provider IR.
func ValidatorsFile(pir ir.ProviderIR) File {
	file := schema.GenerateValidatorsFile(pir)
	if file == nil {
		return noValidatorsFile()
	}
	return GoCodeAST("internal/provider/validators.go", file)
}

// noValidatorsFile returns a file containing only the package declaration and the
// no-validators comment.
func noValidatorsFile() File {
	return File{
		Path: "internal/provider/validators.go",
		Render: func(w io.Writer) error {
			_, err := w.Write([]byte("package provider\n\n// No custom validators are required for this provider.\n"))
			return err
		},
	}
}
