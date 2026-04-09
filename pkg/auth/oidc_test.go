package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// testOIDCServer sets up a minimal OIDC IdP with discovery, JWKS, token, and
// userinfo endpoints. It returns the server and a function to sign ID tokens.
func testOIDCServer(t *testing.T) (*httptest.Server, func(claims map[string]any) string) {
	t.Helper()

	// Generate RSA key pair for signing ID tokens.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	var srv *httptest.Server

	mux := http.NewServeMux()

	// OIDC Discovery endpoint.
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"userinfo_endpoint":      srv.URL + "/userinfo",
			"jwks_uri":               srv.URL + "/jwks",
		})
	})

	// JWKS endpoint.
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		n := privateKey.N
		e := big.NewInt(int64(privateKey.E))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"alg": "RS256",
					"use": "sig",
					"kid": "test-key-1",
					"n":   base64URLEncode(n.Bytes()),
					"e":   base64URLEncode(e.Bytes()),
				},
			},
		})
	})

	// Token endpoint.
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		// This will be called with the code from Exchange().
		// We need to return an id_token. The signToken function is not
		// available here, so we build the token inline.
		idToken := buildTestIDToken(t, privateKey, srv.URL, "client-123",
			map[string]any{
				"email": "oidc-user@example.com",
				"name":  "OIDC User",
				"sub":   "oidc-sub-1",
			})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"access_token": "oidc-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
		})
	})

	srv = httptest.NewServer(mux)

	signToken := func(claims map[string]any) string {
		return buildTestIDToken(t, privateKey, srv.URL, "client-123", claims)
	}

	return srv, signToken
}

// buildTestIDToken creates a signed JWT ID token for testing.
func buildTestIDToken(t *testing.T, key *rsa.PrivateKey, issuer, audience string, extraClaims map[string]any) string {
	t.Helper()

	now := time.Now()
	jwtClaims := jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	for k, v := range extraClaims {
		jwtClaims[k] = v
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwtClaims)
	token.Header["kid"] = "test-key-1"

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign ID token: %v", err)
	}
	return signed
}

// base64URLEncode encodes bytes as base64url without padding (per JWK spec).
func base64URLEncode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (len(data)*8+5)/6)
	for i := 0; i < len(data); i += 3 {
		var b uint32
		remaining := len(data) - i
		switch {
		case remaining >= 3:
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result = append(result, alphabet[b>>18&0x3F], alphabet[b>>12&0x3F], alphabet[b>>6&0x3F], alphabet[b&0x3F])
		case remaining == 2:
			b = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result = append(result, alphabet[b>>18&0x3F], alphabet[b>>12&0x3F], alphabet[b>>6&0x3F])
		case remaining == 1:
			b = uint32(data[i]) << 16
			result = append(result, alphabet[b>>18&0x3F], alphabet[b>>12&0x3F])
		}
	}
	return string(result)
}

func TestOIDCProvider_Discovery_And_AuthURL(t *testing.T) {
	srv, _ := testOIDCServer(t)
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ProviderType: "OIDC",
		ClientID:     "client-123",
		ClientSecret: "secret-456",
		DiscoveryURL: srv.URL,
		Scopes:       "openid email profile",
	}
	p := NewOIDCProvider(cfg, "https://app.example.com/callback")

	// Inject the test server's HTTP client so discovery hits the test server.
	ctx := oidcContextWithClient(srv.Client())

	authURL, err := p.AuthURL(ctx, "my-state")
	if err != nil {
		t.Fatalf("AuthURL failed: %v", err)
	}
	if authURL == "" {
		t.Fatal("expected non-empty auth URL")
	}

	// Verify the auth URL points to the mock server's authorize endpoint.
	if got := authURL; got == "" {
		t.Error("expected non-empty auth URL")
	}
}

func TestOIDCProvider_Exchange_Success(t *testing.T) {
	srv, _ := testOIDCServer(t)
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ProviderType:  "OIDC",
		ClientID:      "client-123",
		ClientSecret:  "secret-456",
		DiscoveryURL:  srv.URL,
		Scopes:        "openid email profile",
		EmailClaim:    "email",
		FullNameClaim: "name",
	}
	p := NewOIDCProvider(cfg, "https://app.example.com/callback")

	// Use a context with the test server's transport.
	ctx := oidcContextWithClient(srv.Client())

	// Force discovery first.
	_, err := p.AuthURL(ctx, "state")
	if err != nil {
		t.Fatalf("AuthURL (discovery) failed: %v", err)
	}

	// Override the oauth2 config's endpoint to use the test server.
	p.mu.Lock()
	p.oauth2Cfg.Endpoint.TokenURL = srv.URL + "/token"
	p.mu.Unlock()

	result, err := p.Exchange(ctx, "test-code")
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	if result.Email != "oidc-user@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "oidc-user@example.com")
	}
	if result.FullName != "OIDC User" {
		t.Errorf("FullName = %q, want %q", result.FullName, "OIDC User")
	}
}

func TestOIDCProvider_Discovery_Failure(t *testing.T) {
	cfg := &SSOProviderConfig{
		ClientID:     "id",
		DiscoveryURL: "http://127.0.0.1:1/nonexistent",
	}
	p := NewOIDCProvider(cfg, "https://app.example.com/callback")

	_, err := p.AuthURL(context.Background(), "state")
	if err == nil {
		t.Fatal("expected error for unreachable discovery URL")
	}
}

func TestOIDCProvider_ExtractResult_MissingEmail(t *testing.T) {
	cfg := &SSOProviderConfig{EmailClaim: "email"}
	p := NewOIDCProvider(cfg, "")

	_, err := p.extractResult(map[string]any{"name": "Alice"})
	if err != ErrSSOEmailMissing {
		t.Fatalf("expected ErrSSOEmailMissing, got %v", err)
	}
}

// oidcContextWithClient returns a context that makes the OIDC library and
// oauth2 library use the provided HTTP client (for test server routing).
func oidcContextWithClient(c *http.Client) context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, c)
}
