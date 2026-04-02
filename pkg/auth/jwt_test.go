package auth

import (
	"testing"
	"time"
)

func testJWTConfig() JWTConfig {
	return JWTConfig{
		Secret:          "test-secret-key-for-jwt-signing-32b",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "moca-test",
	}
}

func TestIssueTokenPair_Success(t *testing.T) {
	cfg := testJWTConfig()
	user := &User{
		Email:    "admin@example.com",
		FullName: "Admin User",
		Roles:    []string{"Administrator", "Sales User"},
	}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if pair.RefreshToken == "" {
		t.Error("expected non-empty refresh token")
	}
	if pair.ExpiresIn != int64(cfg.AccessTokenTTL.Seconds()) {
		t.Errorf("ExpiresIn = %d, want %d", pair.ExpiresIn, int64(cfg.AccessTokenTTL.Seconds()))
	}
}

func TestValidateAccessToken_RoundTrip(t *testing.T) {
	cfg := testJWTConfig()
	user := &User{
		Email:    "admin@example.com",
		FullName: "Admin User",
		Roles:    []string{"Administrator", "Sales User"},
	}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	claims, err := ValidateAccessToken(cfg, pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	if claims.Email != user.Email {
		t.Errorf("Email = %q, want %q", claims.Email, user.Email)
	}
	if claims.FullName != user.FullName {
		t.Errorf("FullName = %q, want %q", claims.FullName, user.FullName)
	}
	if len(claims.Roles) != len(user.Roles) {
		t.Fatalf("Roles len = %d, want %d", len(claims.Roles), len(user.Roles))
	}
	for i, r := range claims.Roles {
		if r != user.Roles[i] {
			t.Errorf("Roles[%d] = %q, want %q", i, r, user.Roles[i])
		}
	}
	if claims.Site != "test-site" {
		t.Errorf("Site = %q, want %q", claims.Site, "test-site")
	}
	if claims.Issuer != cfg.Issuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, cfg.Issuer)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	cfg := testJWTConfig()
	cfg.AccessTokenTTL = -1 * time.Minute // already expired
	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	_, err = ValidateAccessToken(cfg, pair.AccessToken)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateAccessToken_BadSignature(t *testing.T) {
	cfg := testJWTConfig()
	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	// Validate with a different secret.
	wrongCfg := cfg
	wrongCfg.Secret = "wrong-secret-key-different-from-orig"

	_, err = ValidateAccessToken(wrongCfg, pair.AccessToken)
	if err == nil {
		t.Fatal("expected error for bad signature, got nil")
	}
}

func TestValidateRefreshToken_RoundTrip(t *testing.T) {
	cfg := testJWTConfig()
	user := &User{Email: "admin@example.com", FullName: "Admin", Roles: []string{"Admin"}}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	claims, err := ValidateRefreshToken(cfg, pair.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}

	if claims.Email != user.Email {
		t.Errorf("Email = %q, want %q", claims.Email, user.Email)
	}
	if claims.Site != "test-site" {
		t.Errorf("Site = %q, want %q", claims.Site, "test-site")
	}
	if claims.ID == "" {
		t.Error("expected non-empty jti in refresh token")
	}
}

func TestValidateRefreshToken_Expired(t *testing.T) {
	cfg := testJWTConfig()
	cfg.RefreshTokenTTL = -1 * time.Minute
	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}

	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	_, err = ValidateRefreshToken(cfg, pair.RefreshToken)
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
}

func TestDefaultJWTConfig_ReadsEnv(t *testing.T) {
	t.Setenv("MOCA_JWT_SECRET", "my-env-secret")

	cfg := DefaultJWTConfig()
	if cfg.Secret != "my-env-secret" {
		t.Errorf("Secret = %q, want %q", cfg.Secret, "my-env-secret")
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %v, want %v", cfg.AccessTokenTTL, 15*time.Minute)
	}
	if cfg.RefreshTokenTTL != 7*24*time.Hour {
		t.Errorf("RefreshTokenTTL = %v, want %v", cfg.RefreshTokenTTL, 7*24*time.Hour)
	}
	if cfg.Issuer != "moca" {
		t.Errorf("Issuer = %q, want %q", cfg.Issuer, "moca")
	}
}

func TestIssueTokenPair_UniqueJTI(t *testing.T) {
	cfg := testJWTConfig()
	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}

	pair1, err := IssueTokenPair(cfg, user, "site1")
	if err != nil {
		t.Fatalf("first IssueTokenPair: %v", err)
	}
	pair2, err := IssueTokenPair(cfg, user, "site1")
	if err != nil {
		t.Fatalf("second IssueTokenPair: %v", err)
	}

	claims1, _ := ValidateRefreshToken(cfg, pair1.RefreshToken)
	claims2, _ := ValidateRefreshToken(cfg, pair2.RefreshToken)

	if claims1.ID == claims2.ID {
		t.Error("expected different jti values for different token pairs")
	}
}
