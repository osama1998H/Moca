package clicontext

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// --- helpers ---

// minimalMocaYAML is a valid moca.yaml that passes config.LoadAndResolve.
const minimalMocaYAML = `project:
  name: test-project
  version: "0.1.0"

moca: "^1.0.0"

apps:
  core:
    source: builtin

infrastructure:
  database:
    host: localhost
    port: 5432
  redis:
    host: localhost
    port: 6379
`

// newTestRootCmd creates a minimal root command with the same persistent flags
// as the real MOCA root command. This avoids importing pkg/cli in tests.
func newTestRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "moca",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	pf := cmd.PersistentFlags()
	pf.String("site", "", "Target site")
	pf.String("env", "", "Target environment")
	pf.String("project", "", "Project root directory")
	return cmd
}

// setupProject creates a temp dir with a valid moca.yaml and returns its
// symlink-resolved absolute path (avoids macOS /var → /private/var mismatch).
func setupProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte(minimalMocaYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// evalTempDir returns a symlink-resolved t.TempDir().
func evalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// setupStateFile creates .moca/<name> with the given content under dir.
func setupStateFile(t *testing.T, dir, name, content string) {
	t.Helper()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- Project resolver tests ---

func TestResolveProject_WalkUp(t *testing.T) {
	dir := setupProject(t)

	// Create a subdirectory and chdir into it.
	sub := filepath.Join(dir, "apps", "myapp")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	root, cfg, err := resolveProject(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Errorf("project root = %q, want %q", root, dir)
	}
	if cfg == nil {
		t.Fatal("expected non-nil ProjectConfig")
	}
	if cfg.Project.Name != "test-project" {
		t.Errorf("project name = %q, want %q", cfg.Project.Name, "test-project")
	}
}

func TestResolveProject_NoProject(t *testing.T) {
	dir := evalTempDir(t) // empty directory, no moca.yaml

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	root, cfg, err := resolveProject(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != "" {
		t.Errorf("project root = %q, want empty", root)
	}
	if cfg != nil {
		t.Errorf("expected nil ProjectConfig, got %+v", cfg)
	}
}

func TestResolveProject_FlagOverride(t *testing.T) {
	dir := setupProject(t)

	cmd := newTestRootCmd()
	// Simulate --project flag.
	if err := cmd.PersistentFlags().Set("project", dir); err != nil {
		t.Fatal(err)
	}

	root, cfg, err := resolveProject(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Errorf("project root = %q, want %q", root, dir)
	}
	if cfg == nil {
		t.Fatal("expected non-nil ProjectConfig")
	}
}

func TestResolveProject_EnvOverride(t *testing.T) {
	dir := setupProject(t)
	t.Setenv("MOCA_PROJECT", dir)

	// Chdir somewhere else entirely.
	origDir, _ := os.Getwd()
	if err := os.Chdir(evalTempDir(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	root, cfg, err := resolveProject(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != dir {
		t.Errorf("project root = %q, want %q", root, dir)
	}
	if cfg == nil {
		t.Fatal("expected non-nil ProjectConfig")
	}
}

func TestResolveProject_InvalidConfig(t *testing.T) {
	dir := evalTempDir(t)
	// Write an invalid moca.yaml (missing required fields).
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("invalid: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	_, _, err := resolveProject(cmd)
	if err == nil {
		t.Fatal("expected error for invalid moca.yaml, got nil")
	}
}

// --- Site resolver tests ---

func TestResolveSite_FlagPriority(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "current_site", "file-site.localhost")
	t.Setenv("MOCA_SITE", "env-site.localhost")

	cmd := newTestRootCmd()
	if err := cmd.PersistentFlags().Set("site", "flag-site.localhost"); err != nil {
		t.Fatal(err)
	}

	got := resolveSite(cmd, dir)
	if got != "flag-site.localhost" {
		t.Errorf("site = %q, want %q", got, "flag-site.localhost")
	}
}

func TestResolveSite_EnvPriority(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "current_site", "file-site.localhost")
	t.Setenv("MOCA_SITE", "env-site.localhost")

	cmd := newTestRootCmd()
	got := resolveSite(cmd, dir)
	if got != "env-site.localhost" {
		t.Errorf("site = %q, want %q", got, "env-site.localhost")
	}
}

func TestResolveSite_StateFile(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "current_site", "file-site.localhost\n")

	cmd := newTestRootCmd()
	got := resolveSite(cmd, dir)
	if got != "file-site.localhost" {
		t.Errorf("site = %q, want %q (should trim whitespace)", got, "file-site.localhost")
	}
}

func TestResolveSite_NoSite(t *testing.T) {
	cmd := newTestRootCmd()
	got := resolveSite(cmd, "")
	if got != "" {
		t.Errorf("site = %q, want empty", got)
	}
}

// --- Environment resolver tests ---

func TestResolveEnvironment_FlagPriority(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "environment", "staging")
	t.Setenv("MOCA_ENV", "production")

	cmd := newTestRootCmd()
	if err := cmd.PersistentFlags().Set("env", "testing"); err != nil {
		t.Fatal(err)
	}

	got := resolveEnvironment(cmd, dir)
	if got != "testing" {
		t.Errorf("env = %q, want %q", got, "testing")
	}
}

func TestResolveEnvironment_EnvPriority(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "environment", "staging")
	t.Setenv("MOCA_ENV", "production")

	cmd := newTestRootCmd()
	got := resolveEnvironment(cmd, dir)
	if got != "production" {
		t.Errorf("env = %q, want %q", got, "production")
	}
}

func TestResolveEnvironment_StateFile(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "environment", "staging\n")

	cmd := newTestRootCmd()
	got := resolveEnvironment(cmd, dir)
	if got != "staging" {
		t.Errorf("env = %q, want %q", got, "staging")
	}
}

func TestResolveEnvironment_Default(t *testing.T) {
	cmd := newTestRootCmd()
	got := resolveEnvironment(cmd, "")
	if got != "development" {
		t.Errorf("env = %q, want %q", got, "development")
	}
}

// --- Full Resolve + context round-trip tests ---

func TestResolve_FullPipeline(t *testing.T) {
	dir := setupProject(t)
	setupStateFile(t, dir, "current_site", "mysite.localhost")
	setupStateFile(t, dir, "environment", "production")

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	cc, err := Resolve(cmd)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cc.ProjectRoot != dir {
		t.Errorf("ProjectRoot = %q, want %q", cc.ProjectRoot, dir)
	}
	if cc.Project == nil {
		t.Fatal("expected non-nil Project")
	}
	if cc.Site != "mysite.localhost" {
		t.Errorf("Site = %q, want %q", cc.Site, "mysite.localhost")
	}
	if cc.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cc.Environment, "production")
	}
}

func TestResolve_OutsideProject(t *testing.T) {
	dir := evalTempDir(t)

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	cmd := newTestRootCmd()
	cc, err := Resolve(cmd)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if cc.ProjectRoot != "" {
		t.Errorf("ProjectRoot = %q, want empty", cc.ProjectRoot)
	}
	if cc.Project != nil {
		t.Errorf("expected nil Project")
	}
	if cc.Environment != "development" {
		t.Errorf("Environment = %q, want %q", cc.Environment, "development")
	}
}

func TestWithCLIContext_FromContext_RoundTrip(t *testing.T) {
	cc := &CLIContext{
		ProjectRoot: "/some/path",
		Site:        "example.localhost",
		Environment: "production",
	}

	ctx := WithCLIContext(context.Background(), cc)
	got := FromContext(ctx)
	if got != cc {
		t.Errorf("FromContext returned %p, want %p", got, cc)
	}
}

func TestFromContext_Missing(t *testing.T) {
	got := FromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil CLIContext from empty context, got %+v", got)
	}
}

func TestFromCommand_RoundTrip(t *testing.T) {
	cc := &CLIContext{
		ProjectRoot: "/test",
		Site:        "site.localhost",
		Environment: "staging",
	}

	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(WithCLIContext(context.Background(), cc))

	got := FromCommand(cmd)
	if got != cc {
		t.Errorf("FromCommand returned %p, want %p", got, cc)
	}
}
