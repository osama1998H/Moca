package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moca-framework/moca/pkg/cli"
)

// TestAllCommandGroupsRegistered verifies that "moca help" output contains
// all 24 command groups plus the top-level commands.
func TestAllCommandGroupsRegistered(t *testing.T) {
	cli.ResetForTesting()

	root := cli.RootCommand()
	root.AddCommand(NewVersionCommand(), NewCompletionCommand())
	root.AddCommand(allCommands()...)

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root.Execute() error: %v", err)
	}

	helpOutput := buf.String()

	// All command groups that must appear in help output
	expectedCommands := []string{
		"site", "app", "worker", "scheduler",
		"db", "backup", "config", "deploy",
		"generate", "dev", "test", "build",
		"user", "api", "search", "cache",
		"queue", "events", "translate", "log",
		"monitor",
		// Top-level commands
		"init", "status", "doctor",
		"serve", "stop", "restart",
		// Pre-existing
		"version", "completion",
	}

	for _, cmd := range expectedCommands {
		if !strings.Contains(helpOutput, cmd) {
			t.Errorf("help output missing command %q", cmd)
		}
	}
}

// TestPlaceholderSubcommandsReturnNotImplemented verifies that placeholder
// subcommands return the expected CLIError. Uses "site migrate" which remains
// a placeholder after MS-11-T4 implemented the site operational commands.
func TestPlaceholderSubcommandsReturnNotImplemented(t *testing.T) {
	cli.ResetForTesting()

	root := cli.RootCommand()
	root.AddCommand(allCommands()...)

	// Disable context resolution for this test.
	root.PersistentPreRunE = nil

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"site", "migrate"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from placeholder command, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected 'not implemented' error, got: %s", err.Error())
	}
}

// TestDoctorCommandExists verifies that the doctor command is registered
// and has the expected substructure.
func TestDoctorCommandExists(t *testing.T) {
	cli.ResetForTesting()

	root := cli.RootCommand()
	root.AddCommand(allCommands()...)

	for _, cmd := range root.Commands() {
		if cmd.Name() == "doctor" {
			if cmd.Short != "Diagnose system health" {
				t.Errorf("unexpected Short: %q", cmd.Short)
			}
			return
		}
	}
	t.Error("doctor command not found in root commands")
}

// TestServeHasStartAlias verifies that "serve" has "start" as an alias.
func TestServeHasStartAlias(t *testing.T) {
	cmd := NewServeCommand()
	for _, alias := range cmd.Aliases {
		if alias == "start" {
			return
		}
	}
	t.Error("serve command missing 'start' alias")
}

// TestNestedSubgroups verifies api keys and queue dead-letter nesting.
func TestNestedSubgroups(t *testing.T) {
	apiCmd := NewAPICommand()

	var keysFound, webhooksFound bool
	for _, sub := range apiCmd.Commands() {
		switch sub.Name() {
		case "keys":
			keysFound = true
			if len(sub.Commands()) != 4 {
				t.Errorf("api keys: expected 4 subcommands, got %d", len(sub.Commands()))
			}
		case "webhooks":
			webhooksFound = true
			if len(sub.Commands()) != 3 {
				t.Errorf("api webhooks: expected 3 subcommands, got %d", len(sub.Commands()))
			}
		}
	}
	if !keysFound {
		t.Error("api command missing 'keys' subgroup")
	}
	if !webhooksFound {
		t.Error("api command missing 'webhooks' subgroup")
	}

	queueCmd := NewQueueCommand()
	for _, sub := range queueCmd.Commands() {
		if sub.Name() == "dead-letter" {
			if len(sub.Commands()) != 3 {
				t.Errorf("queue dead-letter: expected 3 subcommands, got %d", len(sub.Commands()))
			}
			return
		}
	}
	t.Error("queue command missing 'dead-letter' subgroup")
}
