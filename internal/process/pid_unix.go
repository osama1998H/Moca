//go:build !windows

package process

import (
	"errors"
	"syscall"
)

// IsRunning checks if a process with the given PID exists by sending signal 0.
// Returns true if the process exists (even if owned by another user).
func IsRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
