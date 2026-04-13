package testutils

import (
	"strings"
	"testing"
)

// EnvOption configures a TestEnv during construction.
type EnvOption func(*envConfig)

type envConfig struct {
	prefix       string
	userEmail    string
	userFullName string
	userRoles    []string
	bootstrap    bool
	apps         []string
}

func defaultEnvConfig(t testing.TB) *envConfig {
	// Derive prefix from test name, sanitized for use in schema names.
	name := strings.ToLower(t.Name())
	name = siteNameSanitizer.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if len(name) > 20 {
		name = name[:20]
	}

	return &envConfig{
		prefix:       name,
		userEmail:    "test@moca.dev",
		userFullName: "Test User",
		userRoles:    []string{"System Manager"},
	}
}

// WithPrefix sets the site name prefix. Defaults to the test name.
func WithPrefix(prefix string) EnvOption {
	return func(c *envConfig) {
		c.prefix = prefix
	}
}

// WithUser overrides the default test user identity.
func WithUser(email, fullName string, roles ...string) EnvOption {
	return func(c *envConfig) {
		c.userEmail = email
		c.userFullName = fullName
		if len(roles) > 0 {
			c.userRoles = roles
		}
	}
}

// WithBootstrap enables core MetaType bootstrapping on the test site.
// This registers DocType, User, Role, and other builtin MetaTypes.
func WithBootstrap() EnvOption {
	return func(c *envConfig) {
		c.bootstrap = true
	}
}

// WithApps specifies app names to install in the test site after bootstrap.
func WithApps(apps ...string) EnvOption {
	return func(c *envConfig) {
		c.apps = append(c.apps, apps...)
	}
}
