package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(runEidos())
}

func runEidos() int {
	return runEidosWith(rootCmd, os.Stderr)
}

// runEidosWith executes cmd and writes any error to stderr, returning the exit
// code. It lets tests supply a synthetic command and a bytes.Buffer instead of
// mutating global os.Args/os.Stderr.
func runEidosWith(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			// Best-effort fallback: if the supplied writer fails, print to the
			// process stderr so the error is not lost entirely.
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	return 0
}
