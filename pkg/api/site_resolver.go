package api

import (
	"context"
	"fmt"

	"github.com/moca-framework/moca/pkg/orm"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// DBSiteResolver implements SiteResolver by looking up tenant pools through
// orm.DBManager. It is the default resolver used by moca-server.
type DBSiteResolver struct {
	db *orm.DBManager
}

// NewDBSiteResolver creates a SiteResolver backed by the given DBManager.
func NewDBSiteResolver(db *orm.DBManager) *DBSiteResolver {
	return &DBSiteResolver{db: db}
}

// ResolveSite resolves a site identifier to a SiteContext by obtaining the
// tenant's database pool from DBManager.ForSite.
func (r *DBSiteResolver) ResolveSite(ctx context.Context, siteID string) (*tenancy.SiteContext, error) {
	pool, err := r.db.ForSite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("resolve site %q: %w", siteID, err)
	}
	return &tenancy.SiteContext{
		Name: siteID,
		Pool: pool,
	}, nil
}
