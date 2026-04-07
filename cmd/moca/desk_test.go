package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDeskCommandSubcommands(t *testing.T) {
	cmd := NewDeskCommand()

	expected := []string{"install", "update", "dev"}
	subs := cmd.Commands()
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}

	names := make(map[string]bool)
	for _, sub := range subs {
		names[sub.Name()] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}

	// Verify all subcommands have real implementations (non-nil RunE).
	for _, sub := range subs {
		if sub.RunE == nil {
			t.Errorf("subcommand %q has nil RunE (still a placeholder?)", sub.Name())
		}
	}
}

func TestDeskInstallFlags(t *testing.T) {
	cmd := NewDeskCommand()

	var installCmd = findDeskSubcommand(cmd, "install")
	if installCmd == nil {
		t.Fatal("install subcommand not found")
	}

	if installCmd.Flags().Lookup("verbose") == nil {
		t.Error("install subcommand missing flag --verbose")
	}
}

func TestDeskUpdateFlags(t *testing.T) {
	cmd := NewDeskCommand()

	var updateCmd = findDeskSubcommand(cmd, "update")
	if updateCmd == nil {
		t.Fatal("update subcommand not found")
	}

	if updateCmd.Flags().Lookup("verbose") == nil {
		t.Error("update subcommand missing flag --verbose")
	}
}

func TestDeskDevFlags(t *testing.T) {
	cmd := NewDeskCommand()

	var devCmd = findDeskSubcommand(cmd, "dev")
	if devCmd == nil {
		t.Fatal("dev subcommand not found")
	}

	if devCmd.Flags().Lookup("port") == nil {
		t.Error("dev subcommand missing flag --port")
	}
}

func TestInitCommandHasSkipDeskFlag(t *testing.T) {
	cmd := NewInitCommand()

	if cmd.Flags().Lookup("skip-desk") == nil {
		t.Error("init command missing flag --skip-desk")
	}
}

// findDeskSubcommand returns the named subcommand of cmd, or nil if not found.
func findDeskSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}
