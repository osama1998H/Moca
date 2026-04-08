package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
)

func TestResolveAppNewFrameworkDependency_FrameworkRepo(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot, frameworkModulePath)

	version, replacePath, err := resolveAppNewFrameworkDependency(projectRoot, "dev")
	if err != nil {
		t.Fatalf("resolveAppNewFrameworkDependency: %v", err)
	}
	if version != localFrameworkVersion {
		t.Fatalf("version = %q, want %q", version, localFrameworkVersion)
	}
	if replacePath != localFrameworkReplacePath {
		t.Fatalf("replacePath = %q, want %q", replacePath, localFrameworkReplacePath)
	}
}

func TestResolveAppNewFrameworkDependency_StandaloneRelease(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot, "example.com/project")

	version, replacePath, err := resolveAppNewFrameworkDependency(projectRoot, "v0.1.1-alpha.7")
	if err != nil {
		t.Fatalf("resolveAppNewFrameworkDependency: %v", err)
	}
	if version != "v0.1.1-alpha.7" {
		t.Fatalf("version = %q, want %q", version, "v0.1.1-alpha.7")
	}
	if replacePath != "" {
		t.Fatalf("replacePath = %q, want empty", replacePath)
	}
}

func TestResolveAppNewFrameworkDependency_StandaloneDevVersion(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot, "example.com/project")

	_, _, err := resolveAppNewFrameworkDependency(projectRoot, "dev")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if cliErr.Message != "Standalone app scaffolding requires a released moca binary" {
		t.Fatalf("cliErr.Message = %q", cliErr.Message)
	}
}

func TestRunAppNew_StandaloneDevVersionFailsBeforeScaffold(t *testing.T) {
	projectRoot := t.TempDir()
	writeTestGoMod(t, projectRoot, "example.com/project")

	oldVersion := Version
	Version = "dev"
	defer func() {
		Version = oldVersion
	}()

	cmd := newAppNewCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(clicontext.WithCLIContext(context.Background(), &clicontext.CLIContext{
		ProjectRoot: projectRoot,
		Project:     &config.ProjectConfig{},
	}))

	err := runAppNew(cmd, []string{"my_app"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *output.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if cliErr.Message != "Standalone app scaffolding requires a released moca binary" {
		t.Fatalf("cliErr.Message = %q", cliErr.Message)
	}

	if _, statErr := os.Stat(filepath.Join(projectRoot, "apps", "my_app")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no scaffolded app directory, stat err = %v", statErr)
	}
}

func writeTestGoMod(t *testing.T, dir, modulePath string) {
	t.Helper()

	content := "module " + modulePath + "\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}
