package main

import (
	"bytes"

	"github.com/spf13/cobra"
)

// newTestCommand builds a root command wired to an in-memory buffer for tests.
// It re-enables cobra's default error/usage printing because many existing
// tests assert on that output. Production keeps SilenceErrors/SilenceUsage
// true so main.go controls the single stderr write.
func newTestCommand(args ...string) (*cobra.Command, *bytes.Buffer) {
	cmd := newRootCmd()
	cmd.SilenceErrors = false
	cmd.SilenceUsage = false
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return cmd, out
}
