// Package stubb is a stub app module for the cobra-ext spike (MS-00-T4).
// The init() function registers this app's Cobra commands via MustRegisterCommand.
package stubb

import (
	"fmt"

	"github.com/moca-framework/moca/spikes/cobra-ext/framework/cmd"
	"github.com/spf13/cobra"
)

func init() {
	cmd.MustRegisterCommand(&cobra.Command{
		Use:   "stub-b:greet",
		Short: "Greet from stub-b",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(c.OutOrStdout(), "Greetings from stub-b!")
			return nil
		},
	})
}
