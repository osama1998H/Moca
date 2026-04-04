package main

import (
	"testing"
	"time"
)

// TestMonitorCommandStructure verifies all 3 subcommands exist.
func TestMonitorCommandStructure(t *testing.T) {
	cmd := NewMonitorCommand()

	expected := []string{"live", "metrics", "audit"}
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

// TestMonitorAuditFlags verifies flags for the audit command.
func TestMonitorAuditFlags(t *testing.T) {
	cmd := NewMonitorCommand()

	var auditCmd = findSubcommand(cmd, "audit")
	if auditCmd == nil {
		t.Fatal("audit subcommand not found")
	}

	flags := []string{"site", "user", "doctype", "action", "since", "limit"}
	for _, name := range flags {
		if auditCmd.Flags().Lookup(name) == nil {
			t.Errorf("audit command missing --%s flag", name)
		}
	}

	if auditCmd.RunE == nil {
		t.Error("audit command should have RunE set")
	}
}

// TestMonitorMetricsFlags verifies flags for the metrics command.
func TestMonitorMetricsFlags(t *testing.T) {
	cmd := NewMonitorCommand()

	var metricsCmd = findSubcommand(cmd, "metrics")
	if metricsCmd == nil {
		t.Fatal("metrics subcommand not found")
	}

	if metricsCmd.Flags().Lookup("port") == nil {
		t.Error("metrics command missing --port flag")
	}

	if metricsCmd.RunE == nil {
		t.Error("metrics command should have RunE set")
	}
}

// TestMonitorLiveDeferred verifies the live command exists with RunE.
func TestMonitorLiveDeferred(t *testing.T) {
	cmd := NewMonitorCommand()

	var liveCmd = findSubcommand(cmd, "live")
	if liveCmd == nil {
		t.Fatal("live subcommand not found")
	}

	if liveCmd.RunE == nil {
		t.Error("live command should have RunE set")
	}
}

// TestParseSinceTimeDuration verifies duration-based --since parsing.
func TestParseSinceTimeDuration(t *testing.T) {
	now := time.Now()

	// Test standard Go duration.
	result, err := parseSinceTime("1h")
	if err != nil {
		t.Fatalf("parseSinceTime(\"1h\") error: %v", err)
	}
	diff := now.Sub(result)
	if diff < 59*time.Minute || diff > 61*time.Minute {
		t.Errorf("parseSinceTime(\"1h\") should be ~1h ago, got %v", diff)
	}

	// Test day-based duration.
	result, err = parseSinceTime("7d")
	if err != nil {
		t.Fatalf("parseSinceTime(\"7d\") error: %v", err)
	}
	diff = now.Sub(result)
	expectedDays := 7 * 24 * time.Hour
	if diff < expectedDays-time.Minute || diff > expectedDays+time.Minute {
		t.Errorf("parseSinceTime(\"7d\") should be ~7d ago, got %v", diff)
	}
}

// TestParseSinceTimeAbsolute verifies absolute timestamp parsing.
func TestParseSinceTimeAbsolute(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-01-15", "2026-01-15 00:00:00 +0000 UTC"},
		{"2026-01-15 10:30:00", "2026-01-15 10:30:00 +0000 UTC"},
	}

	for _, tt := range tests {
		result, err := parseSinceTime(tt.input)
		if err != nil {
			t.Errorf("parseSinceTime(%q) error: %v", tt.input, err)
			continue
		}
		got := result.UTC().Format("2006-01-02 15:04:05 -0700 MST")
		if got != tt.expected {
			t.Errorf("parseSinceTime(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestParseSinceTimeInvalid verifies that invalid inputs return errors.
func TestParseSinceTimeInvalid(t *testing.T) {
	_, err := parseSinceTime("not-a-time")
	if err == nil {
		t.Error("parseSinceTime(\"not-a-time\") should return error")
	}
}

// TestPluralY verifies the pluralization helper.
func TestPluralY(t *testing.T) {
	if got := pluralY(1); got != "y" {
		t.Errorf("pluralY(1) = %q, want \"y\"", got)
	}
	if got := pluralY(0); got != "ies" {
		t.Errorf("pluralY(0) = %q, want \"ies\"", got)
	}
	if got := pluralY(5); got != "ies" {
		t.Errorf("pluralY(5) = %q, want \"ies\"", got)
	}
}
