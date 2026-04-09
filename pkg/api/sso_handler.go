package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

const (
	// ssoStateKeyFmt is the Redis key format for CSRF state tokens.
	// Pattern: sso_state:{site}:{token}
	ssoStateKeyFmt = "sso_state:%s:%s"

	// ssoStateTTL is the time-to-live for CSRF state tokens (10 minutes).
	ssoStateTTL = 10 * time.Minute

	// ssoSessionCookieName matches the existing session cookie name.
	ssoSessionCookieName = "moca_sid"

	// ssoDefaultRedirect is the default redirect path after successful SSO login.
	ssoDefaultRedirect = "/desk"
)

// ssoStatePayload is stored in Redis alongside the CSRF state token. It carries
// the provider name and optional redirect URL so the callback handler can resume
// the flow without additional query parameters.
type ssoStatePayload struct {
	Provider   string `json:"provider"`
	Site       string `json:"site"`
	RedirectTo string `json:"redirect_to,omitempty"`
}

// SSOHandler manages SSO HTTP endpoints. It loads SSO provider configs from the
// database, routes to the appropriate protocol handler (OAuth2/OIDC/SAML), and
// finalizes authentication by creating a session and setting the moca_sid cookie.
type SSOHandler struct {
	sessions    *auth.SessionManager
	provisioner *auth.UserProvisioner
	loadConfig  auth.SSOConfigLoadFunc
	encryptor   *auth.FieldEncryptor // may be nil if encryption disabled
	stateStore  *redis.Client        // Redis Session DB (DB 2)
	logger      *slog.Logger
}

// NewSSOHandler creates an SSOHandler with the given dependencies.
func NewSSOHandler(
	sessions *auth.SessionManager,
	provisioner *auth.UserProvisioner,
	loadConfig auth.SSOConfigLoadFunc,
	encryptor *auth.FieldEncryptor,
	stateClient *redis.Client,
	logger *slog.Logger,
) *SSOHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SSOHandler{
		sessions:    sessions,
		provisioner: provisioner,
		loadConfig:  loadConfig,
		encryptor:   encryptor,
		stateStore:  stateClient,
		logger:      logger,
	}
}

// RegisterRoutes registers SSO endpoints on the mux, following the existing
// AuthHandler.RegisterRoutes pattern.
func (h *SSOHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("GET "+p+"/auth/sso/authorize", h.handleAuthorize)
	mux.HandleFunc("GET "+p+"/auth/sso/callback", h.handleCallback)
	mux.HandleFunc("GET "+p+"/auth/saml/metadata", h.handleSAMLMetadata)
	mux.HandleFunc("POST "+p+"/auth/saml/acs", h.handleSAMLACS)
}

// handleAuthorize initiates an SSO flow by redirecting to the IdP.
//
// Query parameters:
//   - provider (required): SSO provider name
//   - redirect_to (optional): relative path to redirect after login
func (h *SSOHandler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "provider query parameter is required")
		return
	}

	redirectTo := r.URL.Query().Get("redirect_to")
	if redirectTo != "" && !isRelativePath(redirectTo) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "redirect_to must be a relative path")
		return
	}

	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	cfg, err := h.loadAndDecryptConfig(r.Context(), site, providerName)
	if err != nil {
		h.logger.Warn("SSO authorize: provider not found",
			slog.String("provider", providerName),
			slog.String("site", site.Name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusNotFound, "SSO_PROVIDER_NOT_FOUND", "SSO provider not found or disabled")
		return
	}

	// Store CSRF state in Redis.
	state, err := h.storeState(r.Context(), site, ssoStatePayload{
		Provider:   providerName,
		Site:       site.Name,
		RedirectTo: redirectTo,
	})
	if err != nil {
		h.logger.Error("SSO authorize: store state failed",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to initiate SSO")
		return
	}

	// Build the callback URL for the IdP to redirect back to.
	callbackURL := buildCallbackURL(r)

	// Generate auth URL based on provider type.
	var authURL string
	switch cfg.ProviderType {
	case "OAuth2":
		p := auth.NewOAuth2Provider(cfg, callbackURL)
		authURL = p.AuthURL(state)

	case "OIDC":
		p := auth.NewOIDCProvider(cfg, callbackURL)
		u, err := p.AuthURL(r.Context(), state)
		if err != nil {
			h.logger.Error("SSO authorize: OIDC discovery failed",
				slog.String("provider", providerName),
				slog.String("error", err.Error()),
			)
			h.ssoErrorRedirect(w, r, "SSO provider configuration error")
			return
		}
		authURL = u

	case "SAML":
		metadataURL := buildSAMLMetadataURL(r, providerName)
		acsURL := buildSAMLACSURL(r)
		sp, err := auth.NewSAMLProvider(cfg, metadataURL, acsURL)
		if err != nil {
			h.logger.Error("SSO authorize: SAML provider init failed",
				slog.String("provider", providerName),
				slog.String("error", err.Error()),
			)
			h.ssoErrorRedirect(w, r, "SSO provider configuration error")
			return
		}
		u, err := sp.AuthURL(state)
		if err != nil {
			h.logger.Error("SSO authorize: SAML auth URL failed",
				slog.String("error", err.Error()),
			)
			h.ssoErrorRedirect(w, r, "SSO provider error")
			return
		}
		authURL = u

	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			fmt.Sprintf("unsupported provider type %q", cfg.ProviderType))
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback processes the OAuth2/OIDC callback after IdP authorization.
func (h *SSOHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateParam := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if stateParam == "" || code == "" {
		h.ssoErrorRedirect(w, r, "Missing state or code parameter")
		return
	}

	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	// Validate and consume CSRF state (single-use).
	payload, err := h.validateState(r.Context(), site, stateParam)
	if err != nil {
		h.logger.Warn("SSO callback: invalid state",
			slog.String("site", site.Name),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "Invalid or expired SSO state")
		return
	}

	cfg, err := h.loadAndDecryptConfig(r.Context(), site, payload.Provider)
	if err != nil {
		h.logger.Warn("SSO callback: provider not found",
			slog.String("provider", payload.Provider),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "SSO provider not found")
		return
	}

	callbackURL := buildCallbackURL(r)

	// Exchange the authorization code for user identity.
	var result *auth.SSOResult
	switch cfg.ProviderType {
	case "OAuth2":
		p := auth.NewOAuth2Provider(cfg, callbackURL)
		result, err = p.Exchange(r.Context(), code)

	case "OIDC":
		p := auth.NewOIDCProvider(cfg, callbackURL)
		result, err = p.Exchange(r.Context(), code)

	default:
		h.ssoErrorRedirect(w, r, "Invalid provider type for OAuth2/OIDC callback")
		return
	}

	if err != nil {
		h.logger.Error("SSO callback: exchange failed",
			slog.String("provider", payload.Provider),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "SSO authentication failed")
		return
	}

	h.completeSSO(w, r, site, cfg, result, payload.RedirectTo)
}

// handleSAMLMetadata serves the SAML SP metadata XML.
func (h *SSOHandler) handleSAMLMetadata(w http.ResponseWriter, r *http.Request) {
	providerName := r.URL.Query().Get("provider")
	if providerName == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "provider query parameter is required")
		return
	}

	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	cfg, err := h.loadAndDecryptConfig(r.Context(), site, providerName)
	if err != nil {
		writeError(w, http.StatusNotFound, "SSO_PROVIDER_NOT_FOUND", "SSO provider not found or disabled")
		return
	}

	metadataURL := buildSAMLMetadataURL(r, providerName)
	acsURL := buildSAMLACSURL(r)
	sp, err := auth.NewSAMLProvider(cfg, metadataURL, acsURL)
	if err != nil {
		h.logger.Error("SSO metadata: provider init failed",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to generate metadata")
		return
	}

	xmlData, err := sp.Metadata()
	if err != nil {
		h.logger.Error("SSO metadata: marshal failed",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to generate metadata")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(xmlData) //nolint:errcheck
}

// handleSAMLACS processes the SAML Assertion Consumer Service POST.
func (h *SSOHandler) handleSAMLACS(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	if err := r.ParseForm(); err != nil {
		h.ssoErrorRedirect(w, r, "Invalid SAML response")
		return
	}

	relayState := r.FormValue("RelayState")
	if relayState == "" {
		h.ssoErrorRedirect(w, r, "Missing SAML RelayState")
		return
	}

	payload, err := h.validateState(r.Context(), site, relayState)
	if err != nil {
		h.logger.Warn("SSO ACS: invalid state",
			slog.String("site", site.Name),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "Invalid or expired SSO state")
		return
	}

	cfg, err := h.loadAndDecryptConfig(r.Context(), site, payload.Provider)
	if err != nil {
		h.ssoErrorRedirect(w, r, "SSO provider not found")
		return
	}

	metadataURL := buildSAMLMetadataURL(r, payload.Provider)
	acsURL := buildSAMLACSURL(r)
	sp, err := auth.NewSAMLProvider(cfg, metadataURL, acsURL)
	if err != nil {
		h.logger.Error("SSO ACS: provider init failed",
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "SSO provider configuration error")
		return
	}

	result, err := sp.ParseResponse(r)
	if err != nil {
		h.logger.Error("SSO ACS: parse SAML response failed",
			slog.String("provider", payload.Provider),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "SAML authentication failed")
		return
	}

	h.completeSSO(w, r, site, cfg, result, payload.RedirectTo)
}

// completeSSO is the shared finalization logic for all SSO protocols.
// It provisions the user, creates a session, sets the cookie, and redirects.
func (h *SSOHandler) completeSSO(
	w http.ResponseWriter,
	r *http.Request,
	site *tenancy.SiteContext,
	cfg *auth.SSOProviderConfig,
	result *auth.SSOResult,
	redirectTo string,
) {
	// Find or create the local user.
	user, err := h.provisioner.FindOrCreate(
		r.Context(), site,
		result.Email, result.FullName,
		cfg.AutoCreateUser, cfg.DefaultRole,
	)
	if err != nil {
		h.logger.Error("SSO: user provisioning failed",
			slog.String("email", result.Email),
			slog.String("provider", cfg.ProviderName),
			slog.String("site", site.Name),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "User account not found or disabled")
		return
	}

	// Create session.
	sid, err := h.sessions.Create(r.Context(), user, site.Name)
	if err != nil {
		h.logger.Error("SSO: session creation failed",
			slog.String("email", result.Email),
			slog.String("error", err.Error()),
		)
		h.ssoErrorRedirect(w, r, "Failed to create session")
		return
	}

	// Set session cookie (matches auth_handler.go pattern).
	http.SetCookie(w, &http.Cookie{
		Name:     ssoSessionCookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})

	h.logger.Info("SSO login successful",
		slog.String("user", user.Email),
		slog.String("provider", cfg.ProviderName),
		slog.String("site", site.Name),
	)

	// Redirect to the requested page or /desk.
	if redirectTo == "" {
		redirectTo = ssoDefaultRedirect
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// loadAndDecryptConfig loads an SSO provider config and decrypts Password fields.
func (h *SSOHandler) loadAndDecryptConfig(
	ctx context.Context,
	site *tenancy.SiteContext,
	providerName string,
) (*auth.SSOProviderConfig, error) {
	cfg, err := h.loadConfig(ctx, site, providerName)
	if err != nil {
		return nil, err
	}

	// Decrypt Password-type fields if encryption is enabled.
	// These fields are stored encrypted via FieldEncryptionHook but loaded
	// via direct SQL (bypassing PostLoadTransformer), so we decrypt explicitly.
	if h.encryptor != nil {
		if cfg.ClientSecret != "" {
			cfg.ClientSecret, err = h.encryptor.Decrypt(cfg.ClientSecret)
			if err != nil {
				return nil, fmt.Errorf("decrypt client_secret: %w", err)
			}
		}
		if cfg.SPPrivateKey != "" {
			cfg.SPPrivateKey, err = h.encryptor.Decrypt(cfg.SPPrivateKey)
			if err != nil {
				return nil, fmt.Errorf("decrypt sp_private_key: %w", err)
			}
		}
	}

	return cfg, nil
}

// storeState persists a CSRF state token in Redis with 10-min TTL.
func (h *SSOHandler) storeState(ctx context.Context, site *tenancy.SiteContext, payload ssoStatePayload) (string, error) {
	token, err := generateSSOStateToken()
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal state payload: %w", err)
	}

	key := fmt.Sprintf(ssoStateKeyFmt, site.Name, token)
	if err := h.stateStore.Set(ctx, key, data, ssoStateTTL).Err(); err != nil {
		return "", fmt.Errorf("store SSO state: %w", err)
	}

	return token, nil
}

// validateState retrieves and deletes (single-use) a CSRF state token.
func (h *SSOHandler) validateState(ctx context.Context, site *tenancy.SiteContext, token string) (*ssoStatePayload, error) {
	key := fmt.Sprintf(ssoStateKeyFmt, site.Name, token)

	data, err := h.stateStore.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, auth.ErrSSOStateInvalid
		}
		return nil, fmt.Errorf("get SSO state: %w", err)
	}

	var payload ssoStatePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal state payload: %w", err)
	}

	return &payload, nil
}

// ssoErrorRedirect redirects to the login page with an error message.
// SSO endpoints are browser flows, so errors are communicated via redirect.
func (h *SSOHandler) ssoErrorRedirect(w http.ResponseWriter, r *http.Request, message string) {
	target := "/desk/login?error=sso_failed&message=" + url.QueryEscape(message)
	http.Redirect(w, r, target, http.StatusFound)
}

// buildCallbackURL constructs the OAuth2/OIDC callback URL from the request.
func buildCallbackURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}
	return fmt.Sprintf("%s://%s/api/v1/auth/sso/callback", scheme, r.Host)
}

// buildSAMLMetadataURL constructs the SAML SP metadata URL from the request.
func buildSAMLMetadataURL(r *http.Request, provider string) string {
	scheme := requestScheme(r)
	return fmt.Sprintf("%s://%s/api/v1/auth/saml/metadata?provider=%s",
		scheme, r.Host, url.QueryEscape(provider))
}

// buildSAMLACSURL constructs the SAML ACS URL from the request.
func buildSAMLACSURL(r *http.Request) string {
	scheme := requestScheme(r)
	return fmt.Sprintf("%s://%s/api/v1/auth/saml/acs", scheme, r.Host)
}

// requestScheme returns the request's scheme, respecting X-Forwarded-Proto.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		return fwd
	}
	return "http"
}

// isRelativePath returns true if the path starts with "/" and does not contain
// a scheme or authority component (preventing open redirects).
func isRelativePath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")
}

// generateSSOStateToken produces a cryptographically random 32-byte hex string.
func generateSSOStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate SSO state token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
