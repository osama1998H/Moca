package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// UserProvisioner handles finding existing users or auto-creating new ones
// during SSO authentication flows. It uses direct SQL (same pattern as
// UserLoader) to avoid DocManager dependency during unauthenticated flows.
type UserProvisioner struct {
	logger *slog.Logger
}

// NewUserProvisioner creates a UserProvisioner.
func NewUserProvisioner(logger *slog.Logger) *UserProvisioner {
	if logger == nil {
		logger = slog.Default()
	}
	return &UserProvisioner{logger: logger}
}

// FindOrCreate looks up a user by email. If the user exists and is enabled,
// it returns the User with roles loaded. If the user doesn't exist and
// autoCreate is true, it creates a new User with a random password and the
// given defaultRole, then returns it. If the user doesn't exist and autoCreate
// is false, it returns ErrUserNotFound.
func (up *UserProvisioner) FindOrCreate(
	ctx context.Context,
	site *tenancy.SiteContext,
	email, fullName string,
	autoCreate bool,
	defaultRole string,
) (*User, error) {
	pool := site.Pool
	if pool == nil {
		return nil, fmt.Errorf("auth: site %q has no database pool", site.Name)
	}

	// Try to load existing user.
	var existingFullName string
	var enabled bool
	err := pool.QueryRow(ctx,
		`SELECT "full_name", "enabled" FROM "tab_user" WHERE "name" = $1`,
		email,
	).Scan(&existingFullName, &enabled)

	if err == nil {
		// User exists.
		if !enabled {
			return nil, ErrUserDisabled
		}
		return up.loadUserWithRoles(ctx, site, email, existingFullName)
	}
	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("auth: check user %q: %w", email, err)
	}

	// User does not exist.
	if !autoCreate {
		return nil, ErrUserNotFound
	}

	// Auto-create user with a random password (never used — SSO users
	// authenticate via IdP only).
	randomPassword, err := generateRandomPassword()
	if err != nil {
		return nil, fmt.Errorf("auth: generate random password: %w", err)
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	now := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Insert user record.
	_, err = tx.Exec(ctx,
		`INSERT INTO "tab_user" ("name", "full_name", "password", "enabled", "owner", "creation", "modified", "modified_by")
		 VALUES ($1, $2, $3, true, 'SSO', $4, $4, 'SSO')`,
		email, fullName, string(hashedPassword), now,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: create user %q: %w", email, err)
	}

	// Insert default role if specified.
	var roles []string
	if defaultRole != "" {
		_, err = tx.Exec(ctx,
			`INSERT INTO "tab_has_role" ("name", "parent", "parenttype", "parentfield", "role", "owner", "creation", "modified", "modified_by")
			 VALUES ($1, $2, 'User', 'roles', $3, 'SSO', $4, $4, 'SSO')`,
			email+"_"+defaultRole, email, defaultRole, now,
		)
		if err != nil {
			return nil, fmt.Errorf("auth: assign role %q to %q: %w", defaultRole, email, err)
		}
		roles = []string{defaultRole}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("auth: commit user creation: %w", err)
	}

	up.logger.Info("SSO user auto-provisioned",
		slog.String("email", email),
		slog.String("site", site.Name),
		slog.String("role", defaultRole),
	)

	return &User{
		Email:    email,
		FullName: fullName,
		Roles:    roles,
	}, nil
}

// loadUserWithRoles loads the full role list for an existing user.
func (up *UserProvisioner) loadUserWithRoles(
	ctx context.Context,
	site *tenancy.SiteContext,
	email, fullName string,
) (*User, error) {
	rows, err := site.Pool.Query(ctx,
		`SELECT "role" FROM "tab_has_role" WHERE "parent" = $1 AND "parenttype" = 'User'`,
		email,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: load roles for %q: %w", email, err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("auth: scan role for %q: %w", email, err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterate roles for %q: %w", email, err)
	}

	return &User{
		Email:    email,
		FullName: fullName,
		Roles:    roles,
	}, nil
}

// generateRandomPassword produces a cryptographically random 32-byte hex string.
func generateRandomPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
