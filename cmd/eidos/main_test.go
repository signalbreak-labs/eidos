package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/cobra"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRunEidosWith_WritesError(t *testing.T) {
	cmd := &cobra.Command{
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("boom")
		},
	}
	var buf bytes.Buffer
	code := runEidosWith(cmd, &buf)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("boom")) {
		t.Errorf("expected error in buffer, got %q", buf.String())
	}
}

func TestRunEidosWith_FallingBackToStderr(t *testing.T) {
	cmd := &cobra.Command{
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("fallback")
		},
	}
	code := runEidosWith(cmd, failingWriter{})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	// The error is written to the real stderr in this fallback path, which is
	// hard to capture in a unit test; we rely on the writer-failure path not
	// panicking and returning the correct exit code.
}

func TestRunEidosWith_Success(t *testing.T) {
	cmd := &cobra.Command{
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	var buf bytes.Buffer
	code := runEidosWith(cmd, &buf)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

// TestRunEidos covers the no-arg wrapper: it executes the real root command
// (which prints help and succeeds) and returns 0.
func TestRunEidos(t *testing.T) {
	if code := runEidos(); code != 0 {
		t.Errorf("runEidos() = %d, want 0", code)
	}
}

// TestMainFunction covers the main entry point by re-executing the test binary
// as a subprocess with an environment flag that makes it call main() directly.
// main() calls os.Exit(runEidos()), which runs the real root command (prints
// help, returns 0), so the subprocess must exit 0. This is the standard
// subprocess pattern for covering an os.Exit wrapper, which cannot be invoked
// in-process without terminating the test binary.
func TestMainFunction(t *testing.T) {
	if os.Getenv("EIDOS_TEST_MAIN") == "1" {
		main()
		return
	}
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestMainFunction") //nolint:gosec // re-executing the test binary is the standard os.Exit wrapper pattern
	cmd.Env = append(os.Environ(), "EIDOS_TEST_MAIN=1")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("main() subprocess exited with error: %v; stderr: %s", err, stderr.String())
	}
}
