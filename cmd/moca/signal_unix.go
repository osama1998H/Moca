//go:build !windows

package main

import "syscall"

// killProcess sends SIGTERM to a process by PID (Unix only).
func killProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
