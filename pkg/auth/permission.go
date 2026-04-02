package auth

import "github.com/moca-framework/moca/pkg/meta"

// Perm is a bitmask type for doctype-level permissions.
// Values are stable and must not be reordered once released.
type Perm int

const (
	PermRead   Perm = 1  // 0x01
	PermWrite  Perm = 2  // 0x02
	PermCreate Perm = 4  // 0x04
	PermDelete Perm = 8  // 0x08
	PermSubmit Perm = 16 // 0x10
	PermCancel Perm = 32 // 0x20
	PermAmend  Perm = 64 // 0x40
)

// permNames maps permission name strings to their bitmask values.
var permNames = map[string]Perm{
	"read":   PermRead,
	"write":  PermWrite,
	"create": PermCreate,
	"delete": PermDelete,
	"submit": PermSubmit,
	"cancel": PermCancel,
	"amend":  PermAmend,
}

// PermFromString converts a permission name to its bitmask value.
// Returns the Perm value and true if the name is valid, or 0 and false otherwise.
func PermFromString(s string) (Perm, bool) {
	p, ok := permNames[s]
	return p, ok
}

// MatchCondition represents a row-level permission filter.
// Field is the document field to match, Value is the user attribute key
// whose value must equal the document's field value.
type MatchCondition struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// EffectivePerms holds the merged permission set for a user on a specific doctype.
// It is produced by ResolvePermissions and cached in Redis.
type EffectivePerms struct {
	// FieldLevelRead is the union of readable fields across matching roles.
	// nil/empty means unrestricted (all fields readable).
	FieldLevelRead []string `json:"field_level_read,omitempty"`
	// FieldLevelWrite is the union of writable fields across matching roles.
	// nil/empty means unrestricted (all fields writable).
	FieldLevelWrite []string `json:"field_level_write,omitempty"`
	// MatchConditions collects row-level match_field/match_value pairs from matching roles.
	MatchConditions []MatchCondition `json:"match_conditions,omitempty"`
	// CustomRules lists the names of custom rules to evaluate (deduplicated).
	CustomRules []string `json:"custom_rules,omitempty"`
	// DocTypePerm is the OR-ed bitmask of all matching PermRules.
	DocTypePerm Perm `json:"doctype_perm"`
}

// HasPerm checks whether the effective permissions include the named permission.
// Returns false for unrecognized permission names.
func (ep *EffectivePerms) HasPerm(perm string) bool {
	p, ok := permNames[perm]
	if !ok {
		return false
	}
	return ep.DocTypePerm&p != 0
}

// ResolvePermissions evaluates all PermRules against the user's roles and
// produces a merged EffectivePerms. This is a pure function with no I/O.
//
// Role merging uses union semantics: bitmasks are OR-ed, field lists are
// unioned, match conditions are collected, and custom rules are deduplicated.
func ResolvePermissions(rules []meta.PermRule, userRoles []string) *EffectivePerms {
	roleSet := make(map[string]struct{}, len(userRoles))
	for _, r := range userRoles {
		roleSet[r] = struct{}{}
	}

	ep := &EffectivePerms{}

	var (
		readSet  map[string]struct{}
		writeSet map[string]struct{}
		rulesSeen map[string]struct{}
	)

	for i := range rules {
		rule := &rules[i]
		if _, ok := roleSet[rule.Role]; !ok {
			continue
		}

		// OR bitmasks.
		ep.DocTypePerm |= Perm(rule.DocTypePerm)

		// Union field-level read.
		if len(rule.FieldLevelRead) > 0 {
			if readSet == nil {
				readSet = make(map[string]struct{})
			}
			for _, f := range rule.FieldLevelRead {
				readSet[f] = struct{}{}
			}
		}

		// Union field-level write.
		if len(rule.FieldLevelWrite) > 0 {
			if writeSet == nil {
				writeSet = make(map[string]struct{})
			}
			for _, f := range rule.FieldLevelWrite {
				writeSet[f] = struct{}{}
			}
		}

		// Collect match conditions.
		if rule.MatchField != "" && rule.MatchValue != "" {
			ep.MatchConditions = append(ep.MatchConditions, MatchCondition{
				Field: rule.MatchField,
				Value: rule.MatchValue,
			})
		}

		// Collect custom rules (deduplicated).
		if rule.CustomRule != "" {
			if rulesSeen == nil {
				rulesSeen = make(map[string]struct{})
			}
			if _, seen := rulesSeen[rule.CustomRule]; !seen {
				rulesSeen[rule.CustomRule] = struct{}{}
				ep.CustomRules = append(ep.CustomRules, rule.CustomRule)
			}
		}
	}

	// Convert sets to slices.
	if readSet != nil {
		ep.FieldLevelRead = make([]string, 0, len(readSet))
		for f := range readSet {
			ep.FieldLevelRead = append(ep.FieldLevelRead, f)
		}
	}
	if writeSet != nil {
		ep.FieldLevelWrite = make([]string, 0, len(writeSet))
		for f := range writeSet {
			ep.FieldLevelWrite = append(ep.FieldLevelWrite, f)
		}
	}

	return ep
}
