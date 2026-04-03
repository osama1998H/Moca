//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/pkg/cli"
)

type runningServeCommand struct {
	cancel    context.CancelFunc
	done      <-chan error
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
	pidPath   string
	siteName  string
	serverURL string
}

func reserveLoopbackPort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected TCPAddr, got %T", ln.Addr())
	}
	return tcpAddr.Port
}

func startServeCommand(t *testing.T, projectRoot string, extraArgs ...string) *runningServeCommand {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(projectRoot, ".moca"), 0o755); err != nil {
		t.Fatalf("mkdir .moca: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(allCommands()...)
	root.PersistentPreRunE = nil

	cfg := testProjectConfig()
	cctx := &clicontext.CLIContext{
		ProjectRoot: projectRoot,
		Project:     cfg,
		Environment: "development",
	}
	root.SetContext(clicontext.WithCLIContext(ctx, cctx))

	siteName := uniqueCLISiteName(t)
	cleanupCLISite(t, siteName)
	schemaName := "tenant_" + siteName
	if _, err := cliAdminPool.Exec(ctx, fmt.Sprintf(
		"CREATE SCHEMA IF NOT EXISTS %s",
		pgx.Identifier{schemaName}.Sanitize(),
	)); err != nil {
		cancel()
		t.Fatalf("create serve test schema: %v", err)
	}
	if _, err := cliAdminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, status, config, admin_email)
		VALUES ($1, $2, 'active', '{}'::jsonb, $3)
		ON CONFLICT (name) DO NOTHING
	`, siteName, schemaName, "serve@test.local"); err != nil {
		cancel()
		t.Fatalf("insert serve test site: %v", err)
	}

	port := reserveLoopbackPort(t)
	args := []string{"serve", "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	args = append(args, extraArgs...)
	root.SetArgs(args)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)

	done := make(chan error, 1)
	go func() {
		done <- root.Execute()
	}()

	pidPath := process.PIDPath(projectRoot)
	pidDeadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		select {
		case <-pidDeadline:
			cancel()
			t.Fatal("PID file did not appear within 10s")
		case err := <-done:
			cancel()
			t.Fatalf("serve exited before PID file appeared: %v\nstdout: %s\nstderr: %s",
				err, stdout.String(), stderr.String())
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	healthDeadline := time.After(10 * time.Second)
	for {
		req, err := http.NewRequest(http.MethodGet, serverURL+"/health", nil)
		if err != nil {
			cancel()
			t.Fatalf("build health request: %v", err)
		}
		req.Header.Set("X-Moca-Site", siteName)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return &runningServeCommand{
					cancel:    cancel,
					done:      done,
					stdout:    stdout,
					stderr:    stderr,
					pidPath:   pidPath,
					siteName:  siteName,
					serverURL: serverURL,
				}
			}
		}

		select {
		case <-healthDeadline:
			cancel()
			t.Fatal("server health endpoint did not become ready within 10s")
		case err := <-done:
			cancel()
			t.Fatalf("serve exited before health endpoint became ready: %v\nstdout: %s\nstderr: %s",
				err, stdout.String(), stderr.String())
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (srv *runningServeCommand) get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, srv.serverURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Moca-Site", srv.siteName)
	return (&http.Client{Timeout: 5 * time.Second}).Do(req)
}

func (srv *runningServeCommand) stop(t *testing.T) (string, string) {
	t.Helper()

	srv.cancel()

	select {
	case err := <-srv.done:
		if err != nil {
			t.Fatalf("serve exited with error: %v\nstdout: %s\nstderr: %s",
				err, srv.stdout.String(), srv.stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not exit within 15s after cancel")
	}

	return srv.stdout.String(), srv.stderr.String()
}

// TestCLI_Serve_LifecycleAndPIDCleanup verifies that moca serve starts
// the HTTP server, writes a PID file, responds to health checks, and
// cleans up the PID file on context cancellation.
func TestCLI_Serve_LifecycleAndPIDCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping serve lifecycle test in short mode")
	}
	tmpDir := t.TempDir()

	srv := startServeCommand(t, tmpDir)

	// Verify PID file is valid.
	pid, err := process.ReadPID(tmpDir)
	if err != nil {
		t.Fatalf("read PID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID file contains %d, expected %d", pid, os.Getpid())
	}

	resp, err := srv.get("/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health check returned %d, expected 200", resp.StatusCode)
	}

	stdout, _ := srv.stop(t)
	if !strings.Contains(stdout, "Moca Development Server") {
		t.Errorf("banner missing expected header, got:\n%s", stdout)
	}

	// PID file should be removed after shutdown.
	if _, err := os.Stat(srv.pidPath); !os.IsNotExist(err) {
		t.Error("PID file still exists after shutdown")
	}

	// Banner should contain "Server stopped."
	if !strings.Contains(stdout, "Server stopped.") {
		t.Errorf("missing 'Server stopped.' in output:\n%s", stdout)
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

	srv := startServeCommand(t, tmpDir, "--no-watch")
	stdout, _ := srv.stop(t)
	if !strings.Contains(stdout, "Watcher:   disabled") {
		t.Errorf("expected 'Watcher:   disabled' in banner, got:\n%s", stdout)
	}
}

// TestCLI_Serve_HealthEndpoint verifies that the HTTP health endpoint responds
// after serve starts.
func TestCLI_Serve_HealthEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping serve test in short mode")
	}
	tmpDir := t.TempDir()

	srv := startServeCommand(t, tmpDir)

	// Hit the health endpoint.
	healthURL := srv.serverURL + "/health"
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		t.Fatalf("build health request: %v", err)
	}
	req.Header.Set("X-Moca-Site", srv.siteName)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check returned %d, expected 200", resp.StatusCode)
	}

	srv.stop(t)
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

	stdout, _, err := executeWithContext(t, tmpDir, "", "stop", "--timeout", "2s")
	if err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Errorf("expected 'stopped' message, got: %s", stdout)
	}

	if waitErr := sleepCmd.Wait(); waitErr != nil && !strings.Contains(waitErr.Error(), "signal") {
		t.Fatalf("wait sleep: %v", waitErr)
	}

	// PID file should be removed.
	if _, err := os.Stat(filepath.Join(pidDir, "process.pid")); !os.IsNotExist(err) {
		t.Error("PID file still exists after stop")
	}
}
