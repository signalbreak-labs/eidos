package transformer

import (
	"github.com/signalbreak-labs/eidos/pkg/ir"
	"github.com/signalbreak-labs/eidos/pkg/parser"
)

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

// parserServersToTransformer converts parser-level servers (whose variables are
// pointer values and which carry source locations) into the transformer's
// version-agnostic Server form. The nil-vs-empty distinction is preserved so
// NormalizeOperationServers can honor an explicit empty override (M-15).
func parserServersToTransformer(servers []parser.Server) []Server {
	if servers == nil {
		return nil
	}
	out := make([]Server, 0, len(servers))
	for _, s := range servers {
		sv := Server{
			URL:         s.URL,
			Description: s.Description,
		}
		if len(s.Variables) > 0 {
			sv.Variables = make(map[string]ServerVariable, len(s.Variables))
			for name, v := range s.Variables {
				if v == nil {
					continue
				}
				enum := make([]string, len(v.Enum))
				copy(enum, v.Enum)
				sv.Variables[name] = ServerVariable{
					Default:     v.Default,
					Enum:        enum,
					Description: v.Description,
				}
			}
		}
		out = append(out, sv)
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
