package main

import (
	"fmt"
	"os"

	"github.com/moca-framework/moca/pkg/cli"
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
