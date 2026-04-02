// Package stuba is a stub app module for the go-workspace spike (MS-00-T4).
// It intentionally requires testify v1.8.0 to demonstrate MVS version resolution:
// the workspace will select v1.9.0 (from stub-b) as the maximum version.
package stuba

import (
	"fmt"

	"github.com/osama1998H/moca/spikes/go-workspace/framework"
	"github.com/stretchr/testify/assert"
)

// HelloFromA returns a greeting that includes the framework version.
// Calling this proves cross-module imports within go.work resolve correctly.
func HelloFromA() string {
	return fmt.Sprintf("Hello from stub-a! Framework: %s", framework.FrameworkVersion())
}

// UseTestify calls assert.ObjectsAreEqual to exercise the testify dependency.
// This module's go.mod requests testify v1.8.0; MVS selects the workspace maximum.
func UseTestify() bool {
	return assert.ObjectsAreEqual(framework.FrameworkVersion(), framework.FrameworkVersion())
}
