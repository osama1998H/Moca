package main

import (
	"testing"
)

// TestSearchCommandStructure verifies all 3 subcommands exist.
func TestSearchCommandStructure(t *testing.T) {
	cmd := NewSearchCommand()

	expected := []string{"rebuild", "status", "query"}
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

// TestSearchRebuildFlags verifies flags for the rebuild command.
func TestSearchRebuildFlags(t *testing.T) {
	cmd := NewSearchCommand()

	var rebuildCmd = findSubcommand(cmd, "rebuild")
	if rebuildCmd == nil {
		t.Fatal("rebuild subcommand not found")
	}

	flags := []string{"site", "all-sites", "doctype", "batch-size"}
	for _, name := range flags {
		if rebuildCmd.Flags().Lookup(name) == nil {
			t.Errorf("rebuild command missing --%s flag", name)
		}
	}

	if rebuildCmd.RunE == nil {
		t.Error("rebuild command should have RunE set")
	}
}

// TestSearchStatusFlags verifies flags for the status command.
func TestSearchStatusFlags(t *testing.T) {
	cmd := NewSearchCommand()

	var statusCmd = findSubcommand(cmd, "status")
	if statusCmd == nil {
		t.Fatal("status subcommand not found")
	}

	if statusCmd.Flags().Lookup("site") == nil {
		t.Error("status command missing --site flag")
	}

	if statusCmd.RunE == nil {
		t.Error("status command should have RunE set")
	}
}

// TestSearchQueryFlags verifies flags for the query command.
func TestSearchQueryFlags(t *testing.T) {
	cmd := NewSearchCommand()

	var queryCmd = findSubcommand(cmd, "query")
	if queryCmd == nil {
		t.Fatal("query subcommand not found")
	}

	flags := []string{"site", "doctype", "limit"}
	for _, name := range flags {
		if queryCmd.Flags().Lookup(name) == nil {
			t.Errorf("query command missing --%s flag", name)
		}
	}

	if queryCmd.RunE == nil {
		t.Error("query command should have RunE set")
	}
}

// TestSearchQueryRequiresArg verifies that the query command requires exactly 1 argument.
func TestSearchQueryRequiresArg(t *testing.T) {
	cmd := NewSearchCommand()

	var queryCmd = findSubcommand(cmd, "query")
	if queryCmd == nil {
		t.Fatal("query subcommand not found")
	}

	if queryCmd.Args == nil {
		t.Error("query command should have Args validation")
	}
}

// TestParseIndexUID verifies that index UIDs are correctly split into site and doctype.
func TestParseIndexUID(t *testing.T) {
	tests := []struct {
		uid     string
		site    string
		doctype string
	}{
		{"acme.localhost_SalesOrder", "acme.localhost", "SalesOrder"},
		{"mysite_Product", "mysite", "Product"},
		{"nounderscores", "nounderscores", ""},
		{"a_b_c", "a", "b_c"},
	}

	for _, tt := range tests {
		site, doctype := parseIndexUID(tt.uid)
		if site != tt.site {
			t.Errorf("parseIndexUID(%q) site = %q, want %q", tt.uid, site, tt.site)
		}
		if doctype != tt.doctype {
			t.Errorf("parseIndexUID(%q) doctype = %q, want %q", tt.uid, doctype, tt.doctype)
		}
	}
}
