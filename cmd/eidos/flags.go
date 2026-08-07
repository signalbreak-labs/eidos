package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Helpers for command construction.

// mustMarkFlagRequired marks a flag as required and panics if the flag is not
// registered on the command. This is only called during command construction,
// so a panic surfaces a programming error at startup rather than at runtime.
func mustMarkFlagRequired(cmd *cobra.Command, name string) {
	if err := cmd.MarkFlagRequired(name); err != nil {
		panic(fmt.Sprintf("failed to mark flag %q as required: %v", name, err))
	}
}
