package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCommandShort(t *testing.T) {
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })
	Version = "1.2.3"

	stdout, _, err := executeRootCommand(t, nil, NewVersionCommand(), "version", "--short")
	if err != nil {
		t.Fatalf("version --short: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "v1.2.3" {
		t.Fatalf("version --short = %q, want %q", got, "v1.2.3")
	}
}

func TestVersionCommandJSON(t *testing.T) {
	origVersion, origCommit, origBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version = origVersion
		Commit = origCommit
		BuildDate = origBuildDate
	})
	Version = "1.2.3"
	Commit = "abc123"
	BuildDate = "2026-04-06T00:00:00Z"

	stdout, _, err := executeRootCommand(t, nil, NewVersionCommand(), "version", "--json")
	if err != nil {
		t.Fatalf("version --json: %v", err)
	}

	var info VersionInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("unmarshal version json: %v", err)
	}
	if info.Version != "1.2.3" {
		t.Fatalf("version = %q, want %q", info.Version, "1.2.3")
	}
	if info.Commit != "abc123" {
		t.Fatalf("commit = %q, want %q", info.Commit, "abc123")
	}
}
