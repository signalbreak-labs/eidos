package ir

// PaginationIR captures pagination strategy metadata for list endpoints.
//
// Style holds one of the PaginationStyle* canonical string constants defined
// below. It is a raw string (rather than a typed enum) so the IR stays a pure
// data layer with no cross-package type dependency; the transformer
// (pkg/transformer/datasource.go) and generator (pkg/generator/client.go) each
// define their own typed enums whose values reference these constants so the
// three vocabularies cannot drift (L-63: previously each package hard-coded its
// own "offset"/"cursor"/"link_header"/"none" literals with no shared source of
// truth).
type PaginationIR struct {
	Style            string `json:"style,omitempty"`
	PageParam        string `json:"page_param,omitempty"`
	PerPageParam     string `json:"per_page_param,omitempty"`
	TotalCountHeader string `json:"total_count_header,omitempty"`
	NextLinkHeader   string `json:"next_link_header,omitempty"`
	CursorField      string `json:"cursor_field,omitempty"`
}

// PaginationStyle* are the canonical string values for pagination strategies,
// shared by the IR Style fields (PaginationIR.Style, ListResourceIR.
// PaginationStyle) and by the typed enums in the transformer and generator.
// They are untyped string constants so each package can adopt them into its
// own typed enum without import cycles.
const (
	// PaginationStyleOffset requests pages using offset/limit or
	// page/per_page parameters.
	PaginationStyleOffset = "offset"
	// PaginationStyleCursor requests pages using a cursor token.
	PaginationStyleCursor = "cursor"
	// PaginationStyleLinkHeader follows RFC 5988 Link headers to retrieve the
	// next page.
	PaginationStyleLinkHeader = "link_header"
	// PaginationStyleNone disables pagination helpers.
	PaginationStyleNone = "none"
)
