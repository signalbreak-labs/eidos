package transformer

import "github.com/signalbreak-labs/eidos/pkg/ir"

// Server is a version-agnostic OpenAPI server entry used before the document is
// transformed into the Terraform-oriented IR.
type Server struct {
	URL         string
	Description string
	Variables   map[string]ServerVariable
}

// ServerVariable is a version-agnostic OpenAPI server URL template variable.
type ServerVariable struct {
	Default     string
	Enum        []string
	Description string
}

// NormalizeOperationServers returns the effective server list for an operation
// using OpenAPI override semantics: operation-level servers override path-item-
// level servers, which in turn override global servers. A nil result means no
// servers are defined, including the case where an explicit empty override was
// supplied. The returned slice is a deep copy so callers can mutate it without
// affecting the input.
func NormalizeOperationServers(global, pathItem, operation []Server) []ir.ServerIR {
	var src []Server
	switch {
	case operation != nil:
		src = operation
	case pathItem != nil:
		src = pathItem
	case global != nil:
		src = global
	default:
		return nil
	}

	if len(src) == 0 {
		return nil
	}

	out := make([]ir.ServerIR, 0, len(src))
	for _, s := range src {
		out = append(out, toServerIR(s))
	}
	return out
}

// toServerIR converts a transformer Server into the normalized IR representation.
func toServerIR(s Server) ir.ServerIR {
	out := ir.ServerIR{
		URL:         s.URL,
		Description: s.Description,
	}
	if len(s.Variables) > 0 {
		out.Variables = make(map[string]ir.ServerVariableIR, len(s.Variables))
		for name, v := range s.Variables {
			enum := make([]string, len(v.Enum))
			copy(enum, v.Enum)
			out.Variables[name] = ir.ServerVariableIR{
				Default:     v.Default,
				Enum:        enum,
				Description: v.Description,
			}
		}
	}
	return out
}
