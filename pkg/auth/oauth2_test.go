package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuth2Provider_AuthURL(t *testing.T) {
	cfg := &SSOProviderConfig{
		ClientID:     "my-client-id",
		AuthorizeURL: "https://idp.example.com/authorize",
		Scopes:       "openid email profile",
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/api/v1/auth/sso/callback")

	authURL := p.AuthURL("test-state-123")

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}

	if parsed.Scheme != "https" || parsed.Host != "idp.example.com" || parsed.Path != "/authorize" {
		t.Errorf("unexpected base URL: %s", authURL)
	}

	q := parsed.Query()
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want %q", got, "code")
	}
	if got := q.Get("client_id"); got != "my-client-id" {
		t.Errorf("client_id = %q, want %q", got, "my-client-id")
	}
	if got := q.Get("redirect_uri"); got != "https://app.example.com/api/v1/auth/sso/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := q.Get("state"); got != "test-state-123" {
		t.Errorf("state = %q, want %q", got, "test-state-123")
	}
	if got := q.Get("scope"); got != "openid email profile" {
		t.Errorf("scope = %q, want %q", got, "openid email profile")
	}
}

func TestOAuth2Provider_AuthURL_NoScopes(t *testing.T) {
	cfg := &SSOProviderConfig{
		ClientID:     "id",
		AuthorizeURL: "https://idp.example.com/authorize",
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/callback")

	authURL := p.AuthURL("s")
	parsed, _ := url.Parse(authURL)
	if parsed.Query().Get("scope") != "" {
		t.Error("expected no scope parameter when Scopes is empty")
	}
}

func TestOAuth2Provider_Exchange_Success(t *testing.T) {
	// Mock IdP server with token and userinfo endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.PostForm.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		if r.PostForm.Get("code") != "test-code-42" {
			t.Errorf("code = %q", r.PostForm.Get("code"))
		}
		if r.PostForm.Get("client_id") != "client-abc" {
			t.Errorf("client_id = %q", r.PostForm.Get("client_id"))
		}
		if r.PostForm.Get("client_secret") != "secret-xyz" {
			t.Errorf("client_secret = %q", r.PostForm.Get("client_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"access_token": "at-999",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer at-999" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer at-999")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"email": "alice@example.com",
			"name":  "Alice Smith",
			"sub":   "12345",
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ClientID:      "client-abc",
		ClientSecret:  "secret-xyz",
		TokenURL:      srv.URL + "/token",
		UserInfoURL:   srv.URL + "/userinfo",
		EmailClaim:    "email",
		FullNameClaim: "name",
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/callback")
	p.SetHTTPClient(srv.Client())

	result, err := p.Exchange(context.Background(), "test-code-42")
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}
	if result.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "alice@example.com")
	}
	if result.FullName != "Alice Smith" {
		t.Errorf("FullName = %q, want %q", result.FullName, "Alice Smith")
	}
	if result.RawAttributes["sub"] != "12345" {
		t.Errorf("RawAttributes[sub] = %q, want %q", result.RawAttributes["sub"], "12345")
	}
}

func TestOAuth2Provider_Exchange_InvalidCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		TokenURL:     srv.URL + "/token",
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/callback")
	p.SetHTTPClient(srv.Client())

	_, err := p.Exchange(context.Background(), "bad-code")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestOAuth2Provider_Exchange_MissingEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"}) //nolint:errcheck
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"name": "Bob",
			// no email field
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		TokenURL:     srv.URL + "/token",
		UserInfoURL:  srv.URL + "/userinfo",
		EmailClaim:   "email",
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/callback")
	p.SetHTTPClient(srv.Client())

	_, err := p.Exchange(context.Background(), "code")
	if err != ErrSSOEmailMissing {
		t.Fatalf("expected ErrSSOEmailMissing, got %v", err)
	}
}

func TestOAuth2Provider_Exchange_MissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token_type": "Bearer"}) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := &SSOProviderConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		TokenURL:     srv.URL,
	}
	p := NewOAuth2Provider(cfg, "https://app.example.com/callback")
	p.SetHTTPClient(srv.Client())

	_, err := p.Exchange(context.Background(), "code")
	if err == nil {
		t.Fatal("expected error for missing access_token")
	}
}

func TestOAuth2Provider_ExtractResult_CustomClaims(t *testing.T) {
	cfg := &SSOProviderConfig{
		EmailClaim:    "mail",
		FullNameClaim: "displayName",
	}
	p := NewOAuth2Provider(cfg, "")

	claims := map[string]any{
		"mail":        "custom@example.com",
		"displayName": "Custom Name",
		"department":  "Engineering",
	}

	result, err := p.extractResult(claims)
	if err != nil {
		t.Fatalf("extractResult failed: %v", err)
	}
	if result.Email != "custom@example.com" {
		t.Errorf("Email = %q", result.Email)
	}
	if result.FullName != "Custom Name" {
		t.Errorf("FullName = %q", result.FullName)
	}
}
