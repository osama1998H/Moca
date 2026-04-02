package meta

import (
	"fmt"
	"strings"
	"unicode"
)

// GenerateRLSPolicies generates DDL statements to enable Row-Level Security
// on the table for the given MetaType and create policies from PermRule
// definitions that have match_field/match_value set.
//
// Returns nil for virtual, single, or child MetaTypes (no RLS needed).
//
// The generated statements include:
//  1. ALTER TABLE ... ENABLE ROW LEVEL SECURITY
//  2. ALTER TABLE ... FORCE ROW LEVEL SECURITY (needed because the DB owner bypasses RLS by default)
//  3. An admin bypass policy allowing Administrator users full access
//  4. Per-condition match policies using current_setting() session variables
//
// Policies use FOR ALL (covers SELECT, UPDATE, DELETE). INSERT does not check
// the USING clause in PostgreSQL so no restriction is needed there.
//
// Identical (match_field, match_value) pairs across different roles are
// deduplicated into a single policy.
func GenerateRLSPolicies(mt *MetaType) []DDLStatement {
	if mt.IsVirtual || mt.IsSingle || mt.IsChildTable {
		return nil
	}

	tableName := TableName(mt.Name)
	quotedTable := sanitizeIdent(tableName)

	var stmts []DDLStatement

	// 1. Enable RLS.
	stmts = append(stmts, DDLStatement{
		SQL:     fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", quotedTable),
		Comment: fmt.Sprintf("enable RLS on %s", tableName),
	})

	// 2. Force RLS (applies even to table owner).
	stmts = append(stmts, DDLStatement{
		SQL:     fmt.Sprintf("ALTER TABLE %s FORCE ROW LEVEL SECURITY", quotedTable),
		Comment: fmt.Sprintf("force RLS on %s (owner bypass prevention)", tableName),
	})

	// 3. Admin bypass policy.
	adminPolicyName := rlsPolicyName(tableName, "admin")
	stmts = append(stmts, DDLStatement{
		SQL: fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
			sanitizeIdent(adminPolicyName), quotedTable),
		Comment: fmt.Sprintf("drop existing admin bypass policy on %s", tableName),
	})
	stmts = append(stmts, DDLStatement{
		SQL: fmt.Sprintf(
			"CREATE POLICY %s ON %s FOR ALL USING (current_setting('moca.is_admin', true) = 'true')",
			sanitizeIdent(adminPolicyName), quotedTable),
		Comment: fmt.Sprintf("create admin bypass policy on %s", tableName),
	})

	// 4. Per-condition match policies (deduplicated by match_field + match_value).
	type matchKey struct {
		field string
		value string
	}
	seen := make(map[matchKey]bool)
	idx := 0

	for _, rule := range mt.Permissions {
		if rule.MatchField == "" || rule.MatchValue == "" {
			continue
		}
		if rule.Role == "Administrator" {
			continue
		}

		mk := matchKey{field: rule.MatchField, value: rule.MatchValue}
		if seen[mk] {
			continue
		}
		seen[mk] = true

		suffix := fmt.Sprintf("match_%d", idx)
		policyName := rlsPolicyName(tableName, suffix)
		gucName := "moca.current_user_" + sanitizeGUCKey(rule.MatchValue)

		stmts = append(stmts, DDLStatement{
			SQL: fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
				sanitizeIdent(policyName), quotedTable),
			Comment: fmt.Sprintf("drop existing match policy %s on %s", policyName, tableName),
		})
		stmts = append(stmts, DDLStatement{
			SQL: fmt.Sprintf(
				"CREATE POLICY %s ON %s FOR ALL USING (%s = current_setting('%s', true))",
				sanitizeIdent(policyName), quotedTable,
				sanitizeIdent(rule.MatchField), gucName),
			Comment: fmt.Sprintf("create match policy on %s: %s = user.%s",
				tableName, rule.MatchField, rule.MatchValue),
		})

		idx++
	}

	return stmts
}

// GenerateDropRLSPolicies generates DDL statements to remove all Moca-managed
// RLS policies from the table and disable RLS. Used during migration rollback
// or when permissions change.
func GenerateDropRLSPolicies(mt *MetaType) []DDLStatement {
	if mt.IsVirtual || mt.IsSingle || mt.IsChildTable {
		return nil
	}

	tableName := TableName(mt.Name)
	quotedTable := sanitizeIdent(tableName)

	var stmts []DDLStatement

	// Drop admin bypass policy.
	adminPolicyName := rlsPolicyName(tableName, "admin")
	stmts = append(stmts, DDLStatement{
		SQL: fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
			sanitizeIdent(adminPolicyName), quotedTable),
		Comment: fmt.Sprintf("drop admin bypass policy on %s", tableName),
	})

	// Drop match policies.
	type matchKey struct {
		field string
		value string
	}
	seen := make(map[matchKey]bool)
	idx := 0

	for _, rule := range mt.Permissions {
		if rule.MatchField == "" || rule.MatchValue == "" {
			continue
		}
		if rule.Role == "Administrator" {
			continue
		}

		mk := matchKey{field: rule.MatchField, value: rule.MatchValue}
		if seen[mk] {
			continue
		}
		seen[mk] = true

		suffix := fmt.Sprintf("match_%d", idx)
		policyName := rlsPolicyName(tableName, suffix)

		stmts = append(stmts, DDLStatement{
			SQL: fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s",
				sanitizeIdent(policyName), quotedTable),
			Comment: fmt.Sprintf("drop match policy %s on %s", policyName, tableName),
		})
		idx++
	}

	// Disable RLS.
	stmts = append(stmts, DDLStatement{
		SQL:     fmt.Sprintf("ALTER TABLE %s DISABLE ROW LEVEL SECURITY", quotedTable),
		Comment: fmt.Sprintf("disable RLS on %s", tableName),
	})

	return stmts
}

// rlsPolicyName returns a deterministic policy name: moca_{tableName}_{suffix}.
func rlsPolicyName(tableName, suffix string) string {
	return fmt.Sprintf("moca_%s_%s", tableName, suffix)
}

// roleToSnake converts a role name to a snake_case string suitable for use in
// PostgreSQL identifiers. Spaces become underscores; non-alphanumeric characters
// (except underscores) are dropped; the result is lowercased.
func roleToSnake(role string) string {
	var b strings.Builder
	for _, r := range role {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	return b.String()
}

// sanitizeGUCKey strips non-alphanumeric characters (except underscores) from
// a key name for use in PostgreSQL GUC variable names like
// moca.current_user_{key}. The result is lowercased.
func sanitizeGUCKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '_':
			b.WriteRune('_')
		}
	}
	return b.String()
}
