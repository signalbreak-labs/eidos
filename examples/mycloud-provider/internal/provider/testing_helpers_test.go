package provider

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)
import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
	client "github.com/mycloud/terraform-provider-mycloud/internal/client"
)

// newMockClient returns a *client.Client backed by an httptest server using the supplied handler.
func newMockClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return client.New(client.WithBaseURL(ts.URL), client.WithHTTPClient(ts.Client()), client.WithRetry(0, client.DefaultRetryPolicy, client.DefaultBackoff))
}

// newMockClientStatus returns a *client.Client whose server responds with the given status code and body for every request.
func newMockClientStatus(t *testing.T, status int, body string) *client.Client {
	t.Helper()
	return newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newMockClientWithLocation returns a *client.Client whose server responds with the given status, Location header, and body.
func newMockClientWithLocation(t *testing.T, status int, location string, body string) *client.Client {
	t.Helper()
	return newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// newTransportErrorClient returns a *client.Client whose backing server is closed, so every request fails with a transport error.
func newTransportErrorClient(t *testing.T) *client.Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	ts.Close()
	return client.New(client.WithBaseURL(ts.URL), client.WithHTTPClient(ts.Client()), client.WithRetry(0, client.DefaultRetryPolicy, client.DefaultBackoff))
}

// newMalformedBaseURLClient returns a *client.Client whose base URL is unparseable, so NewRequest always fails.
func newMalformedBaseURLClient(t *testing.T) *client.Client {
	t.Helper()
	return client.New(client.WithBaseURL(":"), client.WithRetry(0, client.DefaultRetryPolicy, client.DefaultBackoff))
}

// newMockClientReadErrorBody returns a *client.Client whose responses carry a body that errors on Read, exercising the Could not read error response branch.
func newMockClientReadErrorBody(t *testing.T, status int) *client.Client {
	t.Helper()
	return client.New(client.WithBaseURL("http://read-error.test"), client.WithHTTPClient(&http.Client{Transport: readErrorTransport{status: status}}), client.WithRetry(0, client.DefaultRetryPolicy, client.DefaultBackoff))
}

// requireNoErrors fails the test if diags contains any error-level diagnostic.
func requireNoErrors(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("expected no diagnostics errors, got: %s", diags)
	}
}

// hasErrorContaining fails the test unless some diagnostic's Summary or Detail contains substr.
func hasErrorContaining(t *testing.T, diags diag.Diagnostics, substr string) {
	t.Helper()
	for _, d := range diags {
		if strings.Contains(d.Summary(), substr) || strings.Contains(d.Detail(), substr) {
			return
		}
	}
	t.Fatalf("expected a diagnostic containing %q, got: %s", substr, diags)
}

// readErrorTransport is an http.RoundTripper that returns a response with a failing body for every request.
type readErrorTransport struct {
	status int
}

func (r readErrorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: r.status, Header: http.Header{}, Body: failingReadBody{}}, nil
}

// failingReadBody is an io.ReadCloser whose Read always errors, so io.ReadAll fails.
type failingReadBody struct {
}

func (_ failingReadBody) Read(_ []byte) (int, error) {
	return 0, errors.New("read boom")
}
func (_ failingReadBody) Close() error {
	return nil
}
