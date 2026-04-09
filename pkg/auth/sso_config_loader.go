package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// SSOConfigLoader loads SSO provider configurations directly from PostgreSQL.
// It bypasses DocManager for the same reason as UserLoader — SSO callbacks run
// as unauthenticated (Guest) requests and cannot construct a full DocContext.
type SSOConfigLoader struct {
	logger *slog.Logger
}

// NewSSOConfigLoader creates an SSOConfigLoader.
func NewSSOConfigLoader(logger *slog.Logger) *SSOConfigLoader {
	if logger == nil {
		logger = slog.Default()
	}
	return &SSOConfigLoader{logger: logger}
}

// Load returns the SSOConfigLoadFunc that can be passed to SSOHandler.
func (l *SSOConfigLoader) Load(ctx context.Context, site *tenancy.SiteContext, providerName string) (*SSOProviderConfig, error) {
	pool := site.Pool
	if pool == nil {
		return nil, fmt.Errorf("auth: site %q has no database pool", site.Name)
	}

	var cfg SSOProviderConfig
	var autoCreate, enabled bool

	err := pool.QueryRow(ctx,
		`SELECT
			"name", "provider_type", "enabled",
			COALESCE("client_id", ''), COALESCE("client_secret", ''),
			COALESCE("authorize_url", ''), COALESCE("token_url", ''),
			COALESCE("userinfo_url", ''), COALESCE("discovery_url", ''),
			COALESCE("scopes", 'openid email profile'),
			COALESCE("idp_entity_id", ''), COALESCE("idp_sso_url", ''),
			COALESCE("idp_certificate", ''),
			COALESCE("sp_private_key", ''), COALESCE("sp_certificate", ''),
			COALESCE("auto_create_user", false), COALESCE("default_role", ''),
			COALESCE("email_claim", 'email'), COALESCE("fullname_claim", 'name')
		 FROM "tab_sso_provider"
		 WHERE "name" = $1`,
		providerName,
	).Scan(
		&cfg.ProviderName, &cfg.ProviderType, &enabled,
		&cfg.ClientID, &cfg.ClientSecret,
		&cfg.AuthorizeURL, &cfg.TokenURL,
		&cfg.UserInfoURL, &cfg.DiscoveryURL,
		&cfg.Scopes,
		&cfg.IdPEntityID, &cfg.IdPSSOURL,
		&cfg.IdPCertificate,
		&cfg.SPPrivateKey, &cfg.SPCertificate,
		&autoCreate, &cfg.DefaultRole,
		&cfg.EmailClaim, &cfg.FullNameClaim,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrSSOProviderNotFound
		}
		return nil, fmt.Errorf("auth: load SSO provider %q: %w", providerName, err)
	}

	if !enabled {
		return nil, ErrSSOProviderNotFound
	}

	cfg.AutoCreateUser = autoCreate
	return &cfg, nil
}

// LoadFunc returns the SSOConfigLoadFunc function for use as a dependency.
func (l *SSOConfigLoader) LoadFunc() SSOConfigLoadFunc {
	return l.Load
}
