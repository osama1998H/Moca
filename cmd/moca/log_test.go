package main

import (
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// TestLogCommandStructure verifies all 3 subcommands exist.
func TestLogCommandStructure(t *testing.T) {
	cmd := NewLogCommand()

	expected := []string{"tail", "search", "export"}
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

// TestLogTailHasRunE verifies tail has a RunE handler.
func TestLogTailHasRunE(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "tail")
	if cmd == nil {
		t.Fatal("tail subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("tail should have RunE set")
	}
}

// TestLogTailFlags verifies tail has all required flags.
func TestLogTailFlags(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "tail")
	if cmd == nil {
		t.Fatal("tail subcommand not found")
	}

	flags := []string{"process", "level", "site", "request-id", "no-color", "follow"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("tail command missing --%s flag", name)
		}
	}
}

// TestLogSearchHasRunE verifies search has a RunE handler.
func TestLogSearchHasRunE(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "search")
	if cmd == nil {
		t.Fatal("search subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("search should have RunE set")
	}
}

// TestLogSearchFlags verifies search has all required flags.
func TestLogSearchFlags(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "search")
	if cmd == nil {
		t.Fatal("search subcommand not found")
	}

	flags := []string{"process", "level", "since", "site", "request-id", "limit"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("search command missing --%s flag", name)
		}
	}
}

// TestLogSearchArgs verifies search accepts optional QUERY argument.
func TestLogSearchArgs(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "search")
	if cmd == nil {
		t.Fatal("search subcommand not found")
	}
	if cmd.Args == nil {
		t.Error("search should have Args validation set")
	}
}

// TestLogExportHasRunE verifies export has a RunE handler.
func TestLogExportHasRunE(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "export")
	if cmd == nil {
		t.Fatal("export subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("export should have RunE set")
	}
}

// TestLogExportFlags verifies export has all required flags.
func TestLogExportFlags(t *testing.T) {
	cmd := findSubcommand(NewLogCommand(), "export")
	if cmd == nil {
		t.Fatal("export subcommand not found")
	}

	flags := []string{"since", "until", "process", "site", "format", "output", "compress"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("export command missing --%s flag", name)
		}
	}
}

// TestParseLogLine verifies JSON log line parsing.
func TestParseLogLine(t *testing.T) {
	tests := []struct {
		check   func(t *testing.T, e *logEntry)
		name    string
		line    string
		process string
		wantErr bool
	}{
		{
			name:    "valid slog JSON line",
			line:    `{"time":"2024-06-15T10:30:00Z","level":"INFO","msg":"request handled","site":"acme.localhost","request_id":"req-123"}`,
			process: "server",
			check: func(t *testing.T, e *logEntry) {
				if e.Level != "INFO" {
					t.Errorf("level = %q, want INFO", e.Level)
				}
				if e.Msg != "request handled" {
					t.Errorf("msg = %q, want 'request handled'", e.Msg)
				}
				if e.Site != "acme.localhost" {
					t.Errorf("site = %q, want 'acme.localhost'", e.Site)
				}
				if e.RequestID != "req-123" {
					t.Errorf("request_id = %q, want 'req-123'", e.RequestID)
				}
				if e.Process != "server" {
					t.Errorf("process = %q, want 'server'", e.Process)
				}
			},
		},
		{
			name:    "extra fields preserved",
			line:    `{"time":"2024-06-15T10:30:00Z","level":"WARN","msg":"slow query","duration_ms":245}`,
			process: "worker",
			check: func(t *testing.T, e *logEntry) {
				if _, ok := e.Fields["duration_ms"]; !ok {
					t.Error("expected duration_ms in Fields")
				}
			},
		},
		{
			name:    "empty line",
			line:    "",
			process: "server",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			line:    "not json at all",
			process: "server",
			wantErr: true,
		},
		{
			name:    "minimal valid JSON",
			line:    `{"msg":"hello"}`,
			process: "scheduler",
			check: func(t *testing.T, e *logEntry) {
				if e.Msg != "hello" {
					t.Errorf("msg = %q, want 'hello'", e.Msg)
				}
				if e.Process != "scheduler" {
					t.Errorf("process = %q, want 'scheduler'", e.Process)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := parseLogLine(tt.line, tt.process)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, e)
			}
		})
	}
}

// TestLevelAtLeast verifies log level comparison.
func TestLevelAtLeast(t *testing.T) {
	tests := []struct {
		entry   string
		minimum string
		want    bool
	}{
		{"ERROR", "error", true},
		{"WARN", "error", false},
		{"INFO", "info", true},
		{"DEBUG", "info", false},
		{"ERROR", "debug", true},
		{"DEBUG", "debug", true},
		{"WARN", "warn", true},
		{"INFO", "warn", false},
	}

	for _, tt := range tests {
		t.Run(tt.entry+">="+tt.minimum, func(t *testing.T) {
			if got := levelAtLeast(tt.entry, tt.minimum); got != tt.want {
				t.Errorf("levelAtLeast(%q, %q) = %v, want %v", tt.entry, tt.minimum, got, tt.want)
			}
		})
	}
}

// TestLogFilterMatches verifies the filter matching logic.
func TestLogFilterMatches(t *testing.T) {
	base := &logEntry{
		Time:      time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
		Level:     "WARN",
		Msg:       "slow database query detected",
		Site:      "acme.localhost",
		RequestID: "req-abc",
		Process:   "server",
	}

	tests := []struct { //nolint:govet // test table readability
		name   string
		filter logFilter
		want   bool
	}{
		{"no filter matches all", logFilter{}, true},
		{"level match", logFilter{Level: "warn"}, true},
		{"level too high", logFilter{Level: "error"}, false},
		{"site match", logFilter{Site: "acme.localhost"}, true},
		{"site mismatch", logFilter{Site: "other.localhost"}, false},
		{"request-id match", logFilter{RequestID: "req-abc"}, true},
		{"request-id mismatch", logFilter{RequestID: "req-xyz"}, false},
		{"process match", logFilter{Process: "server"}, true},
		{"process mismatch", logFilter{Process: "worker"}, false},
		{"process all matches", logFilter{Process: "all"}, true},
		{"query match", logFilter{Query: "slow"}, true},
		{"query case insensitive", logFilter{Query: "SLOW"}, true},
		{"query mismatch", logFilter{Query: "fast"}, false},
		{"since before entry", logFilter{Since: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)}, true},
		{"since after entry", logFilter{Since: time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)}, false},
		{"until after entry", logFilter{Until: time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)}, true},
		{"until before entry", logFilter{Until: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.matches(base); got != tt.want {
				t.Errorf("filter.matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFormatLogEntry verifies formatted output.
func TestFormatLogEntry(t *testing.T) {
	e := &logEntry{
		Time:    time.Date(2024, 6, 15, 15, 4, 12, 0, time.UTC),
		Level:   "WARN",
		Msg:     "Slow query detected",
		Site:    "acme",
		Process: "server",
	}

	// With no-color, output should not contain ANSI escape codes.
	plain := formatLogEntry(e, true)
	if len(plain) == 0 {
		t.Fatal("expected non-empty formatted output")
	}
	if containsANSI(plain) {
		t.Error("no-color output should not contain ANSI codes")
	}
	// Should contain the key parts.
	for _, part := range []string{"15:04:12", "WARN", "[server]", "acme", "Slow query detected"} {
		if !containsStr(plain, part) {
			t.Errorf("plain output missing %q: %s", part, plain)
		}
	}

	// With color, output should contain ANSI escape codes.
	colored := formatLogEntry(e, false)
	if !containsANSI(colored) {
		t.Error("color output should contain ANSI codes")
	}
}

// TestFormatLogEntryNoSite verifies output when site is empty.
func TestFormatLogEntryNoSite(t *testing.T) {
	e := &logEntry{
		Time:    time.Date(2024, 6, 15, 15, 4, 12, 0, time.UTC),
		Level:   "INFO",
		Msg:     "Server starting",
		Process: "server",
	}

	plain := formatLogEntry(e, true)
	if !containsStr(plain, "Server starting") {
		t.Errorf("output missing message: %s", plain)
	}
	if !containsStr(plain, "[server]") {
		t.Errorf("output missing process: %s", plain)
	}
}

// TestResolveLogDir verifies log directory resolution.
func TestResolveLogDir(t *testing.T) {
	// Default: no LogDir set.
	cfg := &config.ProjectConfig{}
	dir := resolveLogDir(cfg, "/projects/myapp")
	if dir != "/projects/myapp/logs" {
		t.Errorf("default dir = %q, want /projects/myapp/logs", dir)
	}

	// Override with relative path.
	cfg.Development.LogDir = "custom-logs"
	dir = resolveLogDir(cfg, "/projects/myapp")
	if dir != "/projects/myapp/custom-logs" {
		t.Errorf("relative override dir = %q, want /projects/myapp/custom-logs", dir)
	}

	// Override with absolute path.
	cfg.Development.LogDir = "/var/log/moca"
	dir = resolveLogDir(cfg, "/projects/myapp")
	if dir != "/var/log/moca" {
		t.Errorf("absolute override dir = %q, want /var/log/moca", dir)
	}

	// Nil config.
	dir = resolveLogDir(nil, "/projects/myapp")
	if dir != "/projects/myapp/logs" {
		t.Errorf("nil config dir = %q, want /projects/myapp/logs", dir)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findStr(s, substr))
}

func findStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func containsANSI(s string) bool {
	return findStr(s, "\033[")
}
