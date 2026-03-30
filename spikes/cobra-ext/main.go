// Package main is the entry point for the cobra-ext spike binary (MS-00-T4, Spike 5).
//
// The blank imports below trigger each app's init() function, which registers
// Cobra commands via MustRegisterCommand(). By the time main() is called, all
// init() functions have completed and the registry is fully populated.
//
// This models the production moca-server binary pattern described in:
// MOCA_CLI_SYSTEM_DESIGN.md §8 (lines 3363-3406)
//
// This is throwaway spike code. Do not promote to pkg/.
package main

import (
	"fmt"
	"os"

	"github.com/moca-framework/moca/spikes/cobra-ext/framework/cmd"

	// Blank imports trigger init()-based command registration from each app.
	_ "github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-a"
	_ "github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-b"
)

func main() {
	root := cmd.RootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
