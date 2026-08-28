package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestWriteStarterGeneratorConfigBytes_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}

	// This test mutates the process-wide umask to make the asserted 0o600 mode
	// deterministic. Unix has no per-goroutine umask, so this is safe ONLY because
	// no test in this package calls t.Parallel(); do not add t.Parallel here or
	// to sibling tests without reworking this (L-20).
	oldMask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(oldMask) })

	tmp := t.TempDir()
	data := []byte("provider:\n  name: generated\n")

	t.Run("non-force creates file with 0o600", func(t *testing.T) {
		output := filepath.Join(tmp, "nonforce", "generator.yaml")
		if err := WriteStarterGeneratorConfigBytes(output, data, false); err != nil {
			t.Fatalf("WriteStarterGeneratorConfigBytes failed: %v", err)
		}
		info, err := os.Stat(output)
		if err != nil {
			t.Fatalf("stat generated config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("expected file permission 0o600, got 0o%03o", got)
		}
	})

	t.Run("force creates file with 0o600", func(t *testing.T) {
		output := filepath.Join(tmp, "force", "generator.yaml")
		if err := WriteStarterGeneratorConfigBytes(output, data, true); err != nil {
			t.Fatalf("WriteStarterGeneratorConfigBytes force failed: %v", err)
		}
		info, err := os.Stat(output)
		if err != nil {
			t.Fatalf("stat generated config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("expected file permission 0o600 after force write, got 0o%03o", got)
		}
	})
}

func TestWriteStarterGeneratorConfigBytes_AtomicWrite(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "generator.yaml")
	data := []byte("provider:\n  name: test\n")

	if err := WriteStarterGeneratorConfigBytes(output, data, false); err != nil {
		t.Fatalf("WriteStarterGeneratorConfigBytes failed: %v", err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %q, want %q", got, data)
	}
	tmpPath := output + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected temp file %q to be removed", tmpPath)
	}
}

func TestWriteStarterGeneratorConfigBytes_RefusesOverwrite(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "generator.yaml")
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	err := WriteStarterGeneratorConfigBytes(output, []byte("new"), false)
	if err == nil {
		t.Fatal("expected error when overwriting without force")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("expected overwrite refusal, got %q", err.Error())
	}
}

func TestWriteStarterGeneratorConfigBytes_ForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	output := filepath.Join(tmp, "generator.yaml")
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	data := []byte("provider:\n  name: force\n")
	if err := WriteStarterGeneratorConfigBytes(output, data, true); err != nil {
		t.Fatalf("force overwrite failed: %v", err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %q, want %q", got, data)
	}
}
