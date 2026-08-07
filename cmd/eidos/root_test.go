package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestRootCommand_ExecuteSucceeds(t *testing.T) {
	cmd, out := newTestCommand()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected root command to execute without error, got %v", err)
	}

	if cmd.Long == "" {
		t.Fatal("root command has no Long description; assertion would be vacuous")
	}

	output := out.String()
	if !strings.Contains(output, cmd.Long) {
		t.Errorf("expected root help output to contain %q, got:\n%s", cmd.Long, output)
	}
}

func TestRootCommand_VersionFlag(t *testing.T) {
	cmd, out := newTestCommand("--version")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --version to execute without error, got %v", err)
	}

	got := strings.TrimSpace(out.String())
	// Build expected from the root command's name and the version var so this test
	// survives -ldflags overrides and documents the dependency on the `version` symbol.
	want := fmt.Sprintf("%s version %s", cmd.Name(), version)
	if got != want {
		t.Errorf("--version output mismatch: got %q, want %q", got, want)
	}
}
