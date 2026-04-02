// Package stuba is a stub app module for the cobra-ext spike (MS-00-T4).
// The init() function registers this app's Cobra commands via the framework's
// RegisterCommand registry. The registration fires automatically when the main
// binary blank-imports this package.
//
// Pattern (from MOCA_CLI_SYSTEM_DESIGN.md §8, lines 3368-3390):
//
//	import _ "github.com/osama1998H/moca/apps/stub-a"  // triggers init()
package stuba

import (
	"fmt"

	"github.com/osama1998H/moca/spikes/cobra-ext/framework/cmd"
	"github.com/spf13/cobra"
)

func init() {
	cmd.MustRegisterCommand(&cobra.Command{
		Use:   "stub-a:hello",
		Short: "Say hello from stub-a",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(c.OutOrStdout(), "Hello from stub-a!")
			return nil
		},
	})
}
