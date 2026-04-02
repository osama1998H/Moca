package main

import (
	"testing"

	clicontext "github.com/osama1998H/moca/internal/context"

	"github.com/osama1998H/moca/internal/config"
	"github.com/spf13/cobra"
)

func TestDevCommandSubcommands(t *testing.T) {
	cmd := NewDevCommand()

	expected := []string{"console", "shell", "execute", "request", "bench", "profile", "watch", "playground"}
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
}

func TestDevExecuteNotPlaceholder(t *testing.T) {
	cmd := NewDevCommand()

	for _, sub := range cmd.Commands() {
		if sub.Name() == "execute" {
			if sub.RunE == nil {
				t.Error("execute subcommand has nil RunE (still a placeholder?)")
			}
			return
		}
	}
	t.Error("execute subcommand not found")
}

func TestDevRequestNotPlaceholder(t *testing.T) {
	cmd := NewDevCommand()

	for _, sub := range cmd.Commands() {
		if sub.Name() == "request" {
			if sub.RunE == nil {
				t.Error("request subcommand has nil RunE (still a placeholder?)")
			}
			return
		}
	}
	t.Error("request subcommand not found")
}

func TestDevExecuteFlags(t *testing.T) {
	cmd := NewDevCommand()

	var execCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "execute" {
			execCmd = sub
			break
		}
	}
	if execCmd == nil {
		t.Fatal("execute subcommand not found")
	}

	if execCmd.Flags().Lookup("site") == nil {
		t.Error("execute subcommand missing --site flag")
	}

	// Verify requires exactly 1 arg.
	if execCmd.Args == nil {
		t.Error("execute subcommand has no Args validator")
	}
}

func TestDevRequestFlags(t *testing.T) {
	cmd := NewDevCommand()

	var reqCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "request" {
			reqCmd = sub
			break
		}
	}
	if reqCmd == nil {
		t.Fatal("request subcommand not found")
	}

	for _, flag := range []string{"site", "user", "data", "headers", "verbose", "json"} {
		if reqCmd.Flags().Lookup(flag) == nil {
			t.Errorf("request subcommand missing flag --%s", flag)
		}
	}

	// Verify default user.
	userFlag := reqCmd.Flags().Lookup("user")
	if userFlag.DefValue != "Administrator" {
		t.Errorf("--user default = %q, want %q", userFlag.DefValue, "Administrator")
	}

	// Verify requires exactly 2 args.
	if reqCmd.Args == nil {
		t.Error("request subcommand has no Args validator")
	}
}

func TestResolveRequestURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
		port   int
	}{
		{
			name:   "absolute URL unchanged",
			rawURL: "http://example.com/api/v1/resource/User",
			port:   0,
			want:   "http://example.com/api/v1/resource/User",
		},
		{
			name:   "relative URL with default port",
			rawURL: "/api/v1/resource/User",
			port:   0,
			want:   "http://localhost:8000/api/v1/resource/User",
		},
		{
			name:   "relative URL with custom port",
			rawURL: "/api/v1/resource/SalesOrder",
			port:   9090,
			want:   "http://localhost:9090/api/v1/resource/SalesOrder",
		},
		{
			name:   "relative URL root",
			rawURL: "/",
			port:   0,
			want:   "http://localhost:8000/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &clicontext.CLIContext{
				Project: &config.ProjectConfig{
					Development: config.DevelopmentConfig{
						Port: tt.port,
					},
				},
			}
			got := resolveRequestURL(tt.rawURL, ctx)
			if got != tt.want {
				t.Errorf("resolveRequestURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestGenerateExecMain(t *testing.T) {
	result := generateExecMain(`fmt.Println("hello")`)

	// Verify it contains expected elements.
	if !containsSubstring(result, "package main") {
		t.Error("generated code missing 'package main'")
	}
	if !containsSubstring(result, `"context"`) {
		t.Error("generated code missing context import")
	}
	if !containsSubstring(result, `"fmt"`) {
		t.Error("generated code missing fmt import")
	}
	if !containsSubstring(result, "func main()") {
		t.Error("generated code missing func main()")
	}
	if !containsSubstring(result, `fmt.Println("hello")`) {
		t.Error("generated code missing user expression")
	}
	if !containsSubstring(result, "ctx := context.Background()") {
		t.Error("generated code missing ctx variable")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && findSubstring(s, sub)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSortedHeaderKeys(t *testing.T) {
	h := make(map[string][]string)
	h["Z-Header"] = []string{"z"}
	h["A-Header"] = []string{"a"}
	h["M-Header"] = []string{"m"}

	keys := sortedHeaderKeys(h)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "A-Header" || keys[1] != "M-Header" || keys[2] != "Z-Header" {
		t.Errorf("keys not sorted: %v", keys)
	}
}
