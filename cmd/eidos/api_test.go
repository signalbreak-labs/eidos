package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAPICommand_RegisteredFlags(t *testing.T) {
	cmd := newRootCmd()
	apiCmd, _, err := cmd.Find([]string{"api"})
	if err != nil {
		t.Fatalf("failed to find api command: %v", err)
	}
	if apiCmd == nil || apiCmd.Name() != "api" {
		t.Fatal("api command not registered")
	}

	if apiCmd.Flags().Lookup("port") == nil {
		t.Error("api command missing --port flag")
	}

	hostFlag := apiCmd.Flags().Lookup("host")
	if hostFlag == nil {
		t.Error("api command missing --host flag")
	} else if got := hostFlag.DefValue; got != "127.0.0.1" {
		t.Errorf("expected --host default 127.0.0.1, got %q", got)
	}
}

func TestAPICommand_ValidateEndpoint(t *testing.T) {
	server := httptest.NewServer(handleValidate())
	defer server.Close()

	spec := `{
		"openapi": "3.0.1",
		"info": {"title": "Pet Store", "version": "1.0.0"},
		"paths": {
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}}
				},
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			}
		}
	}`

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/validate", strings.NewReader(spec))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to reach %s: %v", server.URL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"valid":true`) {
		t.Errorf("expected valid:true in response body, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"version":"3.0.1"`) {
		t.Errorf("expected detected version 3.0.1, got: %s", bodyStr)
	}
}

func TestAPICommand_ValidateEndpoint_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/validate", handleValidate())
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/validate", http.NoBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to reach %s: %v", server.URL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 Method Not Allowed for GET, got %d", resp.StatusCode)
	}
}

func TestAPICommand_InvalidPort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		wantErr bool
	}{
		{"port zero invalid", "0", true},
		{"port one valid", "1", false},
		{"max port valid", "65535", false},
		{"max port plus one invalid", "65536", true},
		{"well above max invalid", "99999", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, out := newTestCommand("api", "--port", tt.port)
			// Use a short-lived context so valid ports don't block the test
			// waiting for a signal-backed shutdown.
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			cmd.SetContext(ctx)

			err := cmd.Execute()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for port %s, got nil", tt.port)
				}
				if !strings.Contains(err.Error(), "invalid --port") {
					t.Errorf("expected invalid port error, got: %v", err)
				}
				return
			}
			if err != nil {
				// A valid port can still fail to bind in a constrained test
				// environment; only fail if the error is the validation error.
				if strings.Contains(err.Error(), "invalid --port") {
					t.Fatalf("port %s unexpectedly rejected: %v", tt.port, err)
				}
				// Bind failures or short-context shutdowns are acceptable here;
				// we only care that the port was not rejected by validation.
				if !strings.Contains(err.Error(), "api server failed to bind") && !strings.Contains(err.Error(), "context deadline exceeded") {
					t.Fatalf("unexpected error for port %s: %v\noutput: %s", tt.port, err, out.String())
				}
			}
		})
	}
}

func TestServeGracefully(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	errCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		errCh <- serveGracefully(ctx, server, listener)
	}()

	// Wait for the server to be reachable.
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	reached := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, "http://"+addr+"/ready", http.NoBody)
		if err != nil {
			t.Fatalf("failed to create readiness request: %v", err)
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close() //nolint:errcheck // readiness probe: body close error is non-actionable
			reached = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !reached {
		t.Fatal("server did not become reachable within 5s")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}

	wg.Wait()
}

func TestServeGracefully_NilContext(t *testing.T) {
	// Capture the real signal context factory and restore it after the test.
	originalFactory := newSignalContext
	defer func() { newSignalContext = originalFactory }()

	ctx, cancel := context.WithCancel(context.Background())
	newSignalContext = func(_ context.Context) (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	addr := listener.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		//nolint:staticcheck // serveGracefully is explicitly tested with nil context.
		errCh <- serveGracefully(nil, server, listener)
	}()

	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	reached := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, "http://"+addr+"/ready", http.NoBody)
		if err != nil {
			t.Fatalf("failed to create readiness request: %v", err)
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close() //nolint:errcheck // readiness probe: body close error is non-actionable
			reached = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !reached {
		t.Fatal("server did not become reachable within 5s")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

func TestAPIHandler_PanicRecovery(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	panicHandler := func(http.ResponseWriter, *http.Request) { panic("intentional test panic") }
	handler := accessLogMiddleware(recoveryMiddleware(panicHandler, logger), logger)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/validate", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 after panic, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "internal server error") {
		t.Errorf("expected internal server error body, got %q", body)
	}
}

// TestAPIHandler_Routing locks in the L-6 fix: only POST /api/v1/validate is
// routed. An unknown path returns 404 (previously every path reached the
// validate handler, so POST /anything was parsed as a spec and returned 200/400).
func TestAPIHandler_Routing(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(newAPIHandler(logger))
	defer server.Close()

	// Unknown path with POST must be 404, not parsed as a spec.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/api/v1/anything", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown path, got %d", resp.StatusCode)
	}
}

func TestAPIHandler_AccessLog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	okHandler := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	handler := accessLogMiddleware(okHandler, logger)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/validate", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(buf.String(), `"status":200`) {
		t.Errorf("expected access log to contain status 200, got %q", buf.String())
	}
}

func TestAPIHandler_MaxHeaderBytes(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// httptest.NewServer does not enforce http.Server.MaxHeaderBytes, so a
	// custom server configured like the production one is required to exercise
	// the header-size limit (L-7 removed the dead httptest half that asserted
	// nothing).
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	custom := &http.Server{
		Handler:           newAPIHandler(logger),
		MaxHeaderBytes:    1 * 1024,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = custom.Serve(listener) }() //nolint:errcheck // test server: Serve error on shutdown is non-actionable
	defer custom.Close()                       //nolint:errcheck // test cleanup: server close error is non-actionable

	bigHeader := strings.Repeat("x", 4*1024)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+listener.Addr().String()+"/api/v1/validate", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for i := 0; i < 10; i++ {
		req.Header.Set(fmt.Sprintf("X-Test-%d", i), bigHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup: response body close error is non-actionable
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("expected status 431 Request Header Fields Too Large, got %d", resp.StatusCode)
	}
}
