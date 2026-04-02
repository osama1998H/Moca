package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/moca-framework/moca/pkg/tenancy"
)

// UserLoadFunc loads a user by email from a tenant's database, returning the
// User with roles populated plus the bcrypt password hash. This interface
// allows test mocking without a real database.
type UserLoadFunc func(ctx context.Context, site *tenancy.SiteContext, email string) (*User, string, error)

// UserLoader loads user records directly from PostgreSQL for authentication.
// It bypasses DocManager to avoid the circular dependency: auth middleware
// needs a User, but DocManager.Get requires DocContext which needs a User.
type UserLoader struct {
	logger *slog.Logger
}

// NewUserLoader creates a UserLoader.
func NewUserLoader(logger *slog.Logger) *UserLoader {
	return &UserLoader{logger: logger}
}

// LoadByEmail loads a user by email from the tenant's database.
// Returns the User with roles populated, plus the bcrypt password hash.
// Returns ErrUserNotFound if the email does not exist or the user is disabled.
func (ul *UserLoader) LoadByEmail(ctx context.Context, site *tenancy.SiteContext, email string) (*User, string, error) {
	pool := site.Pool
	if pool == nil {
		return nil, "", fmt.Errorf("auth: site %q has no database pool", site.Name)
	}

	// Load user record.
	var fullName, passwordHash string
	var enabled bool
	err := pool.QueryRow(ctx,
		`SELECT "full_name", "password", "enabled" FROM "tabUser" WHERE "name" = $1`,
		email,
	).Scan(&fullName, &passwordHash, &enabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, "", ErrUserNotFound
		}
		return nil, "", fmt.Errorf("auth: load user %q: %w", email, err)
	}

	if !enabled {
		return nil, "", ErrUserNotFound
	}

	// Load roles from HasRole child table.
	rows, err := pool.Query(ctx,
		`SELECT "role" FROM "tabHasRole" WHERE "parent" = $1 AND "parenttype" = 'User'`,
		email,
	)
	if err != nil {
		return nil, "", fmt.Errorf("auth: load roles for %q: %w", email, err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, "", fmt.Errorf("auth: scan role for %q: %w", email, err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("auth: iterate roles for %q: %w", email, err)
	}

	user := &User{
		Email:    email,
		FullName: fullName,
		Roles:    roles,
	}

	return user, passwordHash, nil
}
