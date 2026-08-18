package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/signalbreak-labs/eidos/pkg/api"
)

const (
	// shutdownGracePeriod is the maximum time allowed for the HTTP server to
	// drain in-flight requests during a graceful shutdown.
	shutdownGracePeriod = 10 * time.Second

	// serverReadHeaderTimeout limits the time allowed to read request headers.
	serverReadHeaderTimeout = 10 * time.Second
	// serverReadTimeout limits the time allowed to read the entire request.
	serverReadTimeout = 30 * time.Second
	// serverWriteTimeout limits the time allowed to write the response.
	serverWriteTimeout = 30 * time.Second
	// serverIdleTimeout limits idle keep-alive connections.
	serverIdleTimeout = 120 * time.Second
	// serverMaxHeaderBytes limits the maximum request header size.
	serverMaxHeaderBytes = 1 << 20 // 1 MiB
)

// newSignalContext wraps base in a signal-backed context for graceful shutdown.
// It is a package-level variable so tests can inject a mock context and verify
// the fallback path. Tests that override it must restore the original value,
// typically via defer.
var newSignalContext = func(base context.Context) (context.Context, context.CancelFunc) {
	if base == nil {
		base = context.Background()
	}
	return signal.NotifyContext(base, os.Interrupt, syscall.SIGTERM)
}

type apiFlags struct {
	host string
	port int
}

func newAPICmd() *cobra.Command {
	flags := &apiFlags{}

	cmd := &cobra.Command{
		Use:   "api",
		Short: "Start the Eidos HTTP API server",
		Long: `Starts a lightweight HTTP server that exposes Eidos endpoints.

Implemented endpoint:
  POST /api/v1/validate
    Accepts an OpenAPI document (JSON or YAML) and an optional top-level
    "config" string containing generator.yaml settings, then runs the
    live parse and normalize pipeline. Returns a structured report with
    validation status, diagnostics, a detected spec summary, an optional
    IR preview, and an optional suggested generator.yaml configuration.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAPI(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.host, "host", "127.0.0.1", "Host/interface to listen on")
	cmd.Flags().IntVar(&flags.port, "port", 8080, "Port to listen on")

	return cmd
}

func runAPI(cmd *cobra.Command, flags *apiFlags) error {
	if flags.port < 1 || flags.port > 65535 {
		return fmt.Errorf("invalid --port %d: must be 1-65535", flags.port)
	}

	logger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), nil))
	handler := newAPIHandler(logger)

	// net.JoinHostPort brackets a bare IPv6 literal host ("::1" -> "[::1]:8080"),
	// which the old fmt.Sprintf("%s:%d") produced as "::1:8080" — a string
	// net.SplitHostPort rejects with "too many colons" (N-64).
	addr := net.JoinHostPort(flags.host, strconv.Itoa(flags.port))
	// Use the command context so a canceled/timeout cmd.Context() can abort the
	// bind itself, not just the serve loop (L-5).
	listener, err := (&net.ListenConfig{}).Listen(cmd.Context(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("api server failed to bind %s: %w", addr, err)
	}

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}

	//nolint:errcheck // best-effort startup message
	fmt.Fprintf(cmd.OutOrStdout(), "Starting Eidos API server on %s\n", server.Addr)

	ctx, stop := newSignalContext(cmd.Context())
	defer stop()
	return serveGracefully(ctx, server, listener)
}

func serveGracefully(ctx context.Context, server *http.Server, listener net.Listener) error {
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = newSignalContext(context.Background())
		defer stop()
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("api server graceful shutdown failed: %w", err)
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api server failed: %w", err)
		}
		return nil
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api server failed: %w", err)
		}
		return nil
	}
}

// handleValidate returns a HandlerFunc that validates the incoming request.
func handleValidate() http.HandlerFunc {
	return api.NewValidateHandler()
}

// newAPIHandler returns the production request handler for the API server. It
// routes only the documented POST /api/v1/validate endpoint and returns 404 for
// any other path/method (L-6), then layers panic recovery and structured access
// logging over it so the server never crashes a connection and every request
// is observable.
func newAPIHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/validate", handleValidate())
	return accessLogMiddleware(recoveryMiddleware(mux.ServeHTTP, logger), logger)
}

// recoveryMiddleware catches panics in downstream handlers, logs them, and
// returns a 500 Internal Server Error response to the client.
func recoveryMiddleware(next http.HandlerFunc, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("api handler panic",
					slog.String("path", r.URL.Path),
					slog.String("method", r.Method),
					slog.Any("recover", rec),
				)
				if err := api.WriteJSONError(w, http.StatusInternalServerError, "internal server error"); err != nil {
					logger.Error("api: failed to write panic error response", slog.String("error", err.Error()))
				}
			}
		}()
		next.ServeHTTP(w, r)
	}
}

// responseRecorder captures the HTTP status code written by a downstream
// handler for access-logging purposes.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// accessLogMiddleware logs every request with method, path, status code, and
// duration.
func accessLogMiddleware(next http.HandlerFunc, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := newResponseRecorder(w)
		next.ServeHTTP(rr, r)
		logger.Info("request handled",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rr.statusCode),
			slog.Duration("duration", time.Since(start)),
		)
	}
}
