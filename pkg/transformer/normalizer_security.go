package transformer

// NormalizeOperationSecurity returns the effective security requirements for an
// operation using OpenAPI override semantics: operation-level security
// overrides path-item-level security, which in turn overrides global security.
// A nil result means no security is required, including the case where an
// explicit empty override was supplied. The returned slice is a deep copy: each
// requirement map and its scopes slice are duplicated so callers can mutate the
// result without affecting the input (L-101).
func NormalizeOperationSecurity(global, pathItem, operation []SecurityRequirement) []SecurityRequirement {
	var effective []SecurityRequirement
	switch {
	case operation != nil:
		effective = operation
	case pathItem != nil:
		effective = pathItem
	default:
		effective = global
	}
	if len(effective) == 0 {
		return nil
	}
	out := make([]SecurityRequirement, len(effective))
	for i, req := range effective {
		copyReq := make(SecurityRequirement, len(req))
		for k, v := range req {
			scopes := make([]string, len(v))
			copy(scopes, v)
			copyReq[k] = scopes
		}
		out[i] = copyReq
	}
	return out
}
