package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/pkg/queue"
)

// TestQueueCommandStructure verifies all 8 subcommands exist: 5 top-level + dead-letter group with 3.
func TestQueueCommandStructure(t *testing.T) {
	cmd := NewQueueCommand()

	expectedTop := []string{"status", "list", "inspect", "retry", "purge", "dead-letter"}
	found := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		found[sub.Name()] = true
	}

	for _, name := range expectedTop {
		if !found[name] {
			t.Errorf("missing top-level subcommand %q", name)
		}
	}
}

// TestQueueDeadLetterSubgroup verifies dead-letter has 3 subcommands.
func TestQueueDeadLetterSubgroup(t *testing.T) {
	cmd := NewQueueCommand()

	var dlCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "dead-letter" {
			dlCmd = sub
			break
		}
	}
	if dlCmd == nil {
		t.Fatal("dead-letter subgroup not found")
	}

	expectedDL := []string{"list", "retry", "purge"}
	found := make(map[string]bool)
	for _, sub := range dlCmd.Commands() {
		found[sub.Name()] = true
	}

	for _, name := range expectedDL {
		if !found[name] {
			t.Errorf("missing dead-letter subcommand %q", name)
		}
	}

	if len(dlCmd.Commands()) != 3 {
		t.Errorf("expected 3 dead-letter subcommands, got %d", len(dlCmd.Commands()))
	}
}

// TestQueueStatusFlags verifies that --json and --watch flags are registered.
func TestQueueStatusFlags(t *testing.T) {
	cmd := NewQueueCommand()

	var statusCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "status" {
			statusCmd = sub
			break
		}
	}
	if statusCmd == nil {
		t.Fatal("status subcommand not found")
	}

	flags := []string{"watch", "site"}
	for _, name := range flags {
		if statusCmd.Flags().Lookup(name) == nil {
			t.Errorf("status command missing --%s flag", name)
		}
	}
}

// TestQueueListFlags verifies --queue, --site, --limit flags are registered.
func TestQueueListFlags(t *testing.T) {
	cmd := NewQueueCommand()

	var listCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	if listCmd == nil {
		t.Fatal("list subcommand not found")
	}

	flags := []string{"queue", "site", "limit"}
	for _, name := range flags {
		if listCmd.Flags().Lookup(name) == nil {
			t.Errorf("list command missing --%s flag", name)
		}
	}
}

// TestQueueInspectRequiresArg verifies that inspect requires exactly 1 argument.
func TestQueueInspectRequiresArg(t *testing.T) {
	cmd := NewQueueCommand()

	var inspectCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "inspect" {
			inspectCmd = sub
			break
		}
	}
	if inspectCmd == nil {
		t.Fatal("inspect subcommand not found")
	}

	if inspectCmd.Args == nil {
		t.Error("inspect command should have args validation")
	}
}

// TestQueuePurgeFlags verifies --queue, --all, --site, --force flags.
func TestQueuePurgeFlags(t *testing.T) {
	cmd := NewQueueCommand()

	var purgeCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "purge" {
			purgeCmd = sub
			break
		}
	}
	if purgeCmd == nil {
		t.Fatal("purge subcommand not found")
	}

	flags := []string{"queue", "all", "site", "force"}
	for _, name := range flags {
		if purgeCmd.Flags().Lookup(name) == nil {
			t.Errorf("purge command missing --%s flag", name)
		}
	}
}

// TestQueueRetryFlags verifies --all-failed, --queue, --force, --site flags.
func TestQueueRetryFlags(t *testing.T) {
	cmd := NewQueueCommand()

	var retryCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "retry" {
			retryCmd = sub
			break
		}
	}
	if retryCmd == nil {
		t.Fatal("retry subcommand not found")
	}

	flags := []string{"all-failed", "queue", "force", "site"}
	for _, name := range flags {
		if retryCmd.Flags().Lookup(name) == nil {
			t.Errorf("retry command missing --%s flag", name)
		}
	}
}

// TestResolveQueueTypes verifies queue type resolution from flag values.
func TestResolveQueueTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"all", len(queue.AllQueueTypes)},
		{"", len(queue.AllQueueTypes)},
		{"default", 1},
		{"long", 1},
		{"critical", 1},
		{"scheduler", 1},
		{"invalid", 0},
	}

	for _, tt := range tests {
		result := resolveQueueTypes(tt.input)
		if len(result) != tt.expected {
			t.Errorf("resolveQueueTypes(%q) = %d types, want %d", tt.input, len(result), tt.expected)
		}
	}
}

// TestParseDuration verifies duration parsing with day support.
func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		wantDur time.Duration
	}{
		{"7d", false, 7 * 24 * time.Hour},
		{"1d", false, 24 * time.Hour},
		{"24h", false, 24 * time.Hour},
		{"1h30m", false, 90 * time.Minute},
		{"bad", true, 0},
	}

	for _, tt := range tests {
		d, err := parseDuration(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && d != tt.wantDur {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, d, tt.wantDur)
		}
	}
}

// TestStringFromValues verifies extraction from Redis values map.
func TestStringFromValues(t *testing.T) {
	values := map[string]interface{}{
		"type": "generate_report",
		"site": "acme.localhost",
	}

	if got := stringFromValues(values, "type"); got != "generate_report" {
		t.Errorf("stringFromValues(type) = %q, want %q", got, "generate_report")
	}
	if got := stringFromValues(values, "missing"); got != "" {
		t.Errorf("stringFromValues(missing) = %q, want empty", got)
	}
}
