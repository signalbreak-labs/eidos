package generator

import "github.com/signalbreak-labs/eidos/pkg/generator/internal/schema"

// JSONConvertFile returns the generated internal/provider/json_convert.go file
// containing generic conversions between the Terraform Plugin Framework model
// structs and JSON-ready maps. Resource CRUD bodies that are wired to the
// generated API client use these helpers to build request bodies and to apply
// response payloads back to Terraform state, so no per-attribute mapping code
// has to be generated. The file is a static template like the internal/client
// package files: its content does not depend on the provider IR.
func JSONConvertFile() File {
	return Template("internal/provider/json_convert.go", schema.JSONConvertTemplate, nil)
}
