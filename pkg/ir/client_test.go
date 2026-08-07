package ir

import (
	"testing"
	"time"
)

func TestClientIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ClientIR{
		BaseURLTemplate: "https://api.example.com/v1",
		UserAgent:       "terraform-provider-example/1.0.0",
		RetryMax:        3,
		RetryWaitMin:    100 * time.Millisecond,
		RetryWaitMax:    5 * time.Second,
		Timeout:         30 * time.Second,
		AuthMiddleware:  []string{"api_key", "oauth2"},
		Pagination: &PaginationIR{
			Style:            "cursor",
			CursorField:      "next_cursor",
			TotalCountHeader: "X-Total-Count",
			NextLinkHeader:   "Link",
		},
	})
}
