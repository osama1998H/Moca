//go:build windows

package process

import "os"

// IsRunning checks if a process with the given PID exists.
// On Windows, os.FindProcess always succeeds, so we probe liveness
// via Process.Signal(nil) which checks the process handle.
func IsRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(nil) == nil
}
