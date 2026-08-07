package generator

import (
	"fmt"
	"path/filepath"

	"github.com/signalbreak-labs/eidos/pkg/generator/internal/naming"
	"github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// ModelFile returns the generated internal/provider/model_<name>.go file for a
// single Terraform managed resource. The generated file contains plain Go
// struct types matching the resource schema, suitable for JSON marshaling when
// calling the remote API.
func ModelFile(r ir.ResourceIR) File {
	path := filepath.Join("internal", "provider", fmt.Sprintf("model_%s.go", naming.SnakeCase(r.Name)))
	return GoCodeAST(path, schema.GenerateModelFile(r))
}

// ModelFiles returns the generated model files for every ResourceIR in the
// provider. Files are emitted in the order the resources are supplied.
func ModelFiles(resources []ir.ResourceIR) []File {
	files := make([]File, 0, len(resources))
	for _, r := range resources {
		files = append(files, ModelFile(r))
	}
	return files
}
