package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/spf13/cobra"
)

func newTestRunUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run-ui",
		Short: "Run frontend/Playwright tests",
		Long: `Run UI tests using Playwright.

Creates an ephemeral test site, runs Playwright tests, parses the JSON report,
and displays structured results.

Requires: Node.js, npx, and Playwright installed.`,
		RunE: runTestRunUI,
	}

	f := cmd.Flags()
	f.String("app", "", "Test a specific app's UI tests")
	f.String("site", "", "Test site (auto-created if not specified)")
	f.Bool("headed", false, "Run in headed mode (visible browser)")
	f.String("browser", "chromium", "Browser: chromium, firefox, webkit")
	f.Int("workers", 1, "Parallel workers")
	f.String("filter", "", "Run tests matching pattern")
	f.Bool("update-snapshots", false, "Update visual regression snapshots")
	f.Bool("keep-site", false, "Don't cleanup test site after run")
	f.Bool("verbose", false, "Show Playwright's full output")

	return cmd
}

func runTestRunUI(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	projectRoot := cliCtx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")
	keepSite, _ := cmd.Flags().GetBool("keep-site")
	appName, _ := cmd.Flags().GetString("app")
	headed, _ := cmd.Flags().GetBool("headed")
	browser, _ := cmd.Flags().GetString("browser")
	workers, _ := cmd.Flags().GetInt("workers")
	filter, _ := cmd.Flags().GetString("filter")
	updateSnapshots, _ := cmd.Flags().GetBool("update-snapshots")

	// 1. Check prerequisites.
	if _, err := exec.LookPath("npx"); err != nil {
		return output.NewCLIError("npx not found").
			WithFix("Install Node.js and run: npm init playwright@latest")
	}

	// 2. Discover test directories.
	testDirs := discoverUITestDirs(projectRoot, appName)
	if len(testDirs) == 0 {
		w.PrintInfo("No UI test directories found.")
		w.Print("Expected: apps/{app}/tests/ui/ or desk/tests/ui/")
		return nil
	}

	// 3. Create ephemeral test site.
	siteName, _ := cmd.Flags().GetString("site")
	if siteName == "" {
		siteName = fmt.Sprintf("uitest_%d", time.Now().Unix())
	}

	ctx := cmd.Context()
	svc, err := newServices(ctx, cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	w.Print("Creating test site: %s", siteName)
	siteConfig := tenancy.SiteCreateConfig{
		Name:          siteName,
		AdminEmail:    "test@moca.dev",
		AdminPassword: "TestPass123!",
	}
	if err := svc.Sites.CreateSite(ctx, siteConfig); err != nil {
		return output.NewCLIError("Failed to create test site").
			WithErr(err).WithFix("Ensure PostgreSQL is running.")
	}

	if !keepSite {
		defer func() {
			w.Print("Dropping test site: %s", siteName)
			_ = svc.Sites.DropSite(ctx, siteName, tenancy.SiteDropOptions{Force: true})
		}()
	}

	// 4. Build Playwright arguments.
	serverPort := defaultDevPort
	if cliCtx.Project != nil && cliCtx.Project.Development.Port > 0 {
		serverPort = cliCtx.Project.Development.Port
	}

	jsonReportPath := filepath.Join(os.TempDir(), fmt.Sprintf("moca-pw-%d.json", time.Now().Unix()))
	defer func() { _ = os.Remove(jsonReportPath) }()

	env := append(os.Environ(),
		fmt.Sprintf("MOCA_TEST_BASE_URL=http://localhost:%d", serverPort),
		fmt.Sprintf("MOCA_TEST_SITE=%s", siteName),
		"MOCA_TEST_USER=Administrator",
		"MOCA_TEST_PASSWORD=TestPass123!",
		fmt.Sprintf("PLAYWRIGHT_JSON_OUTPUT_FILE=%s", jsonReportPath),
	)

	var totalPassed, totalFailed, totalSkipped int

	for _, testDir := range testDirs {
		pwArgs := []string{"playwright", "test", "--reporter=json,line"}

		configPath := filepath.Join(testDir, "playwright.config.ts")
		if fileExists(configPath) {
			pwArgs = append(pwArgs, "--config="+configPath)
		}

		if headed {
			pwArgs = append(pwArgs, "--headed")
		}
		if browser != "chromium" {
			pwArgs = append(pwArgs, "--project="+browser)
		}
		if workers > 0 {
			pwArgs = append(pwArgs, fmt.Sprintf("--workers=%d", workers))
		}
		if filter != "" {
			pwArgs = append(pwArgs, "--grep="+filter)
		}
		if updateSnapshots {
			pwArgs = append(pwArgs, "--update-snapshots")
		}

		w.Print("\nRunning UI tests: %s", testDir)

		npxCmd := exec.CommandContext(ctx, "npx", pwArgs...)
		npxCmd.Dir = projectRoot
		npxCmd.Env = env
		if verbose {
			npxCmd.Stdout = os.Stdout
			npxCmd.Stderr = os.Stderr
		}

		_ = npxCmd.Run() // Don't fail on non-zero exit; parse report instead.

		report, parseErr := parsePlaywrightReport(jsonReportPath)
		if parseErr != nil {
			w.PrintWarning(fmt.Sprintf("Could not parse report: %v", parseErr))
			continue
		}

		for _, suite := range report.Suites {
			printPlaywrightSuite(w, suite, &totalPassed, &totalFailed, &totalSkipped)
		}
	}

	total := totalPassed + totalFailed + totalSkipped
	w.Print("\n%d tests: %d passed, %d failed, %d skipped", total, totalPassed, totalFailed, totalSkipped)

	if totalFailed > 0 {
		return output.NewCLIError(fmt.Sprintf("%d UI test(s) failed", totalFailed))
	}
	return nil
}

func discoverUITestDirs(projectRoot, appName string) []string {
	var dirs []string
	if appName != "" {
		dir := filepath.Join(projectRoot, "apps", appName, "tests", "ui")
		if dirExists(dir) {
			dirs = append(dirs, dir)
		}
		return dirs
	}

	deskDir := filepath.Join(projectRoot, "desk", "tests", "ui")
	if dirExists(deskDir) {
		dirs = append(dirs, deskDir)
	}

	appsDir := filepath.Join(projectRoot, "apps")
	entries, _ := os.ReadDir(appsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(appsDir, e.Name(), "tests", "ui")
		if dirExists(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Playwright JSON report types.
type pwReport struct {
	Suites []pwSuite `json:"suites"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	Specs  []pwSpec  `json:"specs"`
	Suites []pwSuite `json:"suites"` // nested suites
}

type pwSpec struct {
	Title string   `json:"title"`
	Tests []pwTest `json:"tests"`
}

type pwTest struct {
	Status  string     `json:"status"`
	Results []pwResult `json:"results"`
}

type pwResult struct {
	Status   string  `json:"status"`
	Duration int     `json:"duration"`
	Error    pwError `json:"error"`
}

type pwError struct {
	Message string `json:"message"`
}

func parsePlaywrightReport(path string) (*pwReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report pwReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func printPlaywrightSuite(w *output.Writer, suite pwSuite, passed, failed, skipped *int) {
	for _, spec := range suite.Specs {
		status := "✓"
		dur := time.Duration(0)
		for _, test := range spec.Tests {
			for _, result := range test.Results {
				dur += time.Duration(result.Duration) * time.Millisecond
			}
			switch test.Status {
			case "passed", "expected":
				*passed++
			case "failed", "unexpected":
				*failed++
				status = "✗"
			case "skipped":
				*skipped++
				status = "○"
			}
		}
		w.Print("  %s %s  (%s)", status, spec.Title, dur.Round(time.Millisecond))
		if status == "✗" {
			for _, test := range spec.Tests {
				for _, result := range test.Results {
					if result.Error.Message != "" {
						w.Print("    Error: %s", result.Error.Message)
					}
				}
			}
		}
	}
	// Recurse into nested suites.
	for _, nested := range suite.Suites {
		printPlaywrightSuite(w, nested, passed, failed, skipped)
	}
}
