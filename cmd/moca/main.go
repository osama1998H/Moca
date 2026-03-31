package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	clicontext "github.com/moca-framework/moca/internal/context"
	"github.com/moca-framework/moca/internal/output"
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
	root.AddCommand(NewVersionCommand(), NewCompletionCommand())
	root.AddCommand(allCommands()...)

	if err := root.Execute(); err != nil {
		handleError(root, err)
		os.Exit(1)
	}
}

// handleError formats and prints the error to stderr.
// CLIError gets the rich 5-section format; other errors get a simple message.
// In JSON mode, errors are emitted as JSON objects on stderr.
func handleError(root *cobra.Command, err error) {
	noColor, _ := root.Flags().GetBool("no-color")
	jsonFlag, _ := root.Flags().GetBool("json")

	var cliErr *output.CLIError

	if jsonFlag {
		if errors.As(err, &cliErr) {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
				"error":     cliErr.Message,
				"context":   cliErr.Context,
				"cause":     cliErr.Cause,
				"fix":       cliErr.Fix,
				"reference": cliErr.Reference,
			})
		} else {
			_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
				"error": err.Error(),
			})
		}
		return
	}

	cc := output.NewColorConfig(noColor, os.Stderr)
	if errors.As(err, &cliErr) {
		cliErr.Format(os.Stderr, cc)
	} else {
		fmt.Fprintf(os.Stderr, "%s %s\n", cc.Error("Error:"), err.Error())
	}
}
