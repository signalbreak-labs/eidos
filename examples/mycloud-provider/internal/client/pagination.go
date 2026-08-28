package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PaginationStyle enumerates supported pagination strategies.
type PaginationStyle string

const (
	// PaginationStyleOffset requests pages using offset/limit or page/per_page parameters.
	PaginationStyleOffset PaginationStyle = "offset"
	// PaginationStyleCursor requests pages using a cursor token.
	PaginationStyleCursor PaginationStyle = "cursor"
	// PaginationStyleLinkHeader follows RFC 5988 Link headers to retrieve the next page.
	PaginationStyleLinkHeader PaginationStyle = "link_header"
	// PaginationStyleNone disables pagination helpers.
	PaginationStyleNone PaginationStyle = "none"
)

// Pagination holds the configured pagination strategy.
type Pagination struct {
	Style        PaginationStyle
	PageParam    string
	PerPageParam string
	NextLinkRel  string
	CursorField  string
}

// DefaultPagination returns the provider's default pagination configuration.
func DefaultPagination() Pagination {
	return Pagination{
		Style:        "none",
		PageParam:    "page",
		PerPageParam: "per_page",
		NextLinkRel:  "next",
		CursorField:  "cursor",
	}
}

// ExtractLinkHeader returns the URL for the requested rel from an RFC 5988 Link header.
func ExtractLinkHeader(header string, rel string) string {
	links := parseLinkHeader(header)
	return links[rel]
}

func parseLinkHeader(header string) map[string]string {
	result := make(map[string]string)
	for _, part := range splitLinks(header) {
		url, rest := splitLink(part)
		if url == "" {
			continue
		}
		for _, p := range parseParams(rest) {
			if p.key == "rel" {
				result[p.value] = url
				break
			}
		}
	}
	return result
}

func splitLinks(header string) []string {
	var parts []string
	start := 0
	depth := 0
	for i, r := range header {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, header[start:i])
				start = i + 1
			}
		}
	}
	if start < len(header) {
		parts = append(parts, header[start:])
	}
	return parts
}

func splitLink(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" || part[0] != '<' {
		return "", ""
	}
	end := strings.IndexByte(part, '>')
	if end < 0 {
		return "", ""
	}
	return part[1:end], part[end+1:]
}

type linkParam struct {
	key   string
	value string
}

func parseParams(rest string) []linkParam {
	var params []linkParam
	for _, p := range strings.Split(rest, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.IndexByte(p, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(p[:idx])
		value := strings.TrimSpace(p[idx+1:])
		if len(value) > 1 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		params = append(params, linkParam{key: key, value: value})
	}
	return params
}

// maxPages bounds ListAllPages so a misbehaving server that keeps returning a
// next cursor or link cannot drive an unbounded request loop (M-9).
const maxPages = 1000

// ListAllPages repeatedly calls fetch with pagination parameters and collects all page bodies.
// The next callback is invoked after each page; it may update params and should return false
// when there are no more pages. The loop is bounded by maxPages and stops early when a next
// callback returns true without advancing the pagination parameters (loop-back detection), so
// a server that echoes the same cursor cannot drive an infinite identical-request loop (M-9).
func ListAllPages(ctx context.Context, params url.Values, fetch func(context.Context, url.Values) (*http.Response, error), next func(*http.Response, []byte, url.Values) bool) ([][]byte, error) {
	var pages [][]byte
	current := cloneValues(params)
	prev := cloneValues(params)
	advanced := false
	for page := 0; page < maxPages; page++ {
		resp, err := fetch(ctx, current)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		pages = append(pages, body)
		if next == nil || !next(resp, body, current) {
			return pages, nil
		}
		// Loop-back detection: a next callback that returns true without advancing
		// the pagination parameters (e.g. the server echoed the same cursor) would
		// otherwise issue an identical request forever. The guard only fires once
		// the parameters have changed at least once, so link_header pagination
		// (which advances via a response-embedded URL, not the parameters) is
		// unaffected.
		changed := !valuesEqual(current, prev)
		if advanced && !changed {
			return pages, nil
		}
		prev = cloneValues(current)
		advanced = advanced || changed
	}
	return nil, fmt.Errorf("pagination exceeded %d pages; the server keeps returning a next page", maxPages)
}

func valuesEqual(a, b url.Values) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		bv, ok := b[key]
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}

func cloneValues(v url.Values) url.Values {
	if v == nil {
		return nil
	}
	out := make(url.Values, len(v))
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}
