//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/moca-framework/moca/internal/process"
)

var errNoServer = errors.New("no running server")

// stopServer stops a running Moca server by sending a signal and polling.
// Returns nil if no server was running (stale PID cleaned up).
// Returns errNoServer if no PID file exists.
func stopServer(projectRoot string, timeout time.Duration, force bool) error {
	pid, err := process.ReadPID(projectRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errNoServer
		}
		return errNoServer
	}

	if !process.IsRunning(pid) {
		_ = process.RemovePID(projectRoot)
		return nil
	}

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("send signal to PID %d: %w", pid, err)
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !process.IsRunning(pid) {
				_ = process.RemovePID(projectRoot)
				return nil
			}
		case <-deadline:
			if !force {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				time.Sleep(1 * time.Second)
				_ = process.RemovePID(projectRoot)
				return nil
			}
			return fmt.Errorf("process %d did not stop within %s", pid, timeout)
		}
	}
}
