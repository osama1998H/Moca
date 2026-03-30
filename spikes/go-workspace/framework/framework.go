// Package framework provides the stub framework module for the go-workspace spike.
// It exports a FrameworkVersion() function that both stub apps import, proving
// cross-module dependency resolution works correctly within a Go workspace.
package framework

// FrameworkVersion returns the current framework version string.
func FrameworkVersion() string {
	return "0.0.1-spike"
}
