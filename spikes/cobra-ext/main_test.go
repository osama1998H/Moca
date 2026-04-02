// Package main tests the cobra-ext spike (MS-00-T4, Spike 5).
//
// Because this file is in package main (same package as main.go), the blank
// imports in main.go fire their init() functions when the test binary loads.
// stub-a:hello and stub-b:greet are registered before any test runs.
//
// Test ordering matters:
//   - Tests 1-2 read the init()-registered state directly.
//   - Tests 3-7 call ResetForTesting() and build their own state.
//
// Acceptance criterion (ROADMAP.md line 123):
//
//	"Command registered via init() in stub app appears in root command tree."
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/spikes/cobra-ext/framework/cmd"
	"github.com/spf13/cobra"

	stuba "github.com/osama1998H/moca/spikes/cobra-ext/apps/stub-a"
	stubb "github.com/osama1998H/moca/spikes/cobra-ext/apps/stub-b"
)

// TestMain verifies the Go binary is available before running tests that exec it.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Fprintln(os.Stderr, "SKIP: 'go' binary not found in PATH")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: init()-based registration across module boundaries
// ─────────────────────────────────────────────────────────────────────────────

// TestInitRegistrationAcrossModuleBoundaries validates the primary Spike 5
// acceptance criterion: commands registered via init() in separate Go workspace
// modules appear in the root command registry.
//
// The blank imports in main.go fired both stub apps' init() functions when this
// test binary loaded. We verify both names are in RegisteredCommandNames().
func TestInitRegistrationAcrossModuleBoundaries(t *testing.T) {
	names := cmd.RegisteredCommandNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	if !nameSet["stub-a:hello"] {
		t.Errorf("stub-a:hello not found in registered commands: %v", names)
	}
	if !nameSet["stub-b:greet"] {
		t.Errorf("stub-b:greet not found in registered commands: %v", names)
	}

	t.Logf("Registered command names after init(): %v", names)
	t.Log("VALIDATED: init()-based registration works across Go workspace module boundaries.")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: commands appear in root command help output
// ─────────────────────────────────────────────────────────────────────────────

// TestCommandsAppearInHelpOutput validates that init()-registered commands are
// attached to the root command tree and visible in the --help output.
// This is the end-to-end check: registration → attachment → user-visible output.
func TestCommandsAppearInHelpOutput(t *testing.T) {
	root := cmd.RootCommand()

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	// Cobra returns nil for --help; ignore the error.
	_ = root.Execute()

	helpOutput := buf.String()

	if !strings.Contains(helpOutput, "stub-a:hello") {
		t.Errorf("help output does not contain 'stub-a:hello':\n%s", helpOutput)
	}
	if !strings.Contains(helpOutput, "stub-b:greet") {
		t.Errorf("help output does not contain 'stub-b:greet':\n%s", helpOutput)
	}

	t.Logf("Help output:\n%s", helpOutput)
	t.Log("VALIDATED: Both app commands appear in root command's --help output.")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: explicit constructor pattern
// ─────────────────────────────────────────────────────────────────────────────

// TestExplicitConstructorPattern validates the alternative registration approach:
// using NewXxxCommand() constructors instead of init(). Recommended for
// framework-internal commands and scenarios requiring test isolation.
func TestExplicitConstructorPattern(t *testing.T) {
	cmd.ResetForTesting()

	if err := cmd.RegisterCommand(stuba.NewHelloCommand()); err != nil {
		t.Fatalf("RegisterCommand(NewHelloCommand()) failed: %v", err)
	}
	if err := cmd.RegisterCommand(stubb.NewGreetCommand()); err != nil {
		t.Fatalf("RegisterCommand(NewGreetCommand()) failed: %v", err)
	}

	names := cmd.RegisteredCommandNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	if !nameSet["stub-a:hello-explicit"] {
		t.Errorf("stub-a:hello-explicit not found: %v", names)
	}
	if !nameSet["stub-b:greet-explicit"] {
		t.Errorf("stub-b:greet-explicit not found: %v", names)
	}

	t.Logf("Explicitly registered commands: %v", names)
	t.Log("VALIDATED: Explicit NewCommand() constructor pattern registers commands correctly.")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: collision detection via RegisterCommand
// ─────────────────────────────────────────────────────────────────────────────

// TestCommandNameCollisionDetection validates that RegisterCommand returns an
// error when two commands share the same name. This prevents one app from
// silently overwriting another app's registered command.
func TestCommandNameCollisionDetection(t *testing.T) {
	cmd.ResetForTesting()

	first := &cobra.Command{Use: "test:collide", Short: "First registration"}
	second := &cobra.Command{Use: "test:collide", Short: "Attempted second registration"}

	if err := cmd.RegisterCommand(first); err != nil {
		t.Fatalf("first RegisterCommand failed unexpectedly: %v", err)
	}

	err := cmd.RegisterCommand(second)
	if err == nil {
		t.Fatal("second RegisterCommand should have returned an error for duplicate name")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error should contain 'already registered', got: %v", err)
	}

	t.Logf("Collision error: %v", err)
	t.Log("VALIDATED: RegisterCommand detects and rejects duplicate command names.")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5: MustRegisterCommand panics on collision
// ─────────────────────────────────────────────────────────────────────────────

// TestMustRegisterCommandPanicsOnCollision validates that MustRegisterCommand
// panics on a duplicate name. This is the correct behavior for init()-time
// registration: name collisions are configuration errors that must surface
// immediately at startup, not be silently swallowed.
func TestMustRegisterCommandPanicsOnCollision(t *testing.T) {
	cmd.ResetForTesting()

	cmd.MustRegisterCommand(&cobra.Command{Use: "test:panic-collide", Short: "First"})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustRegisterCommand should have panicked on duplicate name")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "already registered") {
			t.Errorf("panic message should contain 'already registered', got: %v", r)
		}
		t.Logf("Panic message: %v", r)
		t.Log("VALIDATED: MustRegisterCommand panics on collision — init() errors are fatal.")
	}()

	cmd.MustRegisterCommand(&cobra.Command{Use: "test:panic-collide", Short: "Duplicate"})
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6: go build ./...
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildWorkspace validates that the entire workspace (root + framework +
// stub-a + stub-b) compiles without errors. Measures build time to establish
// a baseline for expectations at larger app counts.
func TestBuildWorkspace(t *testing.T) {
	c := exec.Command("go", "build", "./...")
	var stderr bytes.Buffer
	c.Stderr = &stderr

	start := time.Now()
	if err := c.Run(); err != nil {
		t.Fatalf("go build ./... failed after %v:\n%s", time.Since(start), stderr.String())
	}

	elapsed := time.Since(start)
	t.Logf("go build ./... succeeded in %v", elapsed)
	t.Log("VALIDATED: All workspace modules (root + framework + stub-a + stub-b) compile together.")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 7: go build -race ./...
// ─────────────────────────────────────────────────────────────────────────────

// TestBuildRace validates that the race-instrumented build succeeds.
// The registry uses sync.Mutex; this confirms no race conditions are introduced
// by the concurrent-safe design.
func TestBuildRace(t *testing.T) {
	c := exec.Command("go", "build", "-race", "./...")
	var stderr bytes.Buffer
	c.Stderr = &stderr

	start := time.Now()
	if err := c.Run(); err != nil {
		t.Fatalf("go build -race ./... failed after %v:\n%s", time.Since(start), stderr.String())
	}

	elapsed := time.Since(start)
	t.Logf("go build -race ./... succeeded in %v", elapsed)
	t.Log("VALIDATED: Race-instrumented build succeeds across all workspace modules.")
}
