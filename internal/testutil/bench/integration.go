package bench

import (
	"net/http"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
)

// IntegrationEnv is an alias for testutils.TestEnv, maintained for backward
// compatibility with existing benchmarks.
type IntegrationEnv = testutils.TestEnv

// GatewayBundle is an alias for testutils.GatewayBundle.
type GatewayBundle = testutils.GatewayBundle

// StaticSiteResolver is an alias for testutils.StaticSiteResolver.
type StaticSiteResolver = testutils.StaticSiteResolver

// NewIntegrationEnv provisions a unique tenant schema for integration benchmarks.
// This is a thin wrapper around testutils.NewTestEnv with benchmark-appropriate defaults.
func NewIntegrationEnv(tb testing.TB, prefix string) *IntegrationEnv {
	return testutils.NewTestEnv(tb, testutils.WithPrefix(prefix))
}

// Convenience re-export: NewGatewayBundle on the env already exists via TestEnv.
// This standalone function is kept for benchmark files that use it directly.
func NewGatewayBundleFromEnv(tb testing.TB, env *IntegrationEnv, defaultRate *meta.RateLimitConfig) *GatewayBundle {
	return env.NewGatewayBundle(tb, defaultRate)
}

// HandlerFromBundle extracts the HTTP handler from a GatewayBundle.
func HandlerFromBundle(b *GatewayBundle) http.Handler {
	return b.Handler
}
