package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// API key error types for distinguishing 401 vs 403 in the middleware.
var (
	ErrAPIKeyNotFound = errors.New("api key not found")
	ErrAPIKeySecret   = errors.New("invalid api key secret")
	ErrAPIKeyExpired  = errors.New("api key expired")
	ErrAPIKeyRevoked  = errors.New("api key revoked")
	ErrIPNotAllowed   = errors.New("ip not in allowlist")
	ErrNoSiteContext  = errors.New("no site context for api key validation")
)

// authCacheTTL is the Redis TTL for cached API key auth results.
const authCacheTTL = 60 * time.Second

// APIKey represents a stored API key (no secrets exposed).
type APIKey struct { //nolint:govet // field order matches JSON API contract
	KeyID       string                `json:"key_id"`
	Label       string                `json:"label"`
	UserID      string                `json:"user_id"`
	Scopes      []meta.APIScopePerm   `json:"scopes"`
	RateLimit   *meta.RateLimitConfig `json:"rate_limit,omitempty"`
	IPAllowlist []string              `json:"ip_allowlist"`
	ExpiresAt   *time.Time            `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time            `json:"last_used_at,omitempty"`
	IsActive    bool                  `json:"is_active"`
	CreatedAt   time.Time             `json:"created_at"`
	RevokedAt   *time.Time            `json:"revoked_at,omitempty"`
}

// APIKeyIdentity is the result of a successful API key authentication.
type APIKeyIdentity struct { //nolint:govet // field order is logical grouping
	User      *auth.User
	KeyID     string
	Scopes    []meta.APIScopePerm
	RateLimit *meta.RateLimitConfig
}

// APIKeyValidator abstracts API key authentication for the middleware.
// Using an interface (not concrete *APIKeyStore) for testability.
type APIKeyValidator interface {
	ValidateRequest(ctx context.Context, r *http.Request) (*APIKeyIdentity, error)
}

// APIKeyCreateOpts holds parameters for creating a new API key.
type APIKeyCreateOpts struct { //nolint:govet // field order matches logical grouping
	Label       string
	UserID      string
	Scopes      []meta.APIScopePerm
	RateLimit   *meta.RateLimitConfig
	IPAllowlist []string
	ExpiresAt   *time.Time
}

// APIKeyListFilter controls which keys are returned by List.
type APIKeyListFilter struct {
	UserID string // filter by associated user
	Status string // "active", "revoked", "expired", "all" (default: "active")
}

// apiKeyRow holds the raw database row for an API key.
type apiKeyRow struct { //nolint:govet // field order matches DB column order for scan readability
	KeyID          string
	SecretHash     string
	PrevSecretHash *string
	PrevExpiresAt  *time.Time
	Label          string
	UserID         string
	Scopes         json.RawMessage
	RateLimit      json.RawMessage
	IPAllowlist    json.RawMessage
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	IsActive       bool
	CreatedAt      time.Time
	RevokedAt      *time.Time
}

// cachedIdentity is the Redis-cached form of a validated API key.
type cachedIdentity struct { //nolint:govet // field order is logical grouping
	UserEmail    string                `json:"user_email"`
	UserFullName string                `json:"user_full_name"`
	UserRoles    []string              `json:"user_roles"`
	UserDefaults map[string]string     `json:"user_defaults,omitempty"`
	KeyID        string                `json:"key_id"`
	Scopes       []meta.APIScopePerm   `json:"scopes"`
	RateLimit    *meta.RateLimitConfig `json:"rate_limit,omitempty"`
}

// APIKeyStore manages API key CRUD and validation.
type APIKeyStore struct {
	loadUser auth.UserLoadFunc
	redis    *redis.Client
	logger   *slog.Logger
}

// NewAPIKeyStore creates an APIKeyStore with the given dependencies.
func NewAPIKeyStore(
	loadUser auth.UserLoadFunc,
	redisClient *redis.Client,
	logger *slog.Logger,
) *APIKeyStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeyStore{
		loadUser: loadUser,
		redis:    redisClient,
		logger:   logger,
	}
}

// ValidateRequest implements APIKeyValidator. It extracts the token from the
// request, validates it against the tenant database, and returns the identity.
func (s *APIKeyStore) ValidateRequest(ctx context.Context, r *http.Request) (*APIKeyIdentity, error) {
	site := SiteFromContext(ctx)
	if site == nil || site.Pool == nil {
		return nil, ErrNoSiteContext
	}

	keyID, secret := extractTokenAuth(r)
	if keyID == "" {
		return nil, ErrAPIKeyNotFound
	}

	return s.Validate(ctx, site, keyID, secret, clientIP(r))
}

// Validate authenticates an API key by key_id and secret, checking expiry,
// revocation, and IP allowlist. On success it returns the associated user identity.
func (s *APIKeyStore) Validate(
	ctx context.Context,
	site *tenancy.SiteContext,
	keyID, secret, remoteIP string,
) (*APIKeyIdentity, error) {
	// 1. Check Redis cache first (avoids bcrypt on every request).
	if identity, err := s.checkAuthCache(ctx, site.Name, keyID, secret); err == nil {
		// Cache hit — still need to check IP allowlist.
		if !matchIPAllowlist(remoteIP, identity.ipAllowlist) {
			return nil, ErrIPNotAllowed
		}
		s.touchLastUsedAsync(site.Pool, keyID)
		return identity.toAPIKeyIdentity(), nil
	}

	// 2. Load key from database.
	row, err := s.loadKeyRow(ctx, site.Pool, keyID)
	if err != nil {
		return nil, ErrAPIKeyNotFound
	}

	// 3. Check active / revoked.
	if !row.IsActive {
		return nil, ErrAPIKeyRevoked
	}

	// 4. Check expiry.
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
		return nil, ErrAPIKeyExpired
	}

	// 5. Verify secret via bcrypt (try current, then prev during grace period).
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(row.SecretHash), []byte(secret)); bcryptErr != nil {
		// Try previous secret if within grace period.
		if row.PrevSecretHash != nil && row.PrevExpiresAt != nil && row.PrevExpiresAt.After(time.Now()) {
			if err2 := bcrypt.CompareHashAndPassword([]byte(*row.PrevSecretHash), []byte(secret)); err2 != nil {
				return nil, ErrAPIKeySecret
			}
			s.logger.Warn("api key authenticated with previous secret during grace period",
				slog.String("key_id", keyID),
			)
		} else {
			return nil, ErrAPIKeySecret
		}
	}

	// 6. Check IP allowlist.
	var ipAllowlist []string
	if row.IPAllowlist != nil {
		_ = json.Unmarshal(row.IPAllowlist, &ipAllowlist)
	}
	if !matchIPAllowlist(remoteIP, ipAllowlist) {
		return nil, ErrIPNotAllowed
	}

	// 7. Parse scopes and rate limit.
	var scopes []meta.APIScopePerm
	if row.Scopes != nil {
		_ = json.Unmarshal(row.Scopes, &scopes)
	}
	var rateLimit *meta.RateLimitConfig
	if row.RateLimit != nil && string(row.RateLimit) != "null" {
		rateLimit = &meta.RateLimitConfig{}
		_ = json.Unmarshal(row.RateLimit, rateLimit)
	}

	// 8. Load the associated user.
	user, _, err := s.loadUser(ctx, site, row.UserID)
	if err != nil {
		return nil, fmt.Errorf("load user for api key %s: %w", keyID, err)
	}

	identity := &APIKeyIdentity{
		User:      user,
		KeyID:     keyID,
		Scopes:    scopes,
		RateLimit: rateLimit,
	}

	// 9. Cache the result in Redis.
	s.setAuthCache(ctx, site.Name, keyID, secret, &authCacheEntry{
		cachedIdentity: cachedIdentity{
			UserEmail:    user.Email,
			UserFullName: user.FullName,
			UserRoles:    user.Roles,
			UserDefaults: user.UserDefaults,
			KeyID:        keyID,
			Scopes:       scopes,
			RateLimit:    rateLimit,
		},
		ipAllowlist: ipAllowlist,
	})

	// 10. Update last_used_at asynchronously.
	s.touchLastUsedAsync(site.Pool, keyID)

	return identity, nil
}

// Create generates a new API key, hashes the secret with bcrypt, and stores it.
// Returns the "key_id:secret" pair — the secret is shown only at creation time.
func (s *APIKeyStore) Create(ctx context.Context, pool *pgxpool.Pool, schema string, opts APIKeyCreateOpts) (string, error) {
	keyID, err := generateKeyID()
	if err != nil {
		return "", fmt.Errorf("generate key id: %w", err)
	}

	secret, err := generateSecret()
	if err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash secret: %w", err)
	}

	scopesJSON, _ := json.Marshal(opts.Scopes)
	if opts.Scopes == nil {
		scopesJSON = []byte("[]")
	}

	var rateLimitJSON []byte
	if opts.RateLimit != nil {
		rateLimitJSON, _ = json.Marshal(opts.RateLimit)
	}

	ipAllowlistJSON, _ := json.Marshal(opts.IPAllowlist)
	if opts.IPAllowlist == nil {
		ipAllowlistJSON = []byte("[]")
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.tab_api_key (
			key_id, secret_hash, label, user_id, scopes,
			rate_limit, ip_allowlist, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		pgx.Identifier{schema}.Sanitize(),
	)

	_, err = pool.Exec(ctx, query,
		keyID, string(hash), opts.Label, opts.UserID, scopesJSON,
		rateLimitJSON, ipAllowlistJSON, opts.ExpiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}

	return keyID + ":" + secret, nil
}

// Revoke marks an API key as inactive and records the revocation time.
func (s *APIKeyStore) Revoke(ctx context.Context, pool *pgxpool.Pool, schema, keyID string) error {
	query := fmt.Sprintf(`
		UPDATE %s.tab_api_key
		SET is_active = false, revoked_at = NOW()
		WHERE key_id = $1`,
		pgx.Identifier{schema}.Sanitize(),
	)
	tag, err := pool.Exec(ctx, query, keyID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	s.invalidateAuthCache(ctx, keyID)
	return nil
}

// Rotate generates a new secret for an existing key. The old secret is preserved
// as prev_secret_hash with a grace period expiry. Returns the new "key_id:secret".
func (s *APIKeyStore) Rotate(ctx context.Context, pool *pgxpool.Pool, schema, keyID string, gracePeriod time.Duration) (string, error) {
	newSecret, err := generateSecret()
	if err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newSecret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash secret: %w", err)
	}

	var prevExpires *time.Time
	if gracePeriod > 0 {
		t := time.Now().Add(gracePeriod)
		prevExpires = &t
	}

	query := fmt.Sprintf(`
		UPDATE %s.tab_api_key
		SET prev_secret_hash = secret_hash,
			prev_expires_at = $2,
			secret_hash = $3
		WHERE key_id = $1 AND is_active = true`,
		pgx.Identifier{schema}.Sanitize(),
	)
	tag, err := pool.Exec(ctx, query, keyID, prevExpires, string(newHash))
	if err != nil {
		return "", fmt.Errorf("rotate api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrAPIKeyNotFound
	}

	s.invalidateAuthCache(ctx, keyID)
	return keyID + ":" + newSecret, nil
}

// List returns API keys matching the given filters. Secrets are never returned.
func (s *APIKeyStore) List(ctx context.Context, pool *pgxpool.Pool, schema string, filter APIKeyListFilter) ([]APIKey, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.UserID != "" {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, filter.UserID)
		argIdx++
	}

	switch filter.Status {
	case "revoked":
		conditions = append(conditions, "is_active = false AND revoked_at IS NOT NULL")
	case "expired":
		conditions = append(conditions, fmt.Sprintf("expires_at IS NOT NULL AND expires_at < $%d", argIdx))
		args = append(args, time.Now())
		argIdx++
	case "all":
		// no filter
	default: // "active"
		conditions = append(conditions, "is_active = true")
		conditions = append(conditions, fmt.Sprintf("(expires_at IS NULL OR expires_at > $%d)", argIdx))
		args = append(args, time.Now())
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT key_id, label, user_id, scopes, rate_limit, ip_allowlist,
			   expires_at, last_used_at, is_active, created_at, revoked_at
		FROM %s.tab_api_key %s
		ORDER BY created_at DESC`,
		pgx.Identifier{schema}.Sanitize(), where,
	)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var scopesJSON, rateLimitJSON, ipAllowlistJSON json.RawMessage
		if err := rows.Scan(
			&k.KeyID, &k.Label, &k.UserID, &scopesJSON, &rateLimitJSON, &ipAllowlistJSON,
			&k.ExpiresAt, &k.LastUsedAt, &k.IsActive, &k.CreatedAt, &k.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		if scopesJSON != nil {
			_ = json.Unmarshal(scopesJSON, &k.Scopes)
		}
		if rateLimitJSON != nil && string(rateLimitJSON) != "null" {
			k.RateLimit = &meta.RateLimitConfig{}
			_ = json.Unmarshal(rateLimitJSON, k.RateLimit)
		}
		if ipAllowlistJSON != nil {
			_ = json.Unmarshal(ipAllowlistJSON, &k.IPAllowlist)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Get returns a single API key by key_id. The secret hash is never returned.
func (s *APIKeyStore) Get(ctx context.Context, pool *pgxpool.Pool, schema, keyID string) (*APIKey, error) {
	query := fmt.Sprintf(`
		SELECT key_id, label, user_id, scopes, rate_limit, ip_allowlist,
			   expires_at, last_used_at, is_active, created_at, revoked_at
		FROM %s.tab_api_key
		WHERE key_id = $1`,
		pgx.Identifier{schema}.Sanitize(),
	)

	var k APIKey
	var scopesJSON, rateLimitJSON, ipAllowlistJSON json.RawMessage
	err := pool.QueryRow(ctx, query, keyID).Scan(
		&k.KeyID, &k.Label, &k.UserID, &scopesJSON, &rateLimitJSON, &ipAllowlistJSON,
		&k.ExpiresAt, &k.LastUsedAt, &k.IsActive, &k.CreatedAt, &k.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, fmt.Errorf("get api key: %w", err)
	}

	if scopesJSON != nil {
		_ = json.Unmarshal(scopesJSON, &k.Scopes)
	}
	if rateLimitJSON != nil && string(rateLimitJSON) != "null" {
		k.RateLimit = &meta.RateLimitConfig{}
		_ = json.Unmarshal(rateLimitJSON, k.RateLimit)
	}
	if ipAllowlistJSON != nil {
		_ = json.Unmarshal(ipAllowlistJSON, &k.IPAllowlist)
	}
	return &k, nil
}

// EnsureTable creates the tab_api_key table and indexes in the given schema.
// The operation is idempotent (IF NOT EXISTS).
func (s *APIKeyStore) EnsureTable(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	qs := pgx.Identifier{schema}.Sanitize()
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %[1]s.tab_api_key (
			key_id           TEXT PRIMARY KEY,
			secret_hash      TEXT NOT NULL,
			prev_secret_hash TEXT,
			prev_expires_at  TIMESTAMPTZ,
			label            TEXT NOT NULL,
			user_id          TEXT NOT NULL,
			scopes           JSONB NOT NULL DEFAULT '[]',
			rate_limit       JSONB,
			ip_allowlist     JSONB DEFAULT '[]',
			expires_at       TIMESTAMPTZ,
			last_used_at     TIMESTAMPTZ,
			is_active        BOOLEAN NOT NULL DEFAULT true,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			revoked_at       TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_api_key_user ON %[1]s.tab_api_key (user_id);
		CREATE INDEX IF NOT EXISTS idx_api_key_active ON %[1]s.tab_api_key (is_active) WHERE is_active = true;
	`, qs)

	_, err := pool.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("ensure tab_api_key: %w", err)
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// loadKeyRow fetches the raw API key row from the database.
func (s *APIKeyStore) loadKeyRow(ctx context.Context, pool *pgxpool.Pool, keyID string) (*apiKeyRow, error) {
	site := SiteFromContext(ctx)
	if site == nil {
		return nil, ErrNoSiteContext
	}
	schema := site.DBSchema

	query := fmt.Sprintf(`
		SELECT key_id, secret_hash, prev_secret_hash, prev_expires_at,
			   label, user_id, scopes, rate_limit, ip_allowlist,
			   expires_at, last_used_at, is_active, created_at, revoked_at
		FROM %s.tab_api_key
		WHERE key_id = $1`,
		pgx.Identifier{schema}.Sanitize(),
	)

	var row apiKeyRow
	err := pool.QueryRow(ctx, query, keyID).Scan(
		&row.KeyID, &row.SecretHash, &row.PrevSecretHash, &row.PrevExpiresAt,
		&row.Label, &row.UserID, &row.Scopes, &row.RateLimit, &row.IPAllowlist,
		&row.ExpiresAt, &row.LastUsedAt, &row.IsActive, &row.CreatedAt, &row.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// touchLastUsedAsync updates last_used_at in a fire-and-forget goroutine
// to avoid adding DB write latency to every request.
func (s *APIKeyStore) touchLastUsedAsync(pool *pgxpool.Pool, keyID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := pool.Exec(ctx, `UPDATE tab_api_key SET last_used_at = NOW() WHERE key_id = $1`, keyID)
		if err != nil {
			s.logger.Debug("failed to update api key last_used_at",
				slog.String("key_id", keyID),
				slog.String("error", err.Error()),
			)
		}
	}()
}

// ── Auth cache ────────────────────────────────────────────────────────────────

// authCacheEntry extends cachedIdentity with fields not serialized to Redis.
type authCacheEntry struct {
	cachedIdentity
	ipAllowlist []string
}

func (e *authCacheEntry) toAPIKeyIdentity() *APIKeyIdentity {
	return &APIKeyIdentity{
		User: &auth.User{
			Email:        e.UserEmail,
			FullName:     e.UserFullName,
			Roles:        e.UserRoles,
			UserDefaults: e.UserDefaults,
		},
		KeyID:     e.KeyID,
		Scopes:    e.Scopes,
		RateLimit: e.RateLimit,
	}
}

// authCacheKey builds the Redis key for a cached API key validation.
// Uses SHA256(secret) as discriminator — never stores plaintext or bcrypt hash.
func authCacheKey(site, keyID, secret string) string {
	h := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("apikey_auth:%s:%s:%s", site, keyID, hex.EncodeToString(h[:8]))
}

// authCacheValue is what gets stored in Redis — identity + IP allowlist.
type authCacheValue struct {
	cachedIdentity
	IPAllowlist []string `json:"ip_allowlist"`
}

func (s *APIKeyStore) checkAuthCache(ctx context.Context, site, keyID, secret string) (*authCacheEntry, error) {
	if s.redis == nil {
		return nil, errors.New("no redis")
	}
	key := authCacheKey(site, keyID, secret)
	data, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var val authCacheValue
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, err
	}
	return &authCacheEntry{
		cachedIdentity: val.cachedIdentity,
		ipAllowlist:    val.IPAllowlist,
	}, nil
}

func (s *APIKeyStore) setAuthCache(ctx context.Context, site, keyID, secret string, entry *authCacheEntry) {
	if s.redis == nil {
		return
	}
	val := authCacheValue{
		cachedIdentity: entry.cachedIdentity,
		IPAllowlist:    entry.ipAllowlist,
	}
	data, err := json.Marshal(val)
	if err != nil {
		return
	}
	key := authCacheKey(site, keyID, secret)
	_ = s.redis.Set(ctx, key, data, authCacheTTL).Err()
}

func (s *APIKeyStore) invalidateAuthCache(ctx context.Context, keyID string) {
	if s.redis == nil {
		return
	}
	// Scan for all cache keys matching this key ID and delete them.
	pattern := fmt.Sprintf("apikey_auth:*:%s:*", keyID)
	iter := s.redis.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		_ = s.redis.Del(ctx, iter.Val()).Err()
	}
}

// ── Token extraction and IP helpers ───────────────────────────────────────────

// extractTokenAuth parses the "Authorization: token KEY:SECRET" header.
// Returns empty strings if the header is missing, malformed, or not token-type.
func extractTokenAuth(r *http.Request) (keyID, secret string) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "token") {
		return "", ""
	}
	tokenValue := strings.TrimSpace(parts[1])
	sep := strings.IndexByte(tokenValue, ':')
	if sep < 1 || sep == len(tokenValue)-1 {
		return "", ""
	}
	return tokenValue[:sep], tokenValue[sep+1:]
}

// clientIP extracts the client IP address from the request. It checks
// X-Forwarded-For first (takes the first IP), then falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// RemoteAddr is "host:port" — strip the port.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// matchIPAllowlist checks whether the given IP is permitted by the CIDR allowlist.
// An empty or nil allowlist means all IPs are allowed.
func matchIPAllowlist(ip string, cidrs []string) bool {
	if len(cidrs) == 0 {
		return true
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	for _, cidr := range cidrs {
		// Support both CIDR notation and plain IPs.
		if !strings.Contains(cidr, "/") {
			if cidr == ip {
				return true
			}
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

// ── Key generation ────────────────────────────────────────────────────────────

// generateKeyID creates a key ID with a "moca_" prefix and 16 random hex bytes.
func generateKeyID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "moca_" + hex.EncodeToString(b), nil
}

// generateSecret creates a 32-byte random hex-encoded secret.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
