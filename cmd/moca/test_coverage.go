package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

func newTestCoverageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Generate test coverage report",
		Long: `Run tests and generate a coverage report aggregated per package.

Supports text, HTML, and JSON output formats. Can enforce a minimum
coverage threshold — exits with error if any package falls below it.`,
		RunE: runTestCoverage,
	}

	cmd.Flags().String("app", "", "Coverage for specific app")
	cmd.Flags().String("output", "text", "Output format: text, html, json")
	cmd.Flags().Float64("threshold", 0, "Minimum coverage % (exit error if below)")
	cmd.Flags().String("packages", "./pkg/...", "Comma-separated package patterns")

	return cmd
}

func runTestCoverage(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	projectRoot := cliCtx.ProjectRoot
	outputFmt, _ := cmd.Flags().GetString("output")
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	packagesFlag, _ := cmd.Flags().GetString("packages")
	app, _ := cmd.Flags().GetString("app")

	w := output.NewWriter(cmd)

	// Resolve packages.
	packages := strings.Split(packagesFlag, ",")
	if app != "" {
		packages = []string{fmt.Sprintf("./apps/%s/...", app)}
	}

	// Coverage output file.
	coverFile := filepath.Join(projectRoot, "coverage.out")
	defer func() {
		if outputFmt != "html" {
			_ = os.Remove(coverFile)
		}
	}()

	// Run go test with coverage.
	w.Print("Running tests with coverage...")
	testArgs := []string{
		"test",
		"-coverprofile=" + coverFile,
		"-covermode=atomic",
		"-count=1",
	}
	testArgs = append(testArgs, packages...)

	goCmd := exec.Command("go", testArgs...)
	goCmd.Dir = projectRoot
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr

	if runErr := goCmd.Run(); runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			w.Print("WARNING: Some tests failed (exit code %d), generating coverage anyway.", exitErr.ExitCode())
		} else {
			return output.NewCLIError("Failed to run tests").WithErr(runErr)
		}
	}

	// Parse coverage profile.
	stats, err := parseCoverageProfile(coverFile)
	if err != nil {
		return output.NewCLIError("Failed to parse coverage profile").WithErr(err)
	}

	if len(stats) == 0 {
		w.Print("No coverage data found.")
		return nil
	}

	switch outputFmt {
	case "html":
		htmlFile := filepath.Join(projectRoot, "coverage.html")
		coverCmd := exec.Command("go", "tool", "cover", "-html="+coverFile, "-o", htmlFile)
		coverCmd.Dir = projectRoot
		if err := coverCmd.Run(); err != nil {
			return output.NewCLIError("Failed to generate HTML coverage").WithErr(err)
		}
		w.Print("HTML coverage report: %s", htmlFile)

	case "json":
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintln(out, "[")
		for i, s := range stats {
			comma := ","
			if i == len(stats)-1 {
				comma = ""
			}
			_, _ = fmt.Fprintf(out,
				`  {"package": %q, "statements": %d, "covered": %d, "coverage": %.1f}%s`+"\n",
				s.pkg, s.stmts, s.covered, s.pct, comma)
		}
		_, _ = fmt.Fprintln(out, "]")

	default: // text
		w.Print("")
		w.Print("%-60s %10s %10s %10s", "PACKAGE", "STMTS", "COVERED", "COVERAGE")
		w.Print("%-60s %10s %10s %10s", strings.Repeat("-", 60), "-----", "-------", "--------")

		for _, s := range stats {
			marker := ""
			if threshold > 0 && s.pct < threshold {
				marker = " [BELOW THRESHOLD]"
			}
			w.Print("%-60s %10d %10d %9.1f%%%s", s.pkg, s.stmts, s.covered, s.pct, marker)
		}

		// Overall summary.
		var totalStmts, totalCovered int
		for _, s := range stats {
			totalStmts += s.stmts
			totalCovered += s.covered
		}
		overallPct := 0.0
		if totalStmts > 0 {
			overallPct = float64(totalCovered) / float64(totalStmts) * 100
		}

		w.Print("")
		w.Print("%-60s %10d %10d %9.1f%%", "TOTAL", totalStmts, totalCovered, overallPct)
		if threshold > 0 {
			w.Print("Threshold: %.1f%%", threshold)
		}
	}

	// Check threshold.
	if threshold > 0 {
		for _, s := range stats {
			if s.pct < threshold {
				return output.NewCLIError(
					fmt.Sprintf("Package %s coverage %.1f%% is below threshold %.1f%%", s.pkg, s.pct, threshold),
				)
			}
		}
	}

	return nil
}

type coverageStat struct {
	pkg     string
	stmts   int
	covered int
	pct     float64
}

// parseCoverageProfile reads a Go coverage profile and aggregates per-package.
func parseCoverageProfile(path string) ([]coverageStat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	type pkgAccum struct {
		stmts   int
		covered int
	}
	accum := make(map[string]*pkgAccum)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip the mode line.
		if strings.HasPrefix(line, "mode:") {
			continue
		}

		// Format: file:startLine.startCol,endLine.endCol numStmts count
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		// Extract package from file path.
		filePath := parts[0]
		colonIdx := strings.LastIndex(filePath, ":")
		if colonIdx < 0 {
			continue
		}
		file := filePath[:colonIdx]

		// Package is everything up to the last /.
		slashIdx := strings.LastIndex(file, "/")
		pkg := file
		if slashIdx > 0 {
			pkg = file[:slashIdx]
		}

		numStmts, _ := strconv.Atoi(parts[1])
		count, _ := strconv.Atoi(parts[2])

		if _, ok := accum[pkg]; !ok {
			accum[pkg] = &pkgAccum{}
		}
		accum[pkg].stmts += numStmts
		if count > 0 {
			accum[pkg].covered += numStmts
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	stats := make([]coverageStat, 0, len(accum))
	for pkg, a := range accum {
		pct := 0.0
		if a.stmts > 0 {
			pct = float64(a.covered) / float64(a.stmts) * 100
		}
		stats = append(stats, coverageStat{
			pkg:     pkg,
			stmts:   a.stmts,
			covered: a.covered,
			pct:     pct,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].pkg < stats[j].pkg
	})

	return stats, nil
}
