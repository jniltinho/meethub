// Package cmd defines the meethub Cobra command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "meethub",
	Short:         "MeetHub — team chat with LiveKit meetings",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI and exits with status 1 on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
