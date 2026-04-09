package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OAuth2Provider implements the OAuth2 Authorization Code flow.
// It handles redirect URL generation and authorization code exchange.
type OAuth2Provider struct {
	cfg *SSOProviderConfig
	// httpClient allows injection of a custom HTTP client for testing.
	httpClient  *http.Client
	callbackURL string
}

// NewOAuth2Provider creates an OAuth2Provider from the given configuration.
// The callbackURL is the full URL that the IdP should redirect to after
// authorization (e.g., "https://site.example.com/api/v1/auth/sso/callback").
func NewOAuth2Provider(cfg *SSOProviderConfig, callbackURL string) *OAuth2Provider {
	return &OAuth2Provider{
		cfg:         cfg,
		callbackURL: callbackURL,
		httpClient:  http.DefaultClient,
	}
}

// SetHTTPClient overrides the HTTP client used for token exchange and
// userinfo requests. Used in tests to point at httptest.Server.
func (p *OAuth2Provider) SetHTTPClient(c *http.Client) {
	p.httpClient = c
}

// AuthURL returns the IdP authorization URL with the given CSRF state parameter.
func (p *OAuth2Provider) AuthURL(state string) string {
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {p.cfg.ClientID},
		"redirect_uri":  {p.callbackURL},
		"state":         {state},
	}
	if p.cfg.Scopes != "" {
		params.Set("scope", p.cfg.Scopes)
	}
	return p.cfg.AuthorizeURL + "?" + params.Encode()
}

// Exchange performs the authorization code exchange and fetches user info.
// It POSTs to the token endpoint, then GETs the userinfo endpoint with the
// access token, and returns the extracted user identity.
func (p *OAuth2Provider) Exchange(ctx context.Context, code string) (*SSOResult, error) {
	// Step 1: Exchange authorization code for access token.
	tokenData, err := p.exchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oauth2 token exchange: %w", err)
	}

	accessToken, ok := tokenData["access_token"].(string)
	if !ok || accessToken == "" {
		return nil, fmt.Errorf("oauth2: no access_token in token response")
	}

	// Step 2: Fetch user info using the access token.
	return p.fetchUserInfo(ctx, accessToken)
}

// exchangeCode POSTs the authorization code to the token endpoint and returns
// the parsed JSON response.
func (p *OAuth2Provider) exchangeCode(ctx context.Context, code string) (map[string]any, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.callbackURL},
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return result, nil
}

// fetchUserInfo GETs the userinfo endpoint with the access token as a Bearer
// token and extracts user attributes using the configured claim names.
func (p *OAuth2Provider) fetchUserInfo(ctx context.Context, accessToken string) (*SSOResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send userinfo request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read userinfo response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("parse userinfo response: %w", err)
	}

	return p.extractResult(claims)
}

// extractResult maps IdP claims to an SSOResult using the configured claim names.
func (p *OAuth2Provider) extractResult(claims map[string]any) (*SSOResult, error) {
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

	// Collect all claims as raw attributes.
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
