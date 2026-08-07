package main

import (
	"bytes"
	"errors"
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
