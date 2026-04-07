//go:build windows

package main

import (
	"fmt"
	"os"
)

// killProcess terminates a process by PID on Windows.
func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	return p.Kill()
}
