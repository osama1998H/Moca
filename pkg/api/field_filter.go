package api

import (
	"context"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/meta"
)

// systemFields are always included in responses and never rejected on writes,
// regardless of field-level permission restrictions.
var systemFields = map[string]bool{
	"name":      true,
	"creation":  true,
	"modified":  true,
	"owner":     true,
	"docstatus": true,
}

// FieldLevelTransformer enforces field-level permissions on API payloads.
//
// On responses: if EffectivePerms.FieldLevelRead is non-empty, only fields in
// that list (plus system fields) are returned. Empty/nil means unrestricted.
//
// On requests: if EffectivePerms.FieldLevelWrite is non-empty, any field not in
// the allowed set (and not a system field) is rejected with a PermissionDeniedError.
//
// Users with the "Administrator" role bypass all field-level filtering.
type FieldLevelTransformer struct {
	resolver *auth.CachedPermissionResolver
}

// NewFieldLevelTransformer creates a transformer backed by the given permission resolver.
func NewFieldLevelTransformer(resolver *auth.CachedPermissionResolver) *FieldLevelTransformer {
	return &FieldLevelTransformer{resolver: resolver}
}

// TransformResponse strips fields not in the user's FieldLevelRead set.
func (t *FieldLevelTransformer) TransformResponse(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error) {
	ep, err := t.resolvePerms(ctx, mt.Name)
	if err != nil || ep == nil {
		return body, nil
	}

	if len(ep.FieldLevelRead) == 0 {
		return body, nil
	}

	allowed := makeFieldSet(ep.FieldLevelRead)
	out := make(map[string]any, len(allowed)+len(systemFields))
	for k, v := range body {
		if systemFields[k] || allowed[k] {
			out[k] = v
		}
	}
	return out, nil
}

// TransformRequest rejects request bodies containing fields not in the user's
// FieldLevelWrite set. Returns a *PermissionDeniedError for the first
// unauthorized field encountered.
func (t *FieldLevelTransformer) TransformRequest(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error) {
	ep, err := t.resolvePerms(ctx, mt.Name)
	if err != nil || ep == nil {
		return body, nil
	}

	if len(ep.FieldLevelWrite) == 0 {
		return body, nil
	}

	allowed := makeFieldSet(ep.FieldLevelWrite)
	for k := range body {
		if systemFields[k] || allowed[k] {
			continue
		}
		return nil, &PermissionDeniedError{
			User:    UserFromContext(ctx).Email,
			Doctype: mt.Name,
			Perm:    "write field " + k,
		}
	}
	return body, nil
}

// resolvePerms returns the effective permissions for the current user/site/doctype.
// Returns nil perms (with no error) for Administrator users or when context is incomplete.
func (t *FieldLevelTransformer) resolvePerms(ctx context.Context, doctype string) (*auth.EffectivePerms, error) {
	user := UserFromContext(ctx)
	if user == nil || auth.IsAdministrator(user) {
		return nil, nil
	}

	site := SiteFromContext(ctx)
	if site == nil {
		return nil, nil
	}

	return t.resolver.Resolve(ctx, site.Name, user, doctype)
}

// makeFieldSet converts a slice of field names to a set for O(1) lookup.
func makeFieldSet(fields []string) map[string]bool {
	s := make(map[string]bool, len(fields))
	for _, f := range fields {
		s[f] = true
	}
	return s
}
