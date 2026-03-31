package main

import (
	"fmt"
	"os"

	clicontext "github.com/moca-framework/moca/internal/context"
	"github.com/moca-framework/moca/pkg/cli"
	"github.com/spf13/cobra"
)

// Build-time variables injected via -ldflags.
// See Makefile LDFLAGS: -X main.Version=... -X main.Commit=... -X main.BuildDate=...
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	root := cli.RootCommand()

	// Wire context resolution into the root command's PersistentPreRunE hook.
	// This runs before every command, resolving project/site/environment from
	// the 6-level priority pipeline (flags → env → state files → config → auto-detect → defaults).
	// Project detection is non-fatal: commands that need a project check ctx.Project != nil.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cc, err := clicontext.Resolve(cmd)
		if err != nil {
			return err
		}
		cmd.SetContext(clicontext.WithCLIContext(cmd.Context(), cc))
		return nil
	}

	// Register framework-internal commands using explicit constructors.
	// App-contributed commands use init() + cli.MustRegisterCommand() instead.
	root.AddCommand(
		NewVersionCommand(),
		NewCompletionCommand(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
