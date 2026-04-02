//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/pkg/cli"
)

// TestCLI_Serve_LifecycleAndPIDCleanup verifies that moca serve starts
// the HTTP server, writes a PID file, responds to health checks, and
// cleans up the PID file on context cancellation.
func TestCLI_Serve_LifecycleAndPIDCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping serve lifecycle test in short mode")
	}
	tmpDir := t.TempDir()

	// Create .moca directory (normally created by moca init).
	if err := os.MkdirAll(filepath.Join(tmpDir, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir .moca: %v", err)
	}

	// Use a timeout context so the test cannot hang indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(allCommands()...)
	root.PersistentPreRunE = nil

	cfg := testProjectConfig()
	cctx := &clicontext.CLIContext{
		ProjectRoot: tmpDir,
		Project:     cfg,
		Environment: "development",
	}
	cmdCtx := clicontext.WithCLIContext(ctx, cctx)
	root.SetContext(cmdCtx)
	// Use port 0 so the OS assigns a free port.
	root.SetArgs([]string{"serve", "--port", "0"})

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- root.Execute()
	}()

	// Poll for PID file to appear (indicates server is starting up).
	pidPath := process.PIDPath(tmpDir)
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("PID file did not appear within 10s")
		case err := <-serveDone:
			t.Fatalf("serve exited before PID file appeared: %v\nstdout: %s\nstderr: %s",
				err, outBuf.String(), errBuf.String())
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Verify PID file is valid.
	pid, err := process.ReadPID(tmpDir)
	if err != nil {
		t.Fatalf("read PID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID file contains %d, expected %d", pid, os.Getpid())
	}

	// Wait a bit for server to be fully ready, then check the banner.
	time.Sleep(500 * time.Millisecond)
	banner := outBuf.String()
	if !strings.Contains(banner, "Moca Development Server") {
		t.Errorf("banner missing expected header, got:\n%s", banner)
	}

	// Cancel context to trigger graceful shutdown.
	cancel()

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve exited with error: %v\nstderr: %s", err, errBuf.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not exit within 15s after cancel")
	}

	// PID file should be removed after shutdown.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file still exists after shutdown")
	}

	// Banner should contain "Server stopped."
	finalOutput := outBuf.String()
	if !strings.Contains(finalOutput, "Server stopped.") {
		t.Errorf("missing 'Server stopped.' in output:\n%s", finalOutput)
	}
}

// TestCLI_Serve_AlreadyRunning verifies that moca serve returns an error
// when a PID file exists for a running process.
func TestCLI_Serve_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()

	// Write PID file with our own PID (definitely running).
	if err := process.WritePID(tmpDir); err != nil {
		t.Fatalf("write PID: %v", err)
	}
	t.Cleanup(func() { _ = process.RemovePID(tmpDir) })

	_, _, err := executeWithContext(t, tmpDir, "", "serve", "--port", "0")
	if err == nil {
		t.Fatal("expected error for already-running server, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

// TestCLI_Serve_StalePIDCleanup verifies that a stale PID file (dead process)
// is cleaned up and serve proceeds.
// NOTE: In integration mode (with real DB), serve succeeds instead of failing
// on DB connection, so this test hangs. It is tested in unit mode instead.
func TestCLI_Serve_StalePIDCleanup(t *testing.T) {
	t.Skip("skipped in integration mode: serve succeeds with real DB, causing hang")
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, ".moca")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a PID that doesn't correspond to any running process.
	stalePID := 2147483647 // extremely unlikely to be running
	if err := os.WriteFile(filepath.Join(pidDir, "process.pid"), []byte(strconv.Itoa(stalePID)+"\n"), 0o644); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}

	// Serve will clean up the stale PID but then fail on DB connection (expected).
	// We just verify the PID file was cleaned up before the DB error.
	_, _, err := executeWithContext(t, tmpDir, "", "serve", "--port", "0")
	// The command will likely fail on server creation (DB), but that's OK.
	// The important thing is the stale PID was cleaned up.
	if err != nil && strings.Contains(err.Error(), "already running") {
		t.Error("serve should not report 'already running' for a dead PID")
	}
}

// TestCLI_Serve_NoWatch verifies that --no-watch starts without the file watcher.
func TestCLI_Serve_NoWatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping serve test in short mode")
	}
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(allCommands()...)
	root.PersistentPreRunE = nil

	cfg := testProjectConfig()
	cctx := &clicontext.CLIContext{
		ProjectRoot: tmpDir,
		Project:     cfg,
		Environment: "development",
	}
	cmdCtx := clicontext.WithCLIContext(ctx, cctx)
	root.SetContext(cmdCtx)
	root.SetArgs([]string{"serve", "--port", "0", "--no-watch"})

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- root.Execute()
	}()

	// Wait for startup.
	pidPath := process.PIDPath(tmpDir)
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("PID file did not appear within 10s")
		case err := <-serveDone:
			t.Fatalf("serve exited before PID file: %v\nstdout: %s\nstderr: %s",
				err, outBuf.String(), errBuf.String())
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	time.Sleep(300 * time.Millisecond)
	banner := outBuf.String()
	if !strings.Contains(banner, "Watcher:   disabled") {
		t.Errorf("expected 'Watcher:   disabled' in banner, got:\n%s", banner)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not exit after cancel")
	}
}

// TestCLI_Serve_HealthEndpoint verifies that the HTTP health endpoint responds
// after serve starts.
func TestCLI_Serve_HealthEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping serve test in short mode")
	}
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(allCommands()...)
	root.PersistentPreRunE = nil

	cfg := testProjectConfig()
	cctx := &clicontext.CLIContext{
		ProjectRoot: tmpDir,
		Project:     cfg,
		Environment: "development",
	}
	cmdCtx := clicontext.WithCLIContext(ctx, cctx)
	root.SetContext(cmdCtx)
	root.SetArgs([]string{"serve", "--port", "0"})

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- root.Execute()
	}()

	// Wait for the banner to contain a URL with the assigned port.
	var serverURL string
	deadline := time.After(10 * time.Second)
	for {
		output := outBuf.String()
		if idx := strings.Index(output, "http://"); idx != -1 {
			// Extract URL from "  URL:       http://0.0.0.0:XXXXX"
			line := output[idx:]
			if nl := strings.IndexByte(line, '\n'); nl != -1 {
				serverURL = strings.TrimSpace(line[:nl])
			}
			if serverURL != "" && !strings.HasSuffix(serverURL, ":0") {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatalf("server URL not found in output within 10s:\n%s", outBuf.String())
		case err := <-serveDone:
			t.Fatalf("serve exited early: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Hit the health endpoint.
	healthURL := serverURL + "/api/health"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check returned %d, expected 200", resp.StatusCode)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not exit after cancel")
	}
}

// TestCLI_Stop_NoServer verifies that moca stop returns an error when no
// server is running.
func TestCLI_Stop_NoServer(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, err := executeWithContext(t, tmpDir, "", "stop")
	if err == nil {
		t.Fatal("expected error for stop with no server, got nil")
	}
	if !strings.Contains(err.Error(), "No running") {
		t.Errorf("expected 'No running' error, got: %v", err)
	}
}

// TestCLI_Stop_StaleProcess verifies that moca stop cleans up a stale PID file
// when the process is not running.
func TestCLI_Stop_StaleProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, ".moca")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write PID for a non-existent process.
	stalePID := 2147483647
	if err := os.WriteFile(filepath.Join(pidDir, "process.pid"), []byte(strconv.Itoa(stalePID)+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, _, err := executeWithContext(t, tmpDir, "", "stop")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "Stale PID file removed") {
		t.Errorf("expected 'Stale PID file removed', got: %s", stdout)
	}

	// PID file should be gone.
	if _, err := os.Stat(filepath.Join(pidDir, "process.pid")); !os.IsNotExist(err) {
		t.Error("PID file still exists after stop")
	}
}

// TestCLI_Stop_RunningProcess verifies that moca stop terminates a running
// process and cleans up the PID file.
func TestCLI_Stop_RunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidDir := filepath.Join(tmpDir, ".moca")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Start a sleep subprocess to act as the "server".
	sleepCmd := exec.Command("sleep", "60")
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	sleepPID := sleepCmd.Process.Pid
	t.Cleanup(func() {
		_ = sleepCmd.Process.Kill()
		_ = sleepCmd.Wait()
	})

	// Write its PID.
	if err := os.WriteFile(
		filepath.Join(pidDir, "process.pid"),
		[]byte(fmt.Sprintf("%d\n", sleepPID)),
		0o644,
	); err != nil {
		t.Fatalf("write PID: %v", err)
	}

	stdout, _, err := executeWithContext(t, tmpDir, "", "stop")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Errorf("expected 'stopped' message, got: %s", stdout)
	}

	// Verify process is gone.
	if process.IsRunning(sleepPID) {
		t.Error("process still running after stop")
	}

	// PID file should be removed.
	if _, err := os.Stat(filepath.Join(pidDir, "process.pid")); !os.IsNotExist(err) {
		t.Error("PID file still exists after stop")
	}
}
