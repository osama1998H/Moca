package process_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/internal/process"
)

func TestPID_WriteReadRemoveCycle(t *testing.T) {
	dir := t.TempDir()

	if err := process.WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pid, err := process.ReadPID(dir)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("ReadPID = %d, want %d", pid, os.Getpid())
	}

	if err := process.RemovePID(dir); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}

	// File should be gone.
	path := process.PIDPath(dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("PID file still exists after RemovePID")
	}
}

func TestReadPID_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := process.ReadPID(dir)
	if err == nil {
		t.Fatal("ReadPID on missing file should return error")
	}
}

func TestRemovePID_MissingFileNoError(t *testing.T) {
	dir := t.TempDir()
	if err := process.RemovePID(dir); err != nil {
		t.Fatalf("RemovePID on missing file should not error: %v", err)
	}
}

func TestWritePID_CreatesMocaDirectory(t *testing.T) {
	dir := t.TempDir()

	// .moca/ does not exist yet.
	mocaDir := filepath.Join(dir, ".moca")
	if _, err := os.Stat(mocaDir); !os.IsNotExist(err) {
		t.Fatal(".moca directory should not exist before WritePID")
	}

	if err := process.WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	info, err := os.Stat(mocaDir)
	if err != nil {
		t.Fatalf(".moca directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error(".moca should be a directory")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	if !process.IsRunning(os.Getpid()) {
		t.Error("IsRunning should return true for current process")
	}
}

func TestIsRunning_DeadProcess(t *testing.T) {
	// Start a short-lived subprocess and wait for it to exit.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run subprocess: %v", err)
	}
	deadPID := cmd.Process.Pid

	if process.IsRunning(deadPID) {
		t.Errorf("IsRunning(%d) = true, want false for dead process", deadPID)
	}
}
