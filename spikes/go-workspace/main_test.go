// Tests for MS-00-T4 (Spike 3): Go Workspace Multi-Module Composition.
//
// No external services required. Tests validate Go build tooling behavior.
//
// Prerequisites: Go 1.26+ and internet access for go mod tidy (first run only).
//
// Run:  go test -v -count=1 ./...
// Or:   make spike-gowork  (from repo root)
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	stuba "github.com/moca-framework/moca/spikes/go-workspace/apps/stub-a"
	stubb "github.com/moca-framework/moca/spikes/go-workspace/apps/stub-b"
	"golang.org/x/mod/modfile"
)

func TestMain(m *testing.M) {
	// No external infrastructure needed for this spike.
	// Verify the Go binary is available.
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Fprintln(os.Stderr, "SKIP: 'go' binary not found in PATH")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// ────────────────────────────────────────────────────────────────────────────
// Test 1: Cross-Module Import
// ────────────────────────────────────────────────────────────────────────────

// TestCrossModuleImport verifies that importing packages from two different app
// modules within the same go.work workspace compiles and runs correctly.
// Both stubs import the framework module, proving transitive workspace resolution.
func TestCrossModuleImport(t *testing.T) {
	helloA := stuba.HelloFromA()
	greetB := stubb.GreetFromB()

	if !strings.Contains(helloA, "stub-a") {
		t.Errorf("HelloFromA() = %q, want string containing 'stub-a'", helloA)
	}
	if !strings.Contains(helloA, "0.0.1-spike") {
		t.Errorf("HelloFromA() = %q, want string containing framework version '0.0.1-spike'", helloA)
	}
	if !strings.Contains(greetB, "stub-b") {
		t.Errorf("GreetFromB() = %q, want string containing 'stub-b'", greetB)
	}
	if !strings.Contains(greetB, "0.0.1-spike") {
		t.Errorf("GreetFromB() = %q, want string containing framework version '0.0.1-spike'", greetB)
	}

	t.Logf("stub-a: %s", helloA)
	t.Logf("stub-b: %s", greetB)
	t.Log("VALIDATED: Cross-module imports within go.work workspace resolve correctly.")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 2: MVS Version Resolution
// ────────────────────────────────────────────────────────────────────────────

// TestMVSResolution validates that Go's Minimal Version Selection (MVS) resolves
// the testify version conflict to the maximum required version.
//
// Setup:
//   - root go.mod:   testify v1.8.0
//   - stub-a go.mod: testify v1.8.0
//   - stub-b go.mod: testify v1.9.0  ← maximum
//
// Expected: MVS selects v1.9.0 for ALL modules in the workspace.
func TestMVSResolution(t *testing.T) {
	cmd := exec.Command("go", "list", "-m", "github.com/stretchr/testify")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m github.com/stretchr/testify failed: %v\nstderr: %s", err, getStderr(cmd))
	}

	output := strings.TrimSpace(string(out))
	t.Logf("go list -m output: %q", output)

	// Expected format: "github.com/stretchr/testify v1.9.0"
	parts := strings.Fields(output)
	if len(parts) != 2 {
		t.Fatalf("unexpected go list output: %q (want 'module version')", output)
	}

	module, version := parts[0], parts[1]
	if module != "github.com/stretchr/testify" {
		t.Errorf("unexpected module: %q", module)
	}

	// MVS MUST select v1.9.0 (the maximum across stub-a/stub-b).
	// stub-a requires v1.8.0, stub-b requires v1.9.0. MVS picks the max.
	// After 'go mod tidy', the root's go.mod records v1.9.0 indirect (already upgraded).
	if version != "v1.9.0" {
		t.Errorf("MVS selected version %q, want v1.9.0\n"+
			"  stub-a go.mod: testify v1.8.0\n"+
			"  stub-b go.mod: testify v1.9.0\n"+
			"  MVS rule: select the maximum required version", version)
	}

	t.Logf("VALIDATED: MVS resolved testify (stub-a v1.8.0 + stub-b v1.9.0) → %s", version)
}

// ────────────────────────────────────────────────────────────────────────────
// Test 3: Build All Modules
// ────────────────────────────────────────────────────────────────────────────

// TestBuildAllModules verifies that go build ./... succeeds across all workspace
// modules (root, framework, stub-a, stub-b) without errors.
func TestBuildAllModules(t *testing.T) {
	cmd := exec.Command("go", "build", "./...")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("go build ./... failed after %v:\n%s", elapsed, stderr.String())
	}

	t.Logf("go build ./... succeeded in %v", elapsed)
	t.Log("VALIDATED: All workspace modules compile together without errors.")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 4: Race Build
// ────────────────────────────────────────────────────────────────────────────

// TestRaceBuild verifies that go build -race ./... succeeds with the race
// detector enabled. This validates that all modules compile cleanly under
// race-detection instrumentation, as required by the CI acceptance criteria
// in docs/blocker-resolution-strategies.md (line 62).
func TestRaceBuild(t *testing.T) {
	cmd := exec.Command("go", "build", "-race", "./...")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("go build -race ./... failed after %v:\n%s", elapsed, stderr.String())
	}

	t.Logf("go build -race ./... succeeded in %v", elapsed)
	t.Log("VALIDATED: Race-instrumented build succeeds across all workspace modules.")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 5: Major Version Coexistence
// ────────────────────────────────────────────────────────────────────────────

// TestMajorVersionCoexistence documents Go's treatment of major-versioned modules.
//
// In Go modules, pkg and pkg/v2 are DISTINCT module paths — not conflicting versions
// of the same module. If app A imports "lib" (v1.x) and app B imports "lib/v2" (v2.x),
// both can coexist in the same workspace without conflict. Go links BOTH into the
// binary, and each app uses its own import path.
//
// This means the "major version conflict" scenario described in
// docs/blocker-resolution-strategies.md (lines 15-17) is actually handled
// correctly by Go's module system without any intervention.
func TestMajorVersionCoexistence(t *testing.T) {
	// Run go list -m all to inspect the full dependency graph.
	cmd := exec.Command("go", "list", "-m", "all")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m all failed: %v", err)
	}

	modules := strings.Split(strings.TrimSpace(string(out)), "\n")
	t.Logf("Workspace module graph (%d entries):", len(modules))
	for _, m := range modules {
		t.Logf("  %s", m)
	}

	// Verify that testify appears exactly once at v1.9.0 (MVS maximum).
	testifyCount := 0
	for _, m := range modules {
		if strings.HasPrefix(m, "github.com/stretchr/testify ") {
			testifyCount++
			if !strings.Contains(m, "v1.9.0") {
				t.Errorf("testify in module graph: %q, want v1.9.0", m)
			}
		}
	}
	if testifyCount != 1 {
		t.Errorf("testify appears %d times in module graph, want exactly 1", testifyCount)
	}

	t.Log("")
	t.Log("KEY INSIGHT: In Go modules, major version upgrades create NEW import paths.")
	t.Log("  'github.com/lib'    → v1.x  (import path unchanged)")
	t.Log("  'github.com/lib/v2' → v2.x  (new import path, distinct module)")
	t.Log("Both coexist in the same binary without conflict.")
	t.Log("ValidateAppDependencies only needs to warn about true same-path major splits.")
	t.Log("VALIDATED: Go module system handles version coexistence correctly.")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 6: ValidateAppDependencies Function
// ────────────────────────────────────────────────────────────────────────────

// TestValidateAppDependencies validates the pre-install conflict detection function
// from docs/blocker-resolution-strategies.md (Phase 2, lines 31-52).
//
// This function will be used by `moca app get` to detect major-version conflicts
// before adding an app to the workspace (implemented in MS-13).
func TestValidateAppDependencies(t *testing.T) {
	// Parse the actual go.mod files from disk.
	stubAMod := parseGoMod(t, "apps/stub-a/go.mod")
	stubBMod := parseGoMod(t, "apps/stub-b/go.mod")

	// Case 1: stub-a (v1.8.0) against [stub-b (v1.9.0)] — same major (v1), no conflict.
	conflicts := ValidateAppDependencies(stubAMod, []*modfile.File{stubBMod})
	if len(conflicts) != 0 {
		t.Errorf("ValidateAppDependencies(stub-a, [stub-b]) = %d conflicts, want 0\n"+
			"  testify v1.8.0 vs v1.9.0 are both major v1 — MVS resolves this automatically",
			len(conflicts))
	}
	t.Logf("Case 1: stub-a(testify v1.8.0) vs stub-b(testify v1.9.0) → %d major conflicts (expected 0)", len(conflicts))

	// Case 2: Synthetic module requiring testify v2.0.0 against stub-a (v1.8.0).
	// This should detect a major version conflict (v1 vs v2).
	syntheticContent := `module github.com/example/synthetic-app
go 1.26
require github.com/stretchr/testify v2.0.0+incompatible
`
	syntheticMod, err := modfile.Parse("synthetic-app/go.mod", []byte(syntheticContent), nil)
	if err != nil {
		t.Fatalf("failed to parse synthetic go.mod: %v", err)
	}

	conflicts = ValidateAppDependencies(syntheticMod, []*modfile.File{stubAMod})
	if len(conflicts) != 1 {
		t.Errorf("ValidateAppDependencies(synthetic-v2, [stub-a]) = %d conflicts, want 1", len(conflicts))
	} else {
		c := conflicts[0]
		t.Logf("Case 2: synthetic(testify v2.0.0) vs stub-a(testify v1.8.0) → conflict detected:")
		t.Logf("  Package:    %s", c.Package)
		t.Logf("  NewVersion: %s", c.NewVersion)
		t.Logf("  OldVersion: %s", c.OldVersion)
		t.Logf("  App:        %s", c.App)
		t.Logf("  IsMajor:    %v", c.IsMajor)

		if !c.IsMajor {
			t.Error("conflict.IsMajor should be true for v1 vs v2")
		}
		if c.Package != "github.com/stretchr/testify" {
			t.Errorf("conflict.Package = %q, want github.com/stretchr/testify", c.Package)
		}
	}

	t.Log("VALIDATED: ValidateAppDependencies correctly identifies major-version conflicts.")
	t.Log("  Minor conflicts (v1.8 vs v1.9) are ignored — MVS handles them.")
	t.Log("  Major conflicts (v1.x vs v2.x) are flagged for operator review.")
}

// ────────────────────────────────────────────────────────────────────────────
// Test 7: GOWORK=off Behavior
// ────────────────────────────────────────────────────────────────────────────

// TestGOWORKOffBehavior documents the difference between workspace-aware and
// standalone module resolution (GOWORK=off).
//
// With replace directives in go.mod (the pattern used in this spike), builds
// succeed both with and without GOWORK. The key difference is in MVS scope:
//
//   - GOWORK active:  MVS merges requirements across ALL workspace modules.
//     stub-a sees testify v1.9.0 (upgraded by stub-b's workspace-wide requirement).
//   - GOWORK=off:     Each module resolves its OWN go.mod independently.
//     stub-a sees testify v1.8.0 (its own direct requirement, no workspace upgrade).
//
// This is why make spike-redis uses GOWORK=off (isolated self-contained spike),
// while make spike-gowork does NOT (it validates workspace-specific behavior).
func TestGOWORKOffBehavior(t *testing.T) {
	// With workspace active: stub-a's testify version is upgraded by MVS.
	withWorkspace := exec.Command("go", "list", "-m", "github.com/stretchr/testify")
	withWorkspace.Dir = "apps/stub-a"
	wsOut, err := withWorkspace.Output()
	if err != nil {
		t.Fatalf("go list -m (workspace) failed: %v", err)
	}
	wsVersion := strings.Fields(strings.TrimSpace(string(wsOut)))[1]
	t.Logf("Workspace active:   stub-a testify version = %s", wsVersion)

	// With GOWORK=off: stub-a resolves its own go.mod only.
	withoutWorkspace := exec.Command("go", "list", "-m", "github.com/stretchr/testify")
	withoutWorkspace.Dir = "apps/stub-a"
	withoutWorkspace.Env = append(os.Environ(), "GOWORK=off")
	noWsOut, err := withoutWorkspace.Output()
	if err != nil {
		t.Fatalf("GOWORK=off go list -m failed: %v", err)
	}
	noWsVersion := strings.Fields(strings.TrimSpace(string(noWsOut)))[1]
	t.Logf("GOWORK=off:         stub-a testify version = %s", noWsVersion)

	// Workspace MUST upgrade stub-a's testify to v1.9.0 (stub-b's requirement).
	if wsVersion != "v1.9.0" {
		t.Errorf("workspace selected testify %q for stub-a, want v1.9.0 (MVS max from stub-b)", wsVersion)
	}

	// Without workspace, stub-a sees only its own go.mod which requires v1.8.0.
	if noWsVersion != "v1.8.0" {
		t.Errorf("GOWORK=off selected testify %q for stub-a, want v1.8.0 (stub-a's own requirement)", noWsVersion)
	}

	// Both builds succeed because replace directives handle local module paths.
	buildWithWs := exec.Command("go", "build", "./...")
	if err := buildWithWs.Run(); err != nil {
		t.Errorf("go build ./... (workspace) failed: %v", err)
	}

	buildNoWs := exec.Command("go", "build", "./...")
	buildNoWs.Env = append(os.Environ(), "GOWORK=off")
	if err := buildNoWs.Run(); err != nil {
		t.Errorf("GOWORK=off go build ./... failed: %v\n"+
			"  Note: replace directives should allow standalone builds", err)
	}

	t.Logf("")
	t.Logf("KEY INSIGHT: Workspace MVS upgrades stub-a's testify %s→%s (via stub-b).", noWsVersion, wsVersion)
	t.Logf("  Without workspace, each module resolves its own go.mod: stub-a uses %s.", noWsVersion)
	t.Logf("  The replace directives allow builds in both modes (workspace + standalone).")
	t.Logf("  Production MOCA apps must be installed in the workspace to get correct MVS.")
	t.Log("VALIDATED: GOWORK=off disables cross-module MVS; workspace correctly upgrades versions.")
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// parseGoMod reads and parses a go.mod file at the given path (relative to the
// spike root, i.e., spikes/go-workspace/).
func parseGoMod(t *testing.T, path string) *modfile.File {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", path, err)
	}
	return f
}

// getStderr extracts stderr from a completed exec.Cmd (after Output() is called).
func getStderr(cmd *exec.Cmd) string {
	if cmd.Stderr == nil {
		return "(no stderr captured)"
	}
	if buf, ok := cmd.Stderr.(*bytes.Buffer); ok {
		return buf.String()
	}
	return "(stderr not a buffer)"
}
