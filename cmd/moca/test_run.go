package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/spf13/cobra"
)

func newTestRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run tests (Go tests + framework tests)",
		Long: `Run Go tests for apps installed in the project.

Creates an ephemeral test site, runs tests, and cleans up automatically.
Tests should import pkg/testutils for test environment setup.`,
		RunE: runTestRun,
	}

	cmd.Flags().String("site", "", "Test site name (auto-created if not specified)")
	cmd.Flags().String("app", "", "Test a specific app only")
	cmd.Flags().String("module", "", "Test a specific module")
	cmd.Flags().String("doctype", "", "Test a specific DocType's tests")
	cmd.Flags().Int("parallel", 0, "Parallel test execution (default: GOMAXPROCS)")
	cmd.Flags().Bool("verbose", false, "Verbose test output")
	cmd.Flags().Bool("coverage", false, "Generate coverage report alongside")
	cmd.Flags().Bool("failfast", false, "Stop on first failure")
	cmd.Flags().String("filter", "", "Run tests matching pattern")
	cmd.Flags().Bool("keep-site", false, "Don't cleanup test site after run")

	return cmd
}

func runTestRun(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	w := output.NewWriter(cmd)
	projectRoot := cliCtx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")
	keepSite, _ := cmd.Flags().GetBool("keep-site")

	// Resolve or create ephemeral test site name.
	siteName, _ := cmd.Flags().GetString("site")
	if siteName == "" {
		siteName = fmt.Sprintf("testsite_%d", time.Now().Unix())
	}

	w.Print("Test site: %s", siteName)

	// Create ephemeral test site.
	ctx := cmd.Context()
	svc, err := newServices(ctx, cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	w.Print("Creating test site...")
	siteConfig := tenancy.SiteCreateConfig{
		Name:          siteName,
		AdminEmail:    "test@moca.dev",
		AdminPassword: "TestPass123!",
	}
	if createErr := svc.Sites.CreateSite(ctx, siteConfig); createErr != nil {
		return output.NewCLIError("Failed to create test site").
			WithErr(createErr).
			WithFix("Ensure PostgreSQL is running and accessible.")
	}

	if !keepSite {
		defer func() {
			w.Print("Cleaning up test site %q...", siteName)
			if dropErr := svc.Sites.DropSite(ctx, siteName, tenancy.SiteDropOptions{Force: true}); dropErr != nil {
				w.Print("WARNING: Failed to cleanup test site: %v", dropErr)
			}
		}()
	}

	// Build go test arguments.
	testArgs := buildTestArgs(cmd, projectRoot, siteName)

	// Discover test packages.
	packages, err := discoverTestPackages(cmd, projectRoot)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		w.Print("No test packages found.")
		return nil
	}

	testArgs = append(testArgs, packages...)

	w.Print("Running: go test %s", strings.Join(testArgs, " "))

	// Set up environment.
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("MOCA_TEST_SITE=%s", siteName),
		fmt.Sprintf("MOCA_TEST_PG_HOST=%s", cliCtx.Project.Infrastructure.Database.Host),
		fmt.Sprintf("MOCA_TEST_PG_PORT=%d", cliCtx.Project.Infrastructure.Database.Port),
	)
	if cliCtx.Project.Infrastructure.Redis.Host != "" {
		env = append(env, fmt.Sprintf("MOCA_TEST_REDIS_ADDR=%s:%d",
			cliCtx.Project.Infrastructure.Redis.Host,
			cliCtx.Project.Infrastructure.Redis.Port))
	}

	// Execute go test.
	goCmd := exec.Command("go", append([]string{"test"}, testArgs...)...)
	goCmd.Dir = projectRoot
	goCmd.Env = env
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr

	if err := goCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output.NewCLIError("Tests failed").
				WithErr(err).
				WithContext(fmt.Sprintf("Exit code: %d", exitErr.ExitCode()))
		}
		return err
	}

	w.Print("All tests passed.")
	return nil
}

func buildTestArgs(cmd *cobra.Command, projectRoot, siteName string) []string {
	args := []string{
		"-tags", "integration",
		"-count=1",
		"-race",
	}

	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		args = append(args, "-v")
	}
	if failfast, _ := cmd.Flags().GetBool("failfast"); failfast {
		args = append(args, "-failfast")
	}
	if filter, _ := cmd.Flags().GetString("filter"); filter != "" {
		args = append(args, "-run", filter)
	}
	if parallel, _ := cmd.Flags().GetInt("parallel"); parallel > 0 {
		args = append(args, fmt.Sprintf("-parallel=%d", parallel))
	}
	if coverage, _ := cmd.Flags().GetBool("coverage"); coverage {
		coverFile := filepath.Join(projectRoot, "coverage.out")
		args = append(args, fmt.Sprintf("-coverprofile=%s", coverFile), "-covermode=atomic")
	}

	return args
}

func discoverTestPackages(cmd *cobra.Command, projectRoot string) ([]string, error) {
	app, _ := cmd.Flags().GetString("app")

	if app != "" {
		// Test a specific app.
		appDir := filepath.Join(projectRoot, "apps", app)
		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			return nil, output.NewCLIError(fmt.Sprintf("App %q not found", app)).
				WithFix(fmt.Sprintf("Check that apps/%s exists.", app))
		}
		return []string{fmt.Sprintf("./apps/%s/...", app)}, nil
	}

	// Default: test all packages including framework and apps.
	packages := []string{"./pkg/..."}

	// Also include apps if they exist.
	appsDir := filepath.Join(projectRoot, "apps")
	if entries, err := os.ReadDir(appsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				packages = append(packages, fmt.Sprintf("./apps/%s/...", entry.Name()))
			}
		}
	}

	return packages, nil
}
