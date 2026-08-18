package mcp

import (
	"context"
	"errors"
	"os"

	"github.com/signalbreak-labs/eidos/pkg/specsource"
)

// The MCP tools accept the same `spec` reference shapes the CLI's --spec flag
// accepts (cmd/eidos/remote_spec.go): inline JSON/YAML content, a local file
// path, a file:// URL, or an http(s):// URL. loadSpecRef first tries to load
// the string as a source reference; if it is not one, it falls back to treating
// the string as inline content so existing callers that pass the spec body keep
// working.
//
// The resolution and the hardened remote fetch live in pkg/specsource, shared
// with the CLI (N-62) so the two entry points cannot drift again (file://
// support once existed in one and not the other). Remote http(s) fetches are
// https-only by default (http is opt-in via EIDOS_SPEC_ALLOW_HTTP=1), an SSRF
// guard rejects private/loopback/link-local hosts (relaxed for the initial host
// only via EIDOS_SPEC_ALLOW_PRIVATE=1, never on redirect targets) and pins the
// validated IPs so a DNS rebind cannot redirect the dial (N-55), the fetch runs
// on the handler's context so a client disconnect aborts it (N-53), a 30s
// timeout bounds it, and a 10 MiB response cap guards memory. Credentials are
// never accepted via the spec reference; pass inline content or a local file
// instead.

// errNotASourceRef signals that a string is not a file path or URL and should be
// treated as inline spec content by the caller. It aliases the shared sentinel
// so errors.Is works across the delegation boundary.
var errNotASourceRef = specsource.ErrNotASourceRef

// loadSpecRef loads spec bytes from a local file path, a file:// URL, or an
// http(s):// URL, returning errNotASourceRef when the string is not a source
// reference so the caller can fall back to inline-content handling. It runs on
// ctx so an aborted request cancels an in-flight remote fetch (N-53).
func loadSpecRef(ctx context.Context, ref string) ([]byte, error) {
	data, _, err := specsource.LoadSpec(ctx, ref, mcpSpecOptions(true))
	if errors.Is(err, specsource.ErrNotASourceRef) {
		return nil, errNotASourceRef
	}
	return data, err
}

// loadConfigRef loads generator.yaml bytes from a local file path or a file://
// URL. It returns errNotASourceRef when the string is not a file reference so
// the caller can fall back to inline-content handling. Unlike spec references,
// remote http(s):// config URLs are not resolved: a generator.yaml is small and
// local, and accepting remote URLs would widen the SSRF surface for no real
// benefit — pass inline content or a local path instead (this mirrors the CLI's
// loadSpecBytes so an LLM can hand the `config` argument the same ways it hands
// the `spec` argument).
func loadConfigRef(ctx context.Context, ref string) ([]byte, error) {
	data, err := specsource.LoadConfig(ctx, ref, mcpSpecOptions(true))
	if errors.Is(err, specsource.ErrNotASourceRef) {
		return nil, errNotASourceRef
	}
	return data, err
}

// mcpSpecOptions builds the shared specsource options for the MCP tools: the
// https/http and private-host escape hatches come from the same environment
// variables the CLI honors, and strings that are not source references fall
// back to inline-content handling.
func mcpSpecOptions(inlineFallback bool) specsource.Options {
	return specsource.Options{
		AllowHTTP:      os.Getenv("EIDOS_SPEC_ALLOW_HTTP") == "1",
		AllowPrivate:   os.Getenv("EIDOS_SPEC_ALLOW_PRIVATE") == "1",
		InlineFallback: inlineFallback,
	}
}
