// Package transformer maps normalized OpenAPI schemas to Terraform Plugin Framework
// representations used by the Eidos provider generator.
package transformer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// PaginationStyle classifies how a collection endpoint paginates results.
type PaginationStyle string

const (
	// PaginationOffset indicates page-based or offset-based pagination.
	PaginationOffset PaginationStyle = ir.PaginationStyleOffset
	// PaginationCursor indicates cursor-based pagination.
	PaginationCursor PaginationStyle = ir.PaginationStyleCursor
	// PaginationLinkHeader indicates RFC 5988 Link header pagination.
	PaginationLinkHeader PaginationStyle = ir.PaginationStyleLinkHeader
	// PaginationNone indicates no detectable pagination style.
	PaginationNone PaginationStyle = ir.PaginationStyleNone
)

// DataSource represents a Terraform data source inferred from a collection GET
// endpoint that is not paired with a managed resource instance path. It
// returns a list of items rather than a single known instance.
type DataSource struct {
	Name            string
	CollectionPath  string
	List            *Operation
	PaginationStyle PaginationStyle
}

// ListResource represents a Terraform list resource (tfquery) inferred from a
// collection GET endpoint that shares identity with a managed resource.
type ListResource struct {
	Name            string
	ResourceName    string // matches the paired managed resource type name
	CollectionPath  string
	InstancePath    string
	List            *Operation
	PaginationStyle PaginationStyle
}

// InferDataSources returns data sources inferred from resources that have a
// collection GET operation but no paired managed resource instance path.
// Resources with an instance path are never data sources; those with a paired
// Read operation are surfaced as list resources by InferListResources, while
// incomplete resources (instance path without Read) are dropped from both
// inferences. The returned List operation is an isolated copy (Parameters,
// ResponseHeaders and Extensions are cloned; schema pointers are shared), so
// callers may inspect or mutate it without affecting the source ResourceCRUD.
func InferDataSources(resources []ResourceCRUD) []DataSource {
	var sources []DataSource
	for _, r := range resources {
		if r.List == nil {
			continue
		}
		// Any resource with an instance path is considered paired with a managed
		// resource and is therefore not a data source.
		if r.InstancePath != "" {
			continue
		}
		sources = append(sources, DataSource{
			Name:            collectionName(r.CollectionPath),
			CollectionPath:  r.CollectionPath,
			List:            cloneOperation(r.List),
			PaginationStyle: DetectPaginationStyle(*r.List),
		})
	}
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].Name < sources[j].Name
	})
	sources = dedupByName(sources, func(s DataSource) string { return s.Name })
	return sources
}

// InferListResources returns list resources inferred from resources that have
// both a collection GET operation and a paired managed resource instance path.
// The list resource shares the managed resource's identity schema. The returned
// List operation is an isolated copy (Parameters, ResponseHeaders and Extensions
// are cloned; schema pointers are shared), so callers may inspect or mutate it
// without affecting the source ResourceCRUD.
func InferListResources(resources []ResourceCRUD) []ListResource {
	var lists []ListResource
	for _, r := range resources {
		if r.List == nil || r.InstancePath == "" || r.Read == nil {
			continue
		}
		lists = append(lists, ListResource{
			Name:            collectionName(r.CollectionPath),
			ResourceName:    r.Name,
			CollectionPath:  r.CollectionPath,
			InstancePath:    r.InstancePath,
			List:            cloneOperation(r.List),
			PaginationStyle: DetectPaginationStyle(*r.List),
		})
	}
	sort.SliceStable(lists, func(i, j int) bool {
		return lists[i].Name < lists[j].Name
	})
	lists = dedupByName(lists, func(l ListResource) string { return l.Name })
	return lists
}

// DetectPaginationStyle examines an operation's OpenAPI extensions, response
// headers, and query parameters to determine the pagination style used by a
// collection endpoint.
//
// Detection order:
//  1. x-pagination extension (string "style" key or string value). An
//     unrecognized value is treated as PaginationNone rather than falling back
//     to heuristic detection.
//  2. Response Link header → link_header
//  3. Query parameter names suggesting offset/page or cursor
//  4. Default → none
func DetectPaginationStyle(op Operation) PaginationStyle {
	if style, present := paginationFromExtension(op.Extensions); present {
		return style
	}
	for _, h := range op.ResponseHeaders {
		if strings.EqualFold(h, "Link") {
			return PaginationLinkHeader
		}
	}
	hasOffset := false
	hasCursor := false
	for _, p := range op.Parameters {
		if !strings.EqualFold(p.In, "query") {
			continue
		}
		name := strings.ToLower(p.Name)
		switch {
		case strings.Contains(name, "cursor"), strings.Contains(name, "after"):
			hasCursor = true
		case strings.Contains(name, "page"),
			strings.Contains(name, "offset"),
			strings.Contains(name, "skip"):
			hasOffset = true
		}
	}
	if hasCursor {
		return PaginationCursor
	}
	if hasOffset {
		return PaginationOffset
	}
	return PaginationNone
}

// paginationFromExtension returns the PaginationStyle declared by the
// x-pagination extension, if present. It accepts either a string value or a map
// with a "style" string key. The second result is true when the extension key
// is present. An unrecognized value returns PaginationNone and true, so callers
// can distinguish "missing extension" from "invalid extension" and avoid
// silently falling back to heuristic detection when the user explicitly asked
// for a style we do not recognize.
func paginationFromExtension(ext map[string]any) (PaginationStyle, bool) {
	v, ok := ext["x-pagination"]
	if !ok {
		return "", false
	}
	normalize := func(s string) (PaginationStyle, bool) {
		style := normalizePaginationStyle(s)
		if style == "" {
			return PaginationNone, true
		}
		return style, true
	}
	switch s := v.(type) {
	case string:
		return normalize(s)
	case map[string]any:
		styleVal, ok := s["style"]
		if !ok {
			return PaginationNone, true
		}
		styleStr, ok := styleVal.(string)
		if !ok {
			return PaginationNone, true
		}
		return normalize(styleStr)
	}
	return PaginationNone, true
}

// normalizePaginationStyle maps common pagination style names to the canonical
// PaginationStyle value. It returns "" for unrecognized styles so callers can
// fall back to heuristic detection.
func normalizePaginationStyle(s string) PaginationStyle {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "offset", "page":
		return PaginationOffset
	case "cursor":
		return PaginationCursor
	case "link_header", "link":
		return PaginationLinkHeader
	case "none", "":
		return PaginationNone
	}
	return ""
}

// collectionName returns a stable data-source-style name from a collection path.
// It uses the last static path segment; if the path has only parameters it falls
// back to "collection".
func collectionName(path string) string {
	segs := parsePath(path)
	for i := len(segs) - 1; i >= 0; i-- {
		if !segs[i].IsParam {
			return segs[i].Value
		}
	}
	return "collection"
}

// cloneOperation returns an isolated copy of an Operation pointer suitable for
// use by inferred data sources and list resources. The returned Operation has
// its own Parameters, ResponseHeaders, and Extensions slices/maps, while
// RequestSchema and ResponseSchema pointers are shared with the input. It
// returns nil when the input is nil.
func cloneOperation(op *Operation) *Operation {
	if op == nil {
		return nil
	}
	c := *op
	if len(c.Parameters) > 0 {
		c.Parameters = append([]Parameter(nil), c.Parameters...)
	}
	if len(c.ResponseHeaders) > 0 {
		c.ResponseHeaders = append([]string(nil), c.ResponseHeaders...)
	}
	if len(c.Extensions) > 0 {
		// Copy into a fresh map before reassigning c.Extensions; reassigning
		// first would make the range iterate the new (empty) map and silently
		// drop every extension (L-93).
		copied := make(map[string]any, len(c.Extensions))
		for k, v := range c.Extensions {
			copied[k] = v
		}
		c.Extensions = copied
	}
	return &c
}

// MarshalText implements encoding.TextMarshaler for PaginationStyle.
func (p PaginationStyle) MarshalText() ([]byte, error) {
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for PaginationStyle.
func (p *PaginationStyle) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return fmt.Errorf("pagination style cannot be empty")
	}
	style := normalizePaginationStyle(string(text))
	if style == "" {
		return fmt.Errorf("unsupported pagination style %q", string(text))
	}
	*p = style
	return nil
}
