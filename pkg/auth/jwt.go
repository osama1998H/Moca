package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig holds JWT signing configuration.
type JWTConfig struct {
	Secret          string
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// TokenPair holds an access/refresh token pair returned after login or refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// AccessClaims are the JWT claims embedded in access tokens.
type AccessClaims struct {
	jwt.RegisteredClaims
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Site     string   `json:"site"`
	Roles    []string `json:"roles"`
}

// RefreshClaims are the JWT claims embedded in refresh tokens.
// The ID (jti) field is used for single-use enforcement via Redis.
type RefreshClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Site  string `json:"site"`
}

// DefaultJWTConfig returns a JWTConfig with values from environment variables
// and sensible defaults. The MOCA_JWT_SECRET environment variable must be set
// in production; an empty secret is allowed only for development/testing.
func DefaultJWTConfig() JWTConfig {
	secret := os.Getenv("MOCA_JWT_SECRET")
	return JWTConfig{
		Secret:          secret,
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "moca",
	}
}

// IssueTokenPair creates a signed access/refresh token pair for the given user
// and site. The access token embeds the full user identity (email, roles) so
// that Bearer-authenticated requests require no database round-trip. The refresh
// token includes a unique jti for single-use enforcement.
func IssueTokenPair(cfg JWTConfig, user *User, site string) (*TokenPair, error) {
	now := time.Now()

	// Access token.
	accessClaims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   user.Email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessTokenTTL)),
		},
		Email:    user.Email,
		FullName: user.FullName,
		Roles:    user.Roles,
		Site:     site,
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("auth: sign access token: %w", err)
	}

	// Refresh token with unique jti.
	jti, err := generateJTI()
	if err != nil {
		return nil, fmt.Errorf("auth: generate jti: %w", err)
	}
	refreshClaims := RefreshClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   user.Email,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.RefreshTokenTTL)),
		},
		Email: user.Email,
		Site:  site,
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("auth: sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int64(cfg.AccessTokenTTL.Seconds()),
	}, nil
}

// ValidateAccessToken parses and validates an access token string.
// Returns the embedded claims on success.
func ValidateAccessToken(cfg JWTConfig, tokenStr string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &AccessClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(cfg.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: validate access token: %w", err)
	}
	claims, ok := token.Claims.(*AccessClaims)
	if !ok {
		return nil, fmt.Errorf("auth: unexpected claims type")
	}
	return claims, nil
}

// ValidateRefreshToken parses and validates a refresh token string.
// Returns the embedded claims on success. The caller must additionally check
// the jti against Redis for single-use enforcement.
func ValidateRefreshToken(cfg JWTConfig, tokenStr string) (*RefreshClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &RefreshClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(cfg.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: validate refresh token: %w", err)
	}
	claims, ok := token.Claims.(*RefreshClaims)
	if !ok {
		return nil, fmt.Errorf("auth: unexpected claims type")
	}
	return claims, nil
}

// generateJTI produces a cryptographically random 16-byte hex string for use
// as a JWT ID (jti) claim.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
