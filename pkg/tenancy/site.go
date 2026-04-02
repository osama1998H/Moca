package tenancy

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSiteDisabled is returned by the resolver when a site's status is "disabled".
// The HTTP layer translates this to 503 Service Unavailable.
var ErrSiteDisabled = errors.New("site is disabled")

// SiteContext holds the identity and database pool for a single tenant site.
// It is carried through the document lifecycle via DocContext.
type SiteContext struct {
	Pool          *pgxpool.Pool
	Config        map[string]any
	Name          string
	DBSchema      string
	Status        string
	RedisPrefix   string
	StorageBucket string
	InstalledApps []string
}

// IsActive returns true if the site status is "active" or empty (backwards compat for tests).
func (sc *SiteContext) IsActive() bool {
	return sc.Status == "" || sc.Status == "active"
}

// PrefixRedisKey prepends the site's RedisPrefix to the given key.
func (sc *SiteContext) PrefixRedisKey(key string) string {
	return sc.RedisPrefix + key
}

// PrefixSearchIndex returns the Meilisearch index name for this site and doctype.
func (sc *SiteContext) PrefixSearchIndex(index string) string {
	return fmt.Sprintf("%s_%s", sc.Name, index)
}
