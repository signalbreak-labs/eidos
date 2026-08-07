package generator

import (
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ValueMappersFile returns the generated internal/protocol/value_mappers.go file
// that converts between tftypes.Value and the plain Go structs emitted by
// ModelFile. The providerImport argument is the canonical import path for the
// generated provider package.
func ValueMappersFile(resources []ir.ResourceIR, providerImport string) File {
	path := filepath.Join("internal", "protocol", "value_mappers.go")
	return GoCodeAST(path, schema.GenerateValueMappersFile(resources, providerImport))
}
