package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/output"
)

func TestBuildInitConfig_UsesValidMocaConstraint(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	cmd.Flags().String("db-host", "localhost", "")
	cmd.Flags().Int("db-port", 5432, "")
	cmd.Flags().String("db-user", "postgres", "")
	cmd.Flags().String("db-password", "", "")
	cmd.Flags().String("redis-host", "localhost", "")
	cmd.Flags().Int("redis-port", 6379, "")
	cmd.Flags().Bool("no-kafka", false, "")

	cfg := buildInitConfig(cmd, "demo")

	if cfg.Moca != defaultMocaConstraint {
		t.Fatalf("Moca = %q, want %q", cfg.Moca, defaultMocaConstraint)
	}

	if errs := config.Validate(cfg); len(errs) > 0 {
		t.Fatalf("Validate(buildInitConfig) returned errors: %v", errs)
	}
}

func TestInitCommandCreatesBootstrapArtifacts(t *testing.T) {
	origPostgres := runInitPostgres
	origRedis := runInitRedis
	origRegisterCoreApp := runInitRegisterCoreApp
	origCopyCoreApp := runInitCopyCoreAppFiles
	origWriteGoWork := runInitWriteGoWork
	origGit := runInitGit
	t.Cleanup(func() {
		runInitPostgres = origPostgres
		runInitRedis = origRedis
		runInitRegisterCoreApp = origRegisterCoreApp
		runInitCopyCoreAppFiles = origCopyCoreApp
		runInitWriteGoWork = origWriteGoWork
		runInitGit = origGit
	})

	runInitPostgres = func(context.Context, *config.ProjectConfig) error { return nil }
	runInitRedis = func(context.Context, *config.ProjectConfig, *output.Writer) error { return nil }
	runInitRegisterCoreApp = func(context.Context, *config.ProjectConfig) error { return nil }
	runInitGit = func(string) error { return nil }

	coreSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(coreSource, "manifest.yaml"), []byte("name: core\n"), 0o644); err != nil {
		t.Fatalf("write core manifest: %v", err)
	}
	runInitCopyCoreAppFiles = func(targetDir string) error {
		return copyDir(coreSource, filepath.Join(targetDir, "apps", "core"))
	}
	runInitWriteGoWork = writeGoWork

	targetDir := filepath.Join(t.TempDir(), "project")
	_, _, err := executeRootCommand(t, nil, NewInitCommand(),
		"init", targetDir,
		"--db-host", "localhost",
		"--db-port", "5433",
		"--db-user", "moca",
		"--db-password", "moca_test",
		"--redis-host", "localhost",
		"--redis-port", "6380",
	)
	if err != nil {
		t.Fatalf("init command: %v", err)
	}

	for _, path := range []string{
		filepath.Join(targetDir, "moca.yaml"),
		filepath.Join(targetDir, "moca.lock"),
		filepath.Join(targetDir, "go.work"),
		filepath.Join(targetDir, ".moca"),
		filepath.Join(targetDir, "sites"),
		filepath.Join(targetDir, "apps", "core"),
		filepath.Join(targetDir, "apps", "core", "manifest.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	goWorkData, err := os.ReadFile(filepath.Join(targetDir, "go.work"))
	if err != nil {
		t.Fatalf("read go.work: %v", err)
	}
	if got := string(goWorkData); got != "go 1.26.1\n\nuse (\n\t./apps/core\n)\n" {
		t.Fatalf("unexpected go.work contents:\n%s", got)
	}
}
