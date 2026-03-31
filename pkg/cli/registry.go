// Package cli provides the Cobra root command and command registry for the
// MOCA CLI. Apps register commands via init() + MustRegisterCommand();
// framework-internal commands use explicit constructors called from a wiring file.
//
// Design ref: MOCA_CLI_SYSTEM_DESIGN.md §8 (lines 3363–3406)
// Validated by: spikes/cobra-ext/ADR-005-cobra-cli-extension.md
package cli

import (
	"fmt"
	"sync"

	"github.com/spf13/cobra"
)

var (
	rootCmd *cobra.Command
	mu      sync.Mutex

	// registry holds commands waiting to be attached to rootCmd.
	// RegisterCommand queues here; RootCommand() attaches and returns rootCmd.
	registry []*cobra.Command

	// registeredNames maps command name → command Use string for collision detection.
	registeredNames map[string]string
)

func init() {
	registeredNames = make(map[string]string)
	rootCmd = newRootCommand()
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moca",
		Short: "MOCA framework CLI",
		Long:  "MOCA — a metadata-driven, multitenant business application framework.",

		SilenceErrors: true,
		SilenceUsage:  true,

		// PersistentPreRunE is a no-op placeholder. MS-07-T2 will wire
		// context resolution (project/site/env detection) here.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	// Global persistent flags available to all commands.
	pf := cmd.PersistentFlags()
	pf.String("site", "", "Target site (overrides MOCA_SITE env and .moca/current_site)")
	pf.String("env", "", "Target environment (overrides MOCA_ENV env and .moca/environment)")
	pf.String("project", "", "Project root directory (overrides auto-detection)")
	pf.Bool("json", false, "Output in JSON format")
	pf.Bool("table", false, "Output in table format")
	pf.Bool("no-color", false, "Disable colored output")
	pf.Bool("verbose", false, "Enable verbose output")

	return cmd
}

// RegisterCommand queues a Cobra command for inclusion in the root command tree.
// It is safe to call from init() functions in app modules.
//
// Returns an error if a command with the same name has already been registered.
// Use MustRegisterCommand to panic on collision (the correct behavior for init()).
func RegisterCommand(cmd *cobra.Command) error {
	mu.Lock()
	defer mu.Unlock()

	name := cmd.Name()
	if prev, exists := registeredNames[name]; exists {
		return fmt.Errorf("command %q already registered (existing Use: %q)", name, prev)
	}
	registeredNames[name] = cmd.Use
	registry = append(registry, cmd)
	return nil
}

// MustRegisterCommand calls RegisterCommand and panics on collision.
// This is the intended function for app init() hooks: a command name collision
// is a configuration error that must be fixed at build time, not silently ignored.
// The panic surfaces immediately at program startup, before any request is served.
func MustRegisterCommand(cmd *cobra.Command) {
	if err := RegisterCommand(cmd); err != nil {
		panic("moca/cli: " + err.Error())
	}
}

// RootCommand attaches all queued registry commands to rootCmd and returns it.
// This should be called once in main() after all init() functions have run.
// Subsequent calls re-attach any newly queued commands (idempotent for empty queue).
func RootCommand() *cobra.Command {
	mu.Lock()
	defer mu.Unlock()

	for _, cmd := range registry {
		rootCmd.AddCommand(cmd)
	}
	registry = nil // clear to prevent double-add on subsequent calls
	return rootCmd
}

// RegisteredCommandNames returns the names of all currently registered commands.
// Used in tests to verify that init() registration fired correctly.
func RegisteredCommandNames() []string {
	mu.Lock()
	defer mu.Unlock()

	names := make([]string, 0, len(registeredNames))
	for name := range registeredNames {
		names = append(names, name)
	}
	return names
}

// ResetForTesting clears all registrations and recreates a fresh root command.
// ONLY for use in tests. Do not call in production code.
func ResetForTesting() {
	mu.Lock()
	defer mu.Unlock()

	registry = nil
	registeredNames = make(map[string]string)
	rootCmd = newRootCommand()
}
