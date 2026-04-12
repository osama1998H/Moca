// Package dockerutil provides helpers for invoking Docker Compose across
// v1 (standalone docker-compose binary) and v2 (docker compose plugin).
package dockerutil

import (
	"os/exec"
	"sync"
)

var (
	composeOnce   sync.Once
	composeBinary string
	composePrefix []string
)

func detectCompose() {
	// Try v2 plugin first: "docker compose version"
	if err := exec.Command("docker", "compose", "version").Run(); err == nil {
		composeBinary = "docker"
		composePrefix = []string{"compose"}
		return
	}
	// Fall back to v1 standalone: "docker-compose version"
	if _, err := exec.LookPath("docker-compose"); err == nil {
		composeBinary = "docker-compose"
		composePrefix = nil
		return
	}
	// Default to v2 syntax — will produce a clear error at call site.
	composeBinary = "docker"
	composePrefix = []string{"compose"}
}

// ComposeArgs returns the binary name and full argument slice for a Docker
// Compose invocation. It detects v2 plugin ("docker compose ...") vs v1
// standalone ("docker-compose ...") once per process and caches the result.
//
//	binary, args := ComposeArgs("-f", "docker-compose.yml", "up", "-d")
//	// v2: binary="docker",          args=["compose", "-f", ..., "up", "-d"]
//	// v1: binary="docker-compose",  args=["-f", ..., "up", "-d"]
func ComposeArgs(args ...string) (binary string, fullArgs []string) {
	composeOnce.Do(detectCompose)
	fullArgs = make([]string, 0, len(composePrefix)+len(args))
	fullArgs = append(fullArgs, composePrefix...)
	fullArgs = append(fullArgs, args...)
	return composeBinary, fullArgs
}
