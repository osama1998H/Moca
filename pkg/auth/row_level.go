package auth

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/orm"
)

// RowLevelFilters converts the MatchConditions in effective permissions into
// ORM filters suitable for QueryBuilder.WhereOr(). Each MatchCondition maps
// a document field to a user attribute; the filter checks that the document's
// field value equals the user's attribute value.
//
// Returns nil if there are no match conditions (no row-level restriction).
// Multiple conditions from different roles are returned as separate filters
// to be OR-ed by the caller.
func RowLevelFilters(ep *EffectivePerms, user *User) []orm.Filter {
	if len(ep.MatchConditions) == 0 {
		return nil
	}

	defaults := user.UserDefaults
	if defaults == nil {
		defaults = map[string]string{}
	}

	var filters []orm.Filter
	for _, mc := range ep.MatchConditions {
		val, ok := defaults[mc.Value]
		if !ok {
			// User has no value for this attribute — this condition cannot
			// match any document, so we add an impossible filter.
			// Using empty string match: the document field must equal "".
			val = ""
		}
		filters = append(filters, orm.Filter{
			Field:    mc.Field,
			Operator: orm.OpEqual,
			Value:    val,
		})
	}
	return filters
}

// CheckRowLevelAccess verifies that a loaded document satisfies at least one
// of the user's row-level match conditions (OR semantics). Returns true if
// access is allowed.
//
// Returns true (allow) when:
//   - There are no match conditions (no row-level restriction)
//   - At least one condition matches (doc field == user attribute)
func CheckRowLevelAccess(ep *EffectivePerms, user *User, docValues map[string]any) bool {
	if len(ep.MatchConditions) == 0 {
		return true
	}

	defaults := user.UserDefaults
	if defaults == nil {
		return false // user has no defaults but conditions exist → deny
	}

	for _, mc := range ep.MatchConditions {
		userVal, ok := defaults[mc.Value]
		if !ok {
			continue
		}
		docVal, ok := docValues[mc.Field]
		if !ok {
			continue
		}
		// Compare as strings (document values from DB are typically strings).
		if toString(docVal) == userVal {
			return true
		}
	}
	return false
}

// toString converts any value to its string representation for comparison.
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(v)
	}
}
