package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Structural tests — no DB required
// ---------------------------------------------------------------------------

func TestAPICommandStructure(t *testing.T) {
	cmd := NewAPICommand()

	// Top-level subcommands: list, test, docs, keys, webhooks
	topLevel := cmd.Commands()
	if len(topLevel) != 5 {
		t.Errorf("api: expected 5 top-level subcommands, got %d", len(topLevel))
	}

	expectedTop := map[string]bool{
		"list": false, "test": false, "docs": false, "keys": false, "webhooks": false,
	}
	for _, sub := range topLevel {
		if _, ok := expectedTop[sub.Name()]; ok {
			expectedTop[sub.Name()] = true
		}
	}
	for name, found := range expectedTop {
		if !found {
			t.Errorf("api: missing expected subcommand %q", name)
		}
	}
}

func TestAPIKeysSubcommands(t *testing.T) {
	cmd := NewAPICommand()
	keysCmd := findSubcommand(cmd, "keys")
	if keysCmd == nil {
		t.Fatal("api: missing 'keys' subgroup")
	}

	subs := keysCmd.Commands()
	if len(subs) != 4 {
		t.Errorf("api keys: expected 4 subcommands, got %d", len(subs))
	}

	expected := map[string]bool{"create": false, "revoke": false, "list": false, "rotate": false}
	for _, sub := range subs {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("api keys: missing expected subcommand %q", name)
		}
	}
}

func TestAPIWebhooksSubcommands(t *testing.T) {
	cmd := NewAPICommand()
	webhooksCmd := findSubcommand(cmd, "webhooks")
	if webhooksCmd == nil {
		t.Fatal("api: missing 'webhooks' subgroup")
	}

	subs := webhooksCmd.Commands()
	if len(subs) != 3 {
		t.Errorf("api webhooks: expected 3 subcommands, got %d", len(subs))
	}

	expected := map[string]bool{"list": false, "test": false, "logs": false}
	for _, sub := range subs {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("api webhooks: missing expected subcommand %q", name)
		}
	}
}

func TestAPICommandFlags(t *testing.T) {
	cmd := NewAPICommand()

	tests := []struct {
		path  []string // navigation path: ["keys", "create"]
		flags []string
	}{
		{[]string{"list"}, []string{"site", "doctype", "method", "json"}},
		{[]string{"test"}, []string{"site", "method", "user", "api-key", "data", "repeat", "verbose", "json"}},
		{[]string{"keys", "create"}, []string{"site", "user", "label", "scopes", "expires", "ip-allow", "json"}},
		{[]string{"keys", "revoke"}, []string{"site", "force"}},
		{[]string{"keys", "list"}, []string{"site", "user", "status", "json"}},
		{[]string{"keys", "rotate"}, []string{"site", "grace-period", "json"}},
		{[]string{"webhooks", "list"}, []string{"site", "doctype", "json"}},
		{[]string{"webhooks", "test"}, []string{"site"}},
		{[]string{"webhooks", "logs"}, []string{"site", "status", "limit", "json"}},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			sub := navigateSubcommand(cmd, tt.path)
			if sub == nil {
				t.Fatalf("subcommand %v not found", tt.path)
			}
			for _, flag := range tt.flags {
				if sub.Flags().Lookup(flag) == nil {
					t.Errorf("flag --%s missing on %v", flag, tt.path)
				}
			}
		})
	}
}

func TestAPIDocsHasFlags(t *testing.T) {
	cmd := NewAPICommand()
	docsCmd := findSubcommand(cmd, "docs")
	if docsCmd == nil {
		t.Fatal("api docs: subcommand not found")
	}

	expectedFlags := []string{"site", "output", "format", "serve", "port"}
	for _, flag := range expectedFlags {
		if docsCmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s missing on docs command", flag)
		}
	}

	if docsCmd.RunE == nil {
		t.Error("api docs: RunE should be set (no longer placeholder)")
	}
}

func TestAPIImplementedCommandsHaveRunE(t *testing.T) {
	cmd := NewAPICommand()

	implemented := [][]string{
		{"list"}, {"test"}, {"docs"},
		{"keys", "create"}, {"keys", "revoke"}, {"keys", "list"}, {"keys", "rotate"},
		{"webhooks", "list"}, {"webhooks", "test"}, {"webhooks", "logs"},
	}

	for _, path := range implemented {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			sub := navigateSubcommand(cmd, path)
			if sub == nil {
				t.Fatalf("subcommand %v not found", path)
			}
			if sub.RunE == nil {
				t.Errorf("subcommand %v has nil RunE", path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests for pure helper functions
// ---------------------------------------------------------------------------

func TestParseExpiresDuration(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		wantErr bool
		minDur  time.Duration
		maxDur  time.Duration
	}{
		{input: "", wantNil: true},
		{input: "never", wantNil: true},
		{input: "90d", minDur: 89 * 24 * time.Hour, maxDur: 91 * 24 * time.Hour},
		{input: "1y", minDur: 364 * 24 * time.Hour, maxDur: 366 * 24 * time.Hour},
		{input: "24h", minDur: 23 * time.Hour, maxDur: 25 * time.Hour},
		{input: "7d", minDur: 6 * 24 * time.Hour, maxDur: 8 * 24 * time.Hour},
		{input: "invalid", wantErr: true},
		{input: "xd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpiresDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil time, got nil")
			}
			dur := time.Until(*got)
			if dur < tt.minDur || dur > tt.maxDur {
				t.Errorf("duration %v outside expected range [%v, %v]", dur, tt.minDur, tt.maxDur)
			}
		})
	}
}

func TestParseScopesFlag(t *testing.T) {
	tests := []struct { //nolint:govet // field order matches logical grouping
		name         string
		input        []string
		wantLen      int
		wantScope    string
		wantDocTypes []string
		wantOps      []string
	}{
		{
			name: "single scope", input: []string{"orders:read"}, wantLen: 1,
			wantScope: "orders:read", wantDocTypes: []string{"orders"}, wantOps: []string{"read"},
		},
		{
			name: "multiple scopes", input: []string{"orders:read", "users:write"}, wantLen: 2,
			wantScope: "orders:read", wantDocTypes: []string{"orders"}, wantOps: []string{"read"},
		},
		{name: "nil input", input: nil, wantLen: 0},
		{name: "empty input", input: []string{}, wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseScopesFlag(tt.input)
			if len(result) != tt.wantLen {
				t.Errorf("got %d results, want %d", len(result), tt.wantLen)
				return
			}
			if tt.wantLen > 0 {
				first := result[0]
				if first.Scope != tt.wantScope {
					t.Errorf("Scope: got %q, want %q", first.Scope, tt.wantScope)
				}
				if len(first.DocTypes) == 0 || first.DocTypes[0] != tt.wantDocTypes[0] {
					t.Errorf("DocTypes: got %v, want %v", first.DocTypes, tt.wantDocTypes)
				}
				if len(first.Operations) == 0 || first.Operations[0] != tt.wantOps[0] {
					t.Errorf("Operations: got %v, want %v", first.Operations, tt.wantOps)
				}
			}
		})
	}
}

func TestComputeTimingStats(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		stats := computeTimingStats(nil)
		if stats.Min != 0 || stats.Max != 0 || stats.Avg != 0 || stats.P95 != 0 {
			t.Errorf("expected all zeros, got %+v", stats)
		}
	})

	t.Run("single value", func(t *testing.T) {
		stats := computeTimingStats([]time.Duration{100 * time.Millisecond})
		if stats.Min != 100*time.Millisecond || stats.Max != 100*time.Millisecond {
			t.Errorf("unexpected min/max: %+v", stats)
		}
	})

	t.Run("multiple values", func(t *testing.T) {
		durations := make([]time.Duration, 10)
		for i := range durations {
			durations[i] = time.Duration(i+1) * 10 * time.Millisecond
		}
		stats := computeTimingStats(durations)
		if stats.Min != 10*time.Millisecond {
			t.Errorf("Min: got %v, want 10ms", stats.Min)
		}
		if stats.Max != 100*time.Millisecond {
			t.Errorf("Max: got %v, want 100ms", stats.Max)
		}
		if stats.Avg != 55*time.Millisecond {
			t.Errorf("Avg: got %v, want 55ms", stats.Avg)
		}
		// P95 for 10 items: ceil(10*0.95)-1 = 9 → index 9 → 100ms
		if stats.P95 != 100*time.Millisecond {
			t.Errorf("P95: got %v, want 100ms", stats.P95)
		}
	})

	t.Run("unsorted input", func(t *testing.T) {
		durations := []time.Duration{
			50 * time.Millisecond,
			10 * time.Millisecond,
			90 * time.Millisecond,
			30 * time.Millisecond,
		}
		stats := computeTimingStats(durations)
		if stats.Min != 10*time.Millisecond {
			t.Errorf("Min: got %v, want 10ms", stats.Min)
		}
		if stats.Max != 90*time.Millisecond {
			t.Errorf("Max: got %v, want 90ms", stats.Max)
		}
	})
}

func TestFormatRelativeTime(t *testing.T) {
	if got := formatRelativeTime(nil); got != "never" {
		t.Errorf("nil: got %q, want %q", got, "never")
	}

	recent := time.Now().Add(-30 * time.Second)
	got := formatRelativeTime(&recent)
	if !strings.Contains(got, "s ago") {
		t.Errorf("recent: got %q, expected seconds ago format", got)
	}

	hours := time.Now().Add(-3 * time.Hour)
	got = formatRelativeTime(&hours)
	if !strings.Contains(got, "h ago") {
		t.Errorf("hours: got %q, expected hours ago format", got)
	}
}

func TestFormatRelativeFuture(t *testing.T) {
	if got := formatRelativeFuture(nil); got != "never" {
		t.Errorf("nil: got %q, want %q", got, "never")
	}

	past := time.Now().Add(-1 * time.Hour)
	if got := formatRelativeFuture(&past); got != "expired" {
		t.Errorf("past: got %q, want %q", got, "expired")
	}
}

func TestFormatScopesList(t *testing.T) {
	if got := formatScopesList(nil); got != "-" {
		t.Errorf("nil: got %q, want %q", got, "-")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// navigateSubcommand walks a path of subcommand names from a parent.
func navigateSubcommand(parent *cobra.Command, path []string) *cobra.Command {
	current := parent
	for _, name := range path {
		found := findSubcommand(current, name)
		if found == nil {
			return nil
		}
		current = found
	}
	return current
}
