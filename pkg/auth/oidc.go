package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProvider implements OpenID Connect with auto-discovery.
// Discovery is performed lazily on first use to avoid blocking server startup.
type OIDCProvider struct {
	cfg         *SSOProviderConfig
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauth2Cfg   *oauth2.Config
	callbackURL string

	mu sync.Mutex
}

// NewOIDCProvider creates an OIDCProvider. Discovery happens lazily on first
// call to AuthURL or Exchange.
func NewOIDCProvider(cfg *SSOProviderConfig, callbackURL string) *OIDCProvider {
	return &OIDCProvider{
		cfg:         cfg,
		callbackURL: callbackURL,
	}
}

// AuthURL returns the IdP authorization URL with the given CSRF state parameter.
// It performs OIDC discovery on first call.
func (p *OIDCProvider) AuthURL(ctx context.Context, state string) (string, error) {
	if err := p.ensureDiscovered(ctx); err != nil {
		return "", err
	}
	return p.oauth2Cfg.AuthCodeURL(state), nil
}

// Exchange performs the authorization code exchange, validates the ID token,
// and extracts user claims.
func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*SSOResult, error) {
	if err := p.ensureDiscovered(ctx); err != nil {
		return nil, err
	}

	// Exchange authorization code for tokens.
	token, err := p.oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc token exchange: %w", err)
	}

	// Extract and verify ID token.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("oidc: no id_token in token response")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify id_token: %w", err)
	}

	// Extract claims from ID token.
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}

	return p.extractResult(claims)
}

// ensureDiscovered performs OIDC discovery if not yet done. Thread-safe.
func (p *OIDCProvider) ensureDiscovered(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.provider != nil {
		return nil
	}

	provider, err := oidc.NewProvider(ctx, p.cfg.DiscoveryURL)
	if err != nil {
		return fmt.Errorf("oidc discovery from %q: %w", p.cfg.DiscoveryURL, err)
	}

	scopes := []string{oidc.ScopeOpenID}
	if p.cfg.Scopes != "" {
		for _, s := range strings.Fields(p.cfg.Scopes) {
			if s != oidc.ScopeOpenID {
				scopes = append(scopes, s)
			}
		}
	}

	p.provider = provider
	p.verifier = provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID})
	p.oauth2Cfg = &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  p.callbackURL,
		Scopes:       scopes,
	}

	return nil
}

// extractResult maps IdP claims to an SSOResult using the configured claim names.
func (p *OIDCProvider) extractResult(claims map[string]any) (*SSOResult, error) {
	emailClaim := p.cfg.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}
	fullNameClaim := p.cfg.FullNameClaim
	if fullNameClaim == "" {
		fullNameClaim = "name"
	}

	email, _ := claims[emailClaim].(string)
	if email == "" {
		return nil, ErrSSOEmailMissing
	}

	fullName, _ := claims[fullNameClaim].(string)

	rawAttrs := make(map[string]string, len(claims))
	for k, v := range claims {
		rawAttrs[k] = fmt.Sprintf("%v", v)
	}

	return &SSOResult{
		Email:         email,
		FullName:      fullName,
		RawAttributes: rawAttrs,
	}, nil
}
