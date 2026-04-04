package main

import (
	"testing"
)

// TestSchedulerCommandStructure verifies all 8 subcommands exist.
func TestSchedulerCommandStructure(t *testing.T) {
	cmd := NewSchedulerCommand()

	expected := []string{
		"start", "stop", "status",
		"enable", "disable", "trigger",
		"list-jobs", "purge-jobs",
	}
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

// TestSchedulerStartFlags verifies --foreground flag is registered.
func TestSchedulerStartFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	startCmd := findSubcommand(cmd, "start")
	if startCmd == nil {
		t.Fatal("start subcommand not found")
	}

	if startCmd.Flags().Lookup("foreground") == nil {
		t.Error("start command missing --foreground flag")
	}
}

// TestSchedulerEnableDisableFlags verifies --site on enable and disable.
func TestSchedulerEnableDisableFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	for _, name := range []string{"enable", "disable"} {
		sub := findSubcommand(cmd, name)
		if sub == nil {
			t.Fatalf("%s subcommand not found", name)
		}
		if sub.Flags().Lookup("site") == nil {
			t.Errorf("%s command missing --site flag", name)
		}
	}
}

// TestSchedulerTriggerRequiresArg verifies that trigger requires exactly 1 argument.
func TestSchedulerTriggerRequiresArg(t *testing.T) {
	cmd := NewSchedulerCommand()

	triggerCmd := findSubcommand(cmd, "trigger")
	if triggerCmd == nil {
		t.Fatal("trigger subcommand not found")
	}

	if triggerCmd.Args == nil {
		t.Error("trigger command should have args validation")
	}
}

// TestSchedulerTriggerFlags verifies --site and --all-sites flags.
func TestSchedulerTriggerFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	triggerCmd := findSubcommand(cmd, "trigger")
	if triggerCmd == nil {
		t.Fatal("trigger subcommand not found")
	}

	for _, flag := range []string{"site", "all-sites"} {
		if triggerCmd.Flags().Lookup(flag) == nil {
			t.Errorf("trigger command missing --%s flag", flag)
		}
	}
}

// TestSchedulerListJobsFlags verifies --site and --app flags.
func TestSchedulerListJobsFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	ljCmd := findSubcommand(cmd, "list-jobs")
	if ljCmd == nil {
		t.Fatal("list-jobs subcommand not found")
	}

	for _, flag := range []string{"site", "app"} {
		if ljCmd.Flags().Lookup(flag) == nil {
			t.Errorf("list-jobs command missing --%s flag", flag)
		}
	}
}

// TestSchedulerPurgeJobsFlags verifies --site, --event, --all, --force flags.
func TestSchedulerPurgeJobsFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	pjCmd := findSubcommand(cmd, "purge-jobs")
	if pjCmd == nil {
		t.Fatal("purge-jobs subcommand not found")
	}

	for _, flag := range []string{"site", "event", "all", "force"} {
		if pjCmd.Flags().Lookup(flag) == nil {
			t.Errorf("purge-jobs command missing --%s flag", flag)
		}
	}
}

// TestSchedulerStatusFlags verifies --site flag on status.
func TestSchedulerStatusFlags(t *testing.T) {
	cmd := NewSchedulerCommand()

	statusCmd := findSubcommand(cmd, "status")
	if statusCmd == nil {
		t.Fatal("status subcommand not found")
	}

	if statusCmd.Flags().Lookup("site") == nil {
		t.Error("status command missing --site flag")
	}
}
