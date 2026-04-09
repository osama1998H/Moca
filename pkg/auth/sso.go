package auth

import (
	"context"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// SSOResult is the protocol-agnostic output of an SSO authentication exchange.
// All three SSO protocols (OAuth2, OIDC, SAML) produce this after a successful
// identity assertion.
type SSOResult struct { //nolint:govet // field order prioritizes readability
	// Email is the user's email address from the identity provider.
	Email string
	// FullName is the user's display name from the identity provider.
	FullName string
	// Groups contains group/role names from the identity provider, if provided.
	Groups []string
	// RawAttributes holds the full set of attributes/claims returned by the IdP.
	RawAttributes map[string]string
}

// SSOProviderConfig holds the database-loaded configuration for an SSO provider.
// Password fields (ClientSecret, SPPrivateKey) must be decrypted before use
// since the config is loaded via direct SQL, bypassing DocManager's
// PostLoadTransformer.
type SSOProviderConfig struct {
	ProviderName   string
	ProviderType   string // "OAuth2", "OIDC", "SAML"
	ClientID       string
	ClientSecret   string // decrypted before use
	AuthorizeURL   string
	TokenURL       string
	UserInfoURL    string
	DiscoveryURL   string
	Scopes         string
	IdPEntityID    string
	IdPSSOURL      string
	IdPCertificate string
	SPPrivateKey   string // decrypted before use
	SPCertificate  string
	DefaultRole    string
	EmailClaim     string
	FullNameClaim  string
	AutoCreateUser bool
}

// SSOConfigLoadFunc loads an SSO provider config by name from a tenant's
// database. This follows the same pattern as UserLoadFunc — a function type
// that allows test mocking without a real database.
type SSOConfigLoadFunc func(ctx context.Context, site *tenancy.SiteContext, providerName string) (*SSOProviderConfig, error)
