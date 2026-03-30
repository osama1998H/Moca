package stubb

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewGreetCommand returns the greet command using the explicit constructor pattern.
func NewGreetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stub-b:greet-explicit",
		Short: "Greet from stub-b (explicit constructor pattern)",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(c.OutOrStdout(), "Greetings from stub-b (explicit)!")
			return nil
		},
	}
}
