// Package process provides goroutine supervision and PID file utilities
// for the Moca framework's development server and process management.
package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	mocaDir = ".moca"
	pidFile = "process.pid"
)

// PIDPath returns the full path to the PID file for the given project directory.
func PIDPath(dir string) string {
	return filepath.Join(dir, mocaDir, pidFile)
}

// WritePID writes the current process ID to {dir}/.moca/process.pid.
// It creates the .moca directory if it does not exist.
func WritePID(dir string) error {
	pidDir := filepath.Join(dir, mocaDir)
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		return fmt.Errorf("process: create pid directory: %w", err)
	}
	path := filepath.Join(pidDir, pidFile)
	data := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("process: write pid file: %w", err)
	}
	return nil
}

// ReadPID reads and parses the PID from {dir}/.moca/process.pid.
func ReadPID(dir string) (int, error) {
	path := filepath.Join(dir, mocaDir, pidFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("process: read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("process: parse pid file: %w", err)
	}
	return pid, nil
}

// RemovePID removes the PID file at {dir}/.moca/process.pid.
// It returns nil if the file does not exist.
func RemovePID(dir string) error {
	path := filepath.Join(dir, mocaDir, pidFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("process: remove pid file: %w", err)
	}
	return nil
}

