package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// SiteExtractor is a function that retrieves the SiteContext from a context.
// This avoids an import cycle between pkg/auth and pkg/api.
// In production, pass api.SiteFromContext.
type SiteExtractor func(ctx context.Context) *tenancy.SiteContext

// MocaAuthenticator implements the api.Authenticator interface.
// It checks credentials in order: Bearer JWT → moca_sid session cookie → Guest.
// Only malformed or expired Bearer tokens return errors (401). Missing
// credentials fall back to Guest to allow unauthenticated endpoints.
type MocaAuthenticator struct {
	sessions      *SessionManager
	userLoader    *UserLoader
	siteExtractor SiteExtractor
	logger        *slog.Logger
	jwtCfg        JWTConfig
}

// NewMocaAuthenticator creates an authenticator with the given dependencies.
func NewMocaAuthenticator(
	jwtCfg JWTConfig,
	sessions *SessionManager,
	userLoader *UserLoader,
	siteExtractor SiteExtractor,
	logger *slog.Logger,
) *MocaAuthenticator {
	if logger == nil {
		logger = slog.Default()
	}
	return &MocaAuthenticator{
		jwtCfg:        jwtCfg,
		sessions:      sessions,
		userLoader:    userLoader,
		siteExtractor: siteExtractor,
		logger:        logger,
	}
}

// Authenticate resolves a User from the HTTP request.
//
// Resolution order:
//  1. Authorization: Bearer <token> — validates JWT, returns User from claims (no DB hit).
//  2. moca_sid cookie — looks up session in Redis, returns User from session data.
//  3. Guest fallback — returns a Guest user with the "Guest" role.
//
// Only expired or malformed Bearer tokens return an error. Missing credentials
// produce a Guest user, allowing unauthenticated endpoints (like login) to work.
func (a *MocaAuthenticator) Authenticate(r *http.Request) (*User, error) {
	// 1. Check Bearer token.
	if token := extractBearerToken(r); token != "" {
		claims, err := ValidateAccessToken(a.jwtCfg, token)
		if err != nil {
			a.logger.Debug("bearer token validation failed",
				slog.String("error", err.Error()),
			)
			return nil, err
		}
		return &User{
			Email:        claims.Email,
			FullName:     claims.FullName,
			Roles:        claims.Roles,
			UserDefaults: claims.UserDefaults,
		}, nil
	}

	// 2. Check session cookie.
	if cookie, err := r.Cookie("moca_sid"); err == nil && cookie.Value != "" {
		sess, err := a.sessions.Get(r.Context(), cookie.Value)
		if err == nil {
			return &User{
				Email:        sess.Email,
				FullName:     sess.FullName,
				Roles:        sess.Roles,
				UserDefaults: sess.UserDefaults,
			}, nil
		}
		a.logger.Debug("session lookup failed",
			slog.String("session_id", cookie.Value[:8]+"..."),
			slog.String("error", err.Error()),
		)
	}

	// 3. Guest fallback.
	return guestUser(), nil
}

// guestUser returns the default unauthenticated user.
func guestUser() *User {
	return &User{
		Email:    "Guest",
		FullName: "Guest",
		Roles:    []string{"Guest"},
	}
}

// extractBearerToken extracts the token from an "Authorization: Bearer <token>" header.
// Returns empty string if the header is missing or malformed.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
