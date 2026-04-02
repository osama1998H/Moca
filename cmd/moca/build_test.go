package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildCommandSubcommands(t *testing.T) {
	cmd := NewBuildCommand()

	expected := []string{"desk", "portal", "assets", "app", "server"}
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

	// Verify app and server have real implementations (non-nil RunE).
	for _, sub := range subs {
		if sub.Name() == "app" || sub.Name() == "server" {
			if sub.RunE == nil {
				t.Errorf("subcommand %q has nil RunE (still a placeholder?)", sub.Name())
			}
		}
	}
}

func TestBuildAppFlags(t *testing.T) {
	cmd := NewBuildCommand()

	var appCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "app" {
			appCmd = sub
			break
		}
	}
	if appCmd == nil {
		t.Fatal("app subcommand not found")
	}

	// Verify flags.
	for _, flag := range []string{"race", "verbose"} {
		if appCmd.Flags().Lookup(flag) == nil {
			t.Errorf("app subcommand missing flag --%s", flag)
		}
	}

	// Verify requires exactly 1 arg.
	if appCmd.Args == nil {
		t.Error("app subcommand has no Args validator")
	}
}

func TestBuildServerFlags(t *testing.T) {
	cmd := NewBuildCommand()

	var serverCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "server" {
			serverCmd = sub
			break
		}
	}
	if serverCmd == nil {
		t.Fatal("server subcommand not found")
	}

	// Verify flags.
	for _, flag := range []string{"output", "race", "verbose", "ldflags"} {
		if serverCmd.Flags().Lookup(flag) == nil {
			t.Errorf("server subcommand missing flag --%s", flag)
		}
	}
}

func TestEnsureGoWork(t *testing.T) {
	tests := []struct {
		name        string
		projectRoot string
		wantGoWork  string
		env         []string
	}{
		{
			name:        "appends when not present",
			env:         []string{"PATH=/usr/bin", "HOME=/home/test"},
			projectRoot: "/projects/myapp",
			wantGoWork:  "GOWORK=/projects/myapp/go.work",
		},
		{
			name:        "replaces existing",
			env:         []string{"PATH=/usr/bin", "GOWORK=/old/go.work", "HOME=/home/test"},
			projectRoot: "/projects/myapp",
			wantGoWork:  "GOWORK=/projects/myapp/go.work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensureGoWork(tt.env, tt.projectRoot)

			found := false
			for _, e := range result {
				if e == tt.wantGoWork {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in env, got %v", tt.wantGoWork, result)
			}

			// Verify no duplicate GOWORK entries.
			count := 0
			for _, e := range result {
				if len(e) > 7 && e[:7] == "GOWORK=" {
					count++
				}
			}
			if count != 1 {
				t.Errorf("expected exactly 1 GOWORK entry, got %d", count)
			}
		})
	}
}

func TestJoinArgs(t *testing.T) {
	tests := []struct {
		want string
		args []string
	}{
		{"build", []string{"build"}},
		{"build -race ./...", []string{"build", "-race", "./..."}},
		{"", []string{}},
	}

	for _, tt := range tests {
		got := joinArgs(tt.args)
		if got != tt.want {
			t.Errorf("joinArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}
