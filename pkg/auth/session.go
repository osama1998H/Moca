package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// sessionKeyFmt is the Redis key format for sessions.
	// Pattern: session:{id}
	sessionKeyFmt = "session:%s"

	// refreshKeyFmt is the Redis key format for tracking refresh token jti.
	// Pattern: refresh:{jti}
	refreshKeyFmt = "refresh:%s"

	// defaultSessionTTL is the default session time-to-live.
	defaultSessionTTL = 24 * time.Hour
)

// Session represents stored session data in Redis.
type Session struct {
	UserDefaults map[string]string `json:"user_defaults,omitempty"`
	Email        string            `json:"email"`
	FullName     string            `json:"full_name"`
	Site         string            `json:"site"`
	Roles        []string          `json:"roles"`
	CreatedAt    int64             `json:"created_at"`
}

// SessionManager handles session CRUD using a Redis client (DB 2).
type SessionManager struct {
	client *redis.Client
	ttl    time.Duration
}

// NewSessionManager creates a SessionManager backed by the given Redis client.
// If ttl is zero, defaultSessionTTL (24h) is used.
func NewSessionManager(client *redis.Client, ttl time.Duration) *SessionManager {
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	return &SessionManager{client: client, ttl: ttl}
}

// Create stores a new session for the given user and site, returning a
// cryptographically random session ID.
func (sm *SessionManager) Create(ctx context.Context, user *User, site string) (string, error) {
	sid, err := generateSessionID()
	if err != nil {
		return "", fmt.Errorf("auth: generate session id: %w", err)
	}

	sess := Session{
		Email:        user.Email,
		FullName:     user.FullName,
		Roles:        user.Roles,
		UserDefaults: user.UserDefaults,
		Site:         site,
		CreatedAt:    time.Now().Unix(),
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return "", fmt.Errorf("auth: marshal session: %w", err)
	}

	key := fmt.Sprintf(sessionKeyFmt, sid)
	if err := sm.client.Set(ctx, key, data, sm.ttl).Err(); err != nil {
		return "", fmt.Errorf("auth: store session: %w", err)
	}

	return sid, nil
}

// Get retrieves a session by its ID. Returns ErrSessionNotFound if the session
// does not exist or has expired.
func (sm *SessionManager) Get(ctx context.Context, sessionID string) (*Session, error) {
	key := fmt.Sprintf(sessionKeyFmt, sessionID)
	data, err := sm.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: get session: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("auth: unmarshal session: %w", err)
	}
	return &sess, nil
}

// Destroy deletes a session by its ID. It is a no-op if the session does not exist.
func (sm *SessionManager) Destroy(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf(sessionKeyFmt, sessionID)
	if err := sm.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("auth: destroy session: %w", err)
	}
	return nil
}

// StoreRefreshTokenID records a refresh token's jti in Redis so that it can be
// validated as unused during token rotation. The TTL should match the refresh
// token's own TTL.
func (sm *SessionManager) StoreRefreshTokenID(ctx context.Context, jti string, ttl time.Duration) error {
	key := fmt.Sprintf(refreshKeyFmt, jti)
	if err := sm.client.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("auth: store refresh token id: %w", err)
	}
	return nil
}

// IsRefreshTokenUsed checks whether a refresh token jti has already been
// consumed. Returns true if the jti is NOT found in Redis (i.e., it was
// already revoked or never stored).
func (sm *SessionManager) IsRefreshTokenUsed(ctx context.Context, jti string) (bool, error) {
	key := fmt.Sprintf(refreshKeyFmt, jti)
	exists, err := sm.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("auth: check refresh token: %w", err)
	}
	return exists == 0, nil
}

// RevokeRefreshToken deletes the jti record, marking it as consumed.
func (sm *SessionManager) RevokeRefreshToken(ctx context.Context, jti string) error {
	key := fmt.Sprintf(refreshKeyFmt, jti)
	if err := sm.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	return nil
}

// generateSessionID produces a cryptographically random 32-byte hex string.
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
