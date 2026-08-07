package client

import "encoding/json"

// Envelope is a generic response wrapper used when the API wraps objects in a predictable envelope.
type Envelope struct {
	Data json.RawMessage `json:"data"`
}

// ErrorResponse captures a common error payload shape returned by APIs.
type ErrorResponse struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Empty is a placeholder empty request or response body.
type Empty struct{}
