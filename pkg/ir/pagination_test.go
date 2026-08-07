package ir

import "testing"

func TestPaginationIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, PaginationIR{
		Style:        "offset",
		PageParam:    "page",
		PerPageParam: "per_page",
	})
}
