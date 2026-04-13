package testutils

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// GatewayBundle groups the test gateway and its fully wrapped HTTP handler.
type GatewayBundle struct {
	Gateway *api.Gateway
	Handler http.Handler
}

// StaticSiteResolver always returns the test site's SiteContext.
type StaticSiteResolver struct {
	Site *tenancy.SiteContext
}

// ResolveSite returns the configured test site for the matching site name.
func (r StaticSiteResolver) ResolveSite(_ context.Context, siteID string) (*tenancy.SiteContext, error) {
	if r.Site != nil && r.Site.Name == siteID {
		return r.Site, nil
	}
	return nil, fmt.Errorf("unknown test site %q", siteID)
}

// NewGatewayBundle builds a real API gateway and returns its fully wrapped
// handler for HTTP tests.
func (e *TestEnv) NewGatewayBundle(t testing.TB, defaultRate *meta.RateLimitConfig) *GatewayBundle {
	t.Helper()

	opts := []api.GatewayOption{
		api.WithDocManager(e.DocManager()),
		api.WithRegistry(e.Registry()),
		api.WithLogger(e.Logger),
		api.WithSiteResolver(StaticSiteResolver{Site: e.Site}),
	}
	if defaultRate != nil && e.Redis != nil {
		opts = append(opts, api.WithRateLimiter(api.NewRateLimiter(e.Redis, e.Logger), defaultRate))
	}

	gw := api.NewGateway(opts...)
	resource := api.NewResourceHandler(gw)
	resource.RegisterRoutes(gw.Mux(), "v1")
	gw.SetVersionRouter(api.NewVersionRouter(resource, e.Logger))

	return &GatewayBundle{
		Gateway: gw,
		Handler: gw.Handler(),
	}
}
