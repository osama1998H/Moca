package stuba

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewHelloCommand returns the hello command using the explicit constructor pattern.
// This is an alternative to init()-based registration, recommended for cases where
// the caller needs full control over registration ordering and error handling
// (e.g., framework-internal commands, or when testing individual commands in isolation).
func NewHelloCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stub-a:hello-explicit",
		Short: "Say hello from stub-a (explicit constructor pattern)",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(c.OutOrStdout(), "Hello from stub-a (explicit)!")
			return nil
		},
	}
}
