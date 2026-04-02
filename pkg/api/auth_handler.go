package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/pkg/auth"
)

// AuthHandler provides login, logout, and token refresh HTTP endpoints.
type AuthHandler struct {
	sessions *auth.SessionManager
	loadUser auth.UserLoadFunc
	logger   *slog.Logger
	jwtCfg   auth.JWTConfig
}

// NewAuthHandler creates an AuthHandler with the given dependencies.
func NewAuthHandler(
	jwtCfg auth.JWTConfig,
	sessions *auth.SessionManager,
	userLoader *auth.UserLoader,
	logger *slog.Logger,
) *AuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthHandler{
		jwtCfg:   jwtCfg,
		sessions: sessions,
		loadUser: userLoader.LoadByEmail,
		logger:   logger,
	}
}

// NewAuthHandlerWithLoader creates an AuthHandler with a custom user load function.
// Useful for testing or when user loading does not go through the standard UserLoader.
func NewAuthHandlerWithLoader(
	jwtCfg auth.JWTConfig,
	sessions *auth.SessionManager,
	loader auth.UserLoadFunc,
	logger *slog.Logger,
) *AuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthHandler{
		jwtCfg:   jwtCfg,
		sessions: sessions,
		loadUser: loader,
		logger:   logger,
	}
}

// RegisterRoutes registers auth endpoints on the mux.
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("POST "+p+"/auth/login", h.handleLogin)
	mux.HandleFunc("POST "+p+"/auth/logout", h.handleLogout)
	mux.HandleFunc("POST "+p+"/auth/refresh", h.handleRefresh)
}

// loginRequest is the expected JSON body for POST /api/v1/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the expected JSON body for POST /api/v1/auth/refresh.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "email and password are required")
		return
	}

	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	// Load user and verify password.
	user, passwordHash, err := h.loadUser(r.Context(), site, req.Email)
	if err != nil {
		h.logger.Debug("login: user load failed",
			slog.String("email", req.Email),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid credentials")
		return
	}

	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); bcryptErr != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid credentials")
		return
	}

	// Issue token pair.
	pair, err := auth.IssueTokenPair(h.jwtCfg, user, site.Name)
	if err != nil {
		h.logger.Error("login: issue tokens failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to issue tokens")
		return
	}

	// Store refresh token jti for single-use enforcement.
	refreshClaims, err := auth.ValidateRefreshToken(h.jwtCfg, pair.RefreshToken)
	if err != nil {
		h.logger.Error("login: parse refresh token failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process tokens")
		return
	}
	err = h.sessions.StoreRefreshTokenID(r.Context(), refreshClaims.ID, h.jwtCfg.RefreshTokenTTL)
	if err != nil {
		h.logger.Error("login: store refresh jti failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to store token state")
		return
	}

	// Create session for cookie-based auth.
	sid, err := h.sessions.Create(r.Context(), user, site.Name)
	if err != nil {
		h.logger.Error("login: create session failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create session")
		return
	}

	// Set session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "moca_sid",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})

	h.logger.Info("login successful",
		slog.String("user", user.Email),
		slog.String("site", site.Name),
	)

	writeSuccess(w, http.StatusOK, pair)
}

func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Destroy session if cookie present.
	if cookie, err := r.Cookie("moca_sid"); err == nil && cookie.Value != "" {
		if err := h.sessions.Destroy(r.Context(), cookie.Value); err != nil {
			h.logger.Warn("logout: session destroy failed",
				slog.String("error", err.Error()),
			)
		}
	}

	// Clear cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "moca_sid",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	writeSuccess(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "refresh_token is required")
		return
	}

	// Validate refresh token.
	claims, err := auth.ValidateRefreshToken(h.jwtCfg, req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid or expired refresh token")
		return
	}

	// Check for replay (jti already consumed).
	used, err := h.sessions.IsRefreshTokenUsed(r.Context(), claims.ID)
	if err != nil {
		h.logger.Error("refresh: check jti failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to validate token state")
		return
	}
	if used {
		h.logger.Warn("refresh token replay detected",
			slog.String("email", claims.Email),
			slog.String("jti", claims.ID),
		)
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "refresh token already used")
		return
	}

	// Revoke old refresh token.
	err = h.sessions.RevokeRefreshToken(r.Context(), claims.ID)
	if err != nil {
		h.logger.Error("refresh: revoke jti failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to revoke old token")
		return
	}

	// Load fresh user data (to pick up role changes).
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return
	}

	user, _, err := h.loadUser(r.Context(), site, claims.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "user not found or disabled")
		return
	}

	// Issue new token pair.
	pair, err := auth.IssueTokenPair(h.jwtCfg, user, site.Name)
	if err != nil {
		h.logger.Error("refresh: issue tokens failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to issue tokens")
		return
	}

	// Store new refresh token jti.
	newClaims, err := auth.ValidateRefreshToken(h.jwtCfg, pair.RefreshToken)
	if err != nil {
		h.logger.Error("refresh: parse new refresh token failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process new tokens")
		return
	}
	if err := h.sessions.StoreRefreshTokenID(r.Context(), newClaims.ID, h.jwtCfg.RefreshTokenTTL); err != nil {
		h.logger.Error("refresh: store new jti failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to store new token state")
		return
	}

	writeSuccess(w, http.StatusOK, pair)
}
