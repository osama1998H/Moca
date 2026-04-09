package core

import (
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

func init() {
	apps.MustRegisterInit("core", Initialize)
}

// Initialize registers all core app controllers and hooks with the provided
// registries. Called by the framework during app loading or server startup.
func Initialize(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {
	// Register the User controller override.
	cr.RegisterOverride("User", NewUserController)

	// Placeholder: register core doc event hooks here as needed.
	_ = hr
}
