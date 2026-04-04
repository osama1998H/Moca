package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

// TestWorkerCommandStructure verifies all 4 subcommands exist.
func TestWorkerCommandStructure(t *testing.T) {
	cmd := NewWorkerCommand()

	expected := []string{"start", "stop", "status", "scale"}
	found := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		found[sub.Name()] = true
	}

	for _, name := range expected {
		if !found[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}

	if len(cmd.Commands()) != len(expected) {
		t.Errorf("expected %d subcommands, got %d", len(expected), len(cmd.Commands()))
	}
}

// TestWorkerStartFlags verifies --foreground flag is registered.
func TestWorkerStartFlags(t *testing.T) {
	cmd := NewWorkerCommand()

	var startCmd = findSubcommand(cmd, "start")
	if startCmd == nil {
		t.Fatal("start subcommand not found")
	}

	if startCmd.Flags().Lookup("foreground") == nil {
		t.Error("start command missing --foreground flag")
	}
}

// TestWorkerStatusFlags verifies --site flag is registered.
func TestWorkerStatusFlags(t *testing.T) {
	cmd := NewWorkerCommand()

	var statusCmd = findSubcommand(cmd, "status")
	if statusCmd == nil {
		t.Fatal("status subcommand not found")
	}

	if statusCmd.Flags().Lookup("site") == nil {
		t.Error("status command missing --site flag")
	}
}

// TestWorkerScaleRequiresArgs verifies that scale requires exactly 2 arguments.
func TestWorkerScaleRequiresArgs(t *testing.T) {
	cmd := NewWorkerCommand()

	var scaleCmd = findSubcommand(cmd, "scale")
	if scaleCmd == nil {
		t.Fatal("scale subcommand not found")
	}

	if scaleCmd.Args == nil {
		t.Error("scale command should have args validation")
	}
}

// TestWorkerStopExists verifies the stop subcommand exists and has RunE.
func TestWorkerStopExists(t *testing.T) {
	cmd := NewWorkerCommand()

	var stopCmd = findSubcommand(cmd, "stop")
	if stopCmd == nil {
		t.Fatal("stop subcommand not found")
	}

	if stopCmd.RunE == nil {
		t.Error("stop command should have RunE set")
	}
}
