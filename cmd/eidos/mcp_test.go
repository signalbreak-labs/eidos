package main

import (
	"strings"
	"testing"
)

func TestMCPCommand_Registered(t *testing.T) {
	cmd := newRootCmd()
	mcpCmd, _, err := cmd.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("failed to find mcp command: %v", err)
	}
	if mcpCmd == nil || mcpCmd.Name() != "mcp" {
		t.Fatal("mcp command not registered")
	}
}

// TestNewMCPCmd_Metadata checks command identity (Use, RunE) and a loose
// substring match on Short. The exact Short wording is documentation and may
// be tweaked; asserting it verbatim makes the test brittle (L-9). Use is an
// API contract and stays exact.
func TestNewMCPCmd_Metadata(t *testing.T) {
	cmd := newMCPCmd()

	if cmd.Use != "mcp" {
		t.Errorf("Use: got %q, want %q", cmd.Use, "mcp")
	}
	if !strings.Contains(strings.ToLower(cmd.Short), "mcp") {
		t.Errorf("Short: got %q, want a description mentioning MCP", cmd.Short)
	}
	if cmd.RunE == nil {
		t.Error("RunE is nil; expected a non-nil handler")
	}
}
