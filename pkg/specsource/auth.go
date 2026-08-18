package specsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ApplyAuth attaches the configured authentication headers to the spec request,
// resolving credential values from environment variables immediately before the
// request. Empty required env vars fail loud rather than sending an
// unauthenticated request the vendor will reject.
func ApplyAuth(req *http.Request, auth *Auth) error {
	if auth == nil || strings.TrimSpace(auth.Scheme) == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(auth.Scheme)) {
	case "bearer":
		tok := os.Getenv(strings.TrimSpace(auth.TokenEnv))
		if tok == "" {
			return fmt.Errorf("spec auth scheme bearer requires env var %s to be set", auth.TokenEnv)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	case "basic":
		user := os.Getenv(strings.TrimSpace(auth.UsernameEnv))
		pass := os.Getenv(strings.TrimSpace(auth.PasswordEnv))
		// Both halves must be present: sending Basic with only one credential is
		// a silent misconfiguration (N-59; the prior code only errored when BOTH
		// were unset, so a missing password sent a username-only header).
		if user == "" || pass == "" {
			return fmt.Errorf("spec auth scheme basic requires env vars %s and %s to be set", auth.UsernameEnv, auth.PasswordEnv)
		}
		req.SetBasicAuth(user, pass)
	case "apikey":
		key := os.Getenv(strings.TrimSpace(auth.KeyEnv))
		if key == "" {
			return fmt.Errorf("spec auth scheme apiKey requires env var %s to be set", auth.KeyEnv)
		}
		header := strings.TrimSpace(auth.HeaderName)
		if header == "" {
			return fmt.Errorf("spec auth scheme apiKey requires a header_name")
		}
		req.Header.Set(header, key)
	case "oauth2-client-credentials":
		id := os.Getenv(strings.TrimSpace(auth.ClientIDEnv))
		secret := os.Getenv(strings.TrimSpace(auth.ClientSecretEnv))
		tok, err := FetchClientCredentialsToken(req.Context(), auth.TokenURL, id, secret, DefaultTimeout, DefaultMaxBytes)
		if err != nil {
			return fmt.Errorf("failed to obtain spec auth token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	default:
		return fmt.Errorf("unknown spec auth scheme %q (want bearer, basic, apiKey, or oauth2-client-credentials)", auth.Scheme)
	}
	return nil
}

// FetchClientCredentialsToken performs an OAuth2 client-credentials grant
// against tokenURL, mirroring the generated client's clientCredentialsForm. It
// is exported so the CLI can test the token flow directly.
func FetchClientCredentialsToken(ctx context.Context, tokenURL, clientID, clientSecret string, timeout time.Duration, maxBytes int64) (string, error) {
	if strings.TrimSpace(tokenURL) == "" {
		return "", fmt.Errorf("oauth2-client-credentials requires a token_url")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort response body close
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return "", fmt.Errorf("token response exceeds the %d-byte maximum", maxBytes)
	}
	// Extract the access_token field. A tiny inline parser avoids a heavy
	// dependency for one field; the response shape is the standard OAuth2
	// JSON object.
	token, err := jsonField(body, "access_token")
	if err != nil {
		return "", fmt.Errorf("token response has no access_token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("token response access_token is empty")
	}
	return token, nil
}

// jsonField extracts a top-level string field from a JSON object. It is used
// for the OAuth2 token response (access_token); the body is small and the
// structure is the standard OAuth2 JSON object.
func jsonField(body []byte, name string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return "", fmt.Errorf("token response is not a JSON object: %w", err)
	}
	raw, ok := m[name]
	if !ok {
		return "", fmt.Errorf("field %q not found", name)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("field %q is not a string", name)
	}
	return s, nil
}

// JSONField is the exported form of jsonField, used by tests that exercise the
// token response parser directly.
func JSONField(body []byte, name string) (string, error) {
	return jsonField(body, name)
}
