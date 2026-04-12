package main

import "testing"

// TestNotifyCommandStructure verifies both subcommands exist.
func TestNotifyCommandStructure(t *testing.T) {
	cmd := NewNotifyCommand()

	expected := []string{"test-email", "config"}
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

// TestNotifyTestEmailFlags verifies the test-email flags.
func TestNotifyTestEmailFlags(t *testing.T) {
	cmd := findSubcommand(NewNotifyCommand(), "test-email")
	if cmd == nil {
		t.Fatal("test-email subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("test-email should have RunE set")
	}

	flags := []string{"site", "to", "provider"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("test-email command missing --%s flag", name)
		}
	}
}

// TestNotifyConfigFlags verifies the config flags.
func TestNotifyConfigFlags(t *testing.T) {
	cmd := findSubcommand(NewNotifyCommand(), "config")
	if cmd == nil {
		t.Fatal("config subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("config should have RunE set")
	}

	flags := []string{"site", "set", "json"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("config command missing --%s flag", name)
		}
	}
}

// TestNotifyConfigKeyPath verifies key path mapping.
func TestNotifyConfigKeyPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"smtp.host", "notification.email.smtp.host"},
		{"smtp.port", "notification.email.smtp.port"},
		{"provider", "notification.email.provider"},
		{"ses.region", "notification.email.ses.region"},
		{"smtp.use_tls", "notification.email.smtp.use_tls"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := notifyConfigKeyPath(tt.input)
			if got != tt.want {
				t.Errorf("notifyConfigKeyPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestMaskNotifySecrets verifies secret masking.
func TestMaskNotifySecrets(t *testing.T) {
	flat := map[string]any{
		"smtp.host":     "smtp.example.com",
		"smtp.password": "s3cret",
		"smtp.port":     587,
	}

	masked := maskNotifySecrets(flat)
	if masked["smtp.host"] != "smtp.example.com" {
		t.Errorf("host should not be masked, got %v", masked["smtp.host"])
	}
	if masked["smtp.password"] != "***" {
		t.Errorf("password should be masked, got %v", masked["smtp.password"])
	}
	if masked["smtp.port"] != 587 {
		t.Errorf("port should not be masked, got %v", masked["smtp.port"])
	}
}
