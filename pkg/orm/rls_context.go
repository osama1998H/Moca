package orm

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// SetUserSessionVars sets transaction-scoped PostgreSQL GUC variables for
// Row-Level Security policy evaluation. Uses SET LOCAL so the variables are
// automatically cleared when the transaction ends.
//
// Variables set:
//   - moca.user_email = email
//   - moca.is_admin = "true"/"false" (based on whether "Administrator" is in roles)
//   - moca.current_user_{key} = value (for each entry in defaults)
//
// This function must be called within an active transaction (tx). The variables
// are scoped to that transaction only and do not leak to other connections.
//
// Parameters use primitive types instead of *auth.User to avoid a circular
// import between orm and auth packages.
func SetUserSessionVars(ctx context.Context, tx pgx.Tx, email string, roles []string, defaults map[string]string) error {
	// Set current user.
	if _, err := tx.Exec(ctx,
		fmt.Sprintf("SET LOCAL moca.user_email = %s", QuoteLiteral(email)),
	); err != nil {
		return fmt.Errorf("set moca.user_email: %w", err)
	}

	// Set admin flag.
	isAdmin := "false"
	for _, r := range roles {
		if r == "Administrator" {
			isAdmin = "true"
			break
		}
	}
	if _, err := tx.Exec(ctx,
		fmt.Sprintf("SET LOCAL moca.is_admin = %s", QuoteLiteral(isAdmin)),
	); err != nil {
		return fmt.Errorf("set moca.is_admin: %w", err)
	}

	// Set user defaults.
	for key, val := range defaults {
		gucName := "moca.current_user_" + SanitizeGUCName(key)
		if _, err := tx.Exec(ctx,
			fmt.Sprintf("SET LOCAL %s = %s", gucName, QuoteLiteral(val)),
		); err != nil {
			return fmt.Errorf("set %s: %w", gucName, err)
		}
	}

	return nil
}

// QuoteLiteral escapes a string for safe inclusion as a PostgreSQL string
// literal. Single quotes are doubled and the result is wrapped in single quotes.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// SanitizeGUCName strips characters that are not valid in PostgreSQL GUC
// variable names. Only lowercase letters, digits, and underscores are kept.
func SanitizeGUCName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '_':
			b.WriteRune('_')
		}
	}
	return b.String()
}
