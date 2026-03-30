// Package cmd provides the root Cobra command and the RegisterCommand registry
// for the cobra-ext spike (MS-00-T4, Spike 5).
//
// This mirrors the production pkg/cli package that will be implemented in MS-01.
// MOCA apps register Cobra commands via init() functions in their hooks.go files,
// which call RegisterCommand() or MustRegisterCommand().
// The main binary uses blank imports to trigger the init() calls.
//
// Design ref: MOCA_CLI_SYSTEM_DESIGN.md §8 (lines 3363-3406)
package cmd

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
	rootCmd = &cobra.Command{
		Use:   "moca",
		Short: "MOCA framework CLI (spike)",
		Long:  "MOCA framework CLI — spike validation of init()-based command registration.",
	}
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
	rootCmd = &cobra.Command{
		Use:   "moca",
		Short: "MOCA framework CLI (spike)",
		Long:  "MOCA framework CLI — spike validation of init()-based command registration.",
	}
}
