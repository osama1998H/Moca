//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/moca-framework/moca/internal/process"
)

var errNoServer = errors.New("no running server")

// stopServer stops a running Moca server by terminating the process.
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

	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if force {
		if err := p.Kill(); err != nil {
			return fmt.Errorf("kill process %d: %w", pid, err)
		}
	} else {
		_ = p.Signal(os.Interrupt)
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
				_ = p.Kill()
				time.Sleep(1 * time.Second)
				_ = process.RemovePID(projectRoot)
				return nil
			}
			return fmt.Errorf("process %d did not stop within %s", pid, timeout)
		}
	}
}
