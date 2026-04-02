// Package stubb is a stub app module for the go-workspace spike (MS-00-T4).
// It intentionally requires testify v1.9.0 to demonstrate MVS version resolution:
// the workspace selects this as the maximum, upgrading stub-a's v1.8.0 requirement.
package stubb

import (
	"fmt"

	"github.com/osama1998H/moca/spikes/go-workspace/framework"
	"github.com/stretchr/testify/assert"
)

// GreetFromB returns a greeting that includes the framework version.
// Calling this proves cross-module imports within go.work resolve correctly.
func GreetFromB() string {
	return fmt.Sprintf("Greet from stub-b! Framework: %s", framework.FrameworkVersion())
}

// UseTestify calls assert.ObjectsAreEqual to exercise the testify dependency.
// This module's go.mod requests testify v1.9.0 — the maximum in the workspace.
func UseTestify() bool {
	return assert.ObjectsAreEqual(framework.FrameworkVersion(), framework.FrameworkVersion())
}
