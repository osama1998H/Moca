package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/pkg/cli"
	"github.com/spf13/cobra"
)

func TestStatusCommandNoActiveSite(t *testing.T) {
	projectRoot := t.TempDir()
	stdout, _, err := executeRootCommand(t, testCLIContext(projectRoot, ""), NewStatusCommand(), "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "Active site: none") {
		t.Fatalf("expected explicit no active site, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Server: stopped") {
		t.Fatalf("expected stopped server status, got:\n%s", stdout)
	}
}

func TestStatusCommandJSON(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir .moca: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".moca", "worker.pid"), []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write worker pid: %v", err)
	}

	stdout, _, err := executeRootCommand(t, testCLIContext(projectRoot, "acme.localhost"), NewStatusCommand(), "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}

	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("unmarshal status json: %v", err)
	}
	if report.ActiveSite != "acme.localhost" {
		t.Fatalf("active_site = %q, want %q", report.ActiveSite, "acme.localhost")
	}
	if report.Worker.State == "" {
		t.Fatal("expected worker state in json output")
	}
}

func TestStatusCommandReadsCurrentSiteFromStateFile(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "moca.yaml"), []byte(`project:
  name: test-erp
  version: "0.1.0"

moca: ">=0.1.0"

apps:
  core:
    source: builtin

infrastructure:
  database:
    host: localhost
    port: 5433
  redis:
    host: localhost
    port: 6380
`), 0o644); err != nil {
		t.Fatalf("write moca.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir .moca: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".moca", "current_site"), []byte("file-site.localhost\n"), 0o644); err != nil {
		t.Fatalf("write current_site: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(projectRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(NewStatusCommand())
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cc, err := clicontext.Resolve(cmd)
		if err != nil {
			return err
		}
		cmd.SetContext(clicontext.WithCLIContext(cmd.Context(), cc))
		return nil
	}

	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"status"})

	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout.String(), "Active site: file-site.localhost") {
		t.Fatalf("expected state-file site in output, got:\n%s", stdout.String())
	}
}
