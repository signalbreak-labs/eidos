package ir

import "time"

// ClientIR captures provider-level HTTP client configuration emitted by the
// generator.
//
// Duration fields (RetryWaitMin, RetryWaitMax, Timeout) serialize as
// nanosecond integers, matching Go's json.Marshal convention for time.Duration.
type ClientIR struct {
	BaseURLTemplate string        `json:"base_url_template,omitempty"`
	UserAgent       string        `json:"user_agent,omitempty"`
	RetryMax        int           `json:"retry_max,omitempty"`
	RetryWaitMin    time.Duration `json:"retry_wait_min,omitempty"`
	RetryWaitMax    time.Duration `json:"retry_wait_max,omitempty"`
	Timeout         time.Duration `json:"timeout,omitempty"`
	AuthMiddleware  []string      `json:"auth_middleware,omitempty"`
	Pagination      *PaginationIR `json:"pagination,omitempty"`
	Logging         *LoggingIR    `json:"logging,omitempty"`
}

// LoggingIR carries the generator.yaml HTTP trace-logging settings into the IR
// so generation can bake them into the provider's Configure-time
// client.LoggingConfig. Field names mirror the generated client's
// LoggingConfig (pkg/generator/logging.go), not config.LoggingConfig: the
// transformer translates FilePath→LogFile and drops Enabled in favor of
// "enabled iff LogFile is non-empty", matching the generated client's New
// guard. The practitioner-facing log_* provider attributes override these
// baked defaults at Configure time.
type LoggingIR struct {
	LogFile                string   `json:"log_file,omitempty"`
	CaptureRequestHeaders  bool     `json:"capture_request_headers,omitempty"`
	CaptureRequestBody     bool     `json:"capture_request_body,omitempty"`
	CaptureResponseHeaders bool     `json:"capture_response_headers,omitempty"`
	CaptureResponseBody    bool     `json:"capture_response_body,omitempty"`
	MaxBodyBytes           int      `json:"max_body_bytes,omitempty"`
	RedactHeaders          []string `json:"redact_headers,omitempty"`
}
