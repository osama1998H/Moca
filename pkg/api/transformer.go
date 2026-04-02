package api

import (
	"context"
	"fmt"

	"github.com/moca-framework/moca/pkg/meta"
)

// OperationType signals which REST operation is being processed,
// allowing transformers to vary behaviour (e.g. DefaultFields on list only).
type OperationType int

const (
	OpGet    OperationType = iota // single-document read
	OpList                        // list/paginated read
	OpCreate                      // document creation
	OpUpdate                      // document update
)

// Transformer transforms request or response payloads for a given MetaType.
// Implementations must be safe for concurrent use.
type Transformer interface {
	// TransformRequest modifies an inbound request body before it reaches the
	// CRUD layer. The returned map replaces the original body.
	TransformRequest(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error)

	// TransformResponse modifies an outbound response body before it is
	// serialised to JSON. The returned map replaces the original body.
	TransformResponse(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error)
}

// TransformerChain is an ordered slice of transformers applied sequentially.
// Request transformers run first-to-last; response transformers run first-to-last.
type TransformerChain []Transformer

// TransformRequest applies each transformer's request transform in order.
func (tc TransformerChain) TransformRequest(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error) {
	var err error
	for _, t := range tc {
		body, err = t.TransformRequest(ctx, mt, body)
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

// TransformResponse applies each transformer's response transform in order.
func (tc TransformerChain) TransformResponse(ctx context.Context, mt *meta.MetaType, body map[string]any) (map[string]any, error) {
	var err error
	for _, t := range tc {
		body, err = t.TransformResponse(ctx, mt, body)
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

// NewTransformerChain builds the standard transformer chain for a MetaType and
// optional API version. The chain order is:
//  1. ReadOnlyEnforcer — strip read-only fields from requests (no-op on response)
//  2. FieldFilter      — exclude non-API fields from responses (no-op on request)
//  3. AliasRemapper    — rename alias↔internal on request/response
//
// FieldFilter runs before AliasRemapper so it sees internal field names;
// AliasRemapper runs last so request aliases are resolved to internal names
// before reaching CRUD, and response internal names are aliased for the client.
func NewTransformerChain(mt *meta.MetaType, version *meta.APIVersion) TransformerChain {
	var versionMapping map[string]string
	var versionExclude []string
	if version != nil {
		versionMapping = version.FieldMapping
		versionExclude = version.ExcludeFields
	}

	return TransformerChain{
		NewReadOnlyEnforcer(mt),
		NewFieldFilter(mt, versionExclude),
		NewAliasRemapper(mt, versionMapping),
	}
}

// ── ReadOnlyEnforcer ───────────────────────────────────────────────────────

// ReadOnlyEnforcer strips fields marked ReadOnly or APIReadOnly from
// create and update request bodies. Response bodies pass through unchanged.
type ReadOnlyEnforcer struct {
	readOnly map[string]bool
}

// NewReadOnlyEnforcer builds a ReadOnlyEnforcer from the MetaType's field definitions.
func NewReadOnlyEnforcer(mt *meta.MetaType) *ReadOnlyEnforcer {
	ro := make(map[string]bool)
	for _, f := range mt.Fields {
		if f.ReadOnly || f.APIReadOnly {
			ro[f.Name] = true
		}
	}
	return &ReadOnlyEnforcer{readOnly: ro}
}

func (e *ReadOnlyEnforcer) TransformRequest(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	for k := range body {
		if e.readOnly[k] {
			delete(body, k)
		}
	}
	return body, nil
}

func (e *ReadOnlyEnforcer) TransformResponse(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	return body, nil // no-op
}

// ── AliasRemapper ──────────────────────────────────────────────────────────

// AliasRemapper performs bidirectional field name mapping between API-facing
// aliases and internal field names.
//
// On request:  alias key → internal key (so CRUD layer sees canonical names).
// On response: internal key → alias key (so the client sees API names).
type AliasRemapper struct {
	toInternal map[string]string // alias → internal
	toAlias    map[string]string // internal → alias
}

// NewAliasRemapper builds an AliasRemapper from FieldDef.APIAlias values,
// merged with optional version-specific field mappings.
// Version mappings take precedence over FieldDef aliases.
func NewAliasRemapper(mt *meta.MetaType, versionMapping map[string]string) *AliasRemapper {
	toInternal := make(map[string]string)
	toAlias := make(map[string]string)

	// Base aliases from field definitions.
	for _, f := range mt.Fields {
		if f.APIAlias != "" {
			toInternal[f.APIAlias] = f.Name
			toAlias[f.Name] = f.APIAlias
		}
	}

	// Version-specific mappings override base aliases.
	// FieldMapping keys are API-facing names, values are internal names.
	for apiName, internalName := range versionMapping {
		toInternal[apiName] = internalName
		toAlias[internalName] = apiName
	}

	return &AliasRemapper{toInternal: toInternal, toAlias: toAlias}
}

func (a *AliasRemapper) TransformRequest(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	if len(a.toInternal) == 0 {
		return body, nil
	}
	out := make(map[string]any, len(body))
	for k, v := range body {
		if internal, ok := a.toInternal[k]; ok {
			out[internal] = v
		} else {
			out[k] = v
		}
	}
	return out, nil
}

func (a *AliasRemapper) TransformResponse(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	if len(a.toAlias) == 0 {
		return body, nil
	}
	out := make(map[string]any, len(body))
	for k, v := range body {
		if alias, ok := a.toAlias[k]; ok {
			out[alias] = v
		} else {
			out[k] = v
		}
	}
	return out, nil
}

// ── FieldFilter ────────────────────────────────────────────────────────────

// FieldFilter removes non-API fields from responses and applies
// DefaultFields / AlwaysInclude rules. Request bodies pass through unchanged.
//
// Fields explicitly defined with InAPI==false are removed. Fields not defined
// in the MetaType schema (system fields like "name") pass through.
type FieldFilter struct {
	// notInAPI is the set of field names explicitly marked InAPI == false.
	notInAPI map[string]bool
	// exclude is the merged set of APIConfig.ExcludeFields and version-specific excludes.
	exclude map[string]bool
	// alwaysInclude are fields that must always be present in responses.
	alwaysInclude map[string]bool
	// defaultFields restricts list responses to this set (when non-empty).
	defaultFields map[string]bool
}

// NewFieldFilter builds a FieldFilter from the MetaType and optional
// version-specific exclude list.
func NewFieldFilter(mt *meta.MetaType, versionExclude []string) *FieldFilter {
	notInAPI := make(map[string]bool)
	for _, f := range mt.Fields {
		if !f.InAPI {
			notInAPI[f.Name] = true
		}
	}

	exclude := make(map[string]bool)
	if mt.APIConfig != nil {
		for _, name := range mt.APIConfig.ExcludeFields {
			exclude[name] = true
		}
	}
	for _, name := range versionExclude {
		exclude[name] = true
	}

	alwaysInclude := make(map[string]bool)
	if mt.APIConfig != nil {
		for _, name := range mt.APIConfig.AlwaysInclude {
			alwaysInclude[name] = true
		}
	}

	defaultFields := make(map[string]bool)
	if mt.APIConfig != nil {
		for _, name := range mt.APIConfig.DefaultFields {
			defaultFields[name] = true
		}
	}

	return &FieldFilter{
		notInAPI:      notInAPI,
		exclude:       exclude,
		alwaysInclude: alwaysInclude,
		defaultFields: defaultFields,
	}
}

func (f *FieldFilter) TransformRequest(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	return body, nil // no-op
}

func (f *FieldFilter) TransformResponse(ctx context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	op := OperationTypeFromContext(ctx)

	// For list operations with DefaultFields configured, restrict to that set.
	if op == OpList && len(f.defaultFields) > 0 {
		return f.applyDefaultFields(body), nil
	}

	// General filtering: remove fields explicitly marked InAPI=false and excluded fields.
	for k := range body {
		if f.alwaysInclude[k] {
			continue
		}
		if f.exclude[k] || f.notInAPI[k] {
			delete(body, k)
		}
	}
	return body, nil
}

// applyDefaultFields restricts the body to defaultFields ∪ alwaysInclude,
// while also applying the general notInAPI/exclude rules.
func (f *FieldFilter) applyDefaultFields(body map[string]any) map[string]any {
	out := make(map[string]any, len(f.defaultFields)+len(f.alwaysInclude))
	for k, v := range body {
		if f.alwaysInclude[k] || f.defaultFields[k] {
			if f.exclude[k] || f.notInAPI[k] {
				continue
			}
			out[k] = v
		}
	}
	return out
}

// ── buildTransformerChain helper for handlers ──────────────────────────────

// buildTransformerChain constructs the appropriate transformer chain for a
// request by resolving the API version from context against the MetaType's
// configured versions.
func buildTransformerChain(ctx context.Context, mt *meta.MetaType, extra ...Transformer) TransformerChain {
	versionStr := APIVersionFromContext(ctx)

	var version *meta.APIVersion
	if versionStr != "" && mt.APIConfig != nil {
		for i := range mt.APIConfig.Versions {
			if mt.APIConfig.Versions[i].Version == versionStr {
				version = &mt.APIConfig.Versions[i]
				break
			}
		}
	}

	chain := NewTransformerChain(mt, version)

	// Insert non-nil extras after ReadOnlyEnforcer (index 0), before FieldFilter.
	var nonNil []Transformer
	for _, e := range extra {
		if e != nil {
			nonNil = append(nonNil, e)
		}
	}
	if len(nonNil) == 0 {
		return chain
	}

	result := make(TransformerChain, 0, len(chain)+len(nonNil))
	result = append(result, chain[0]) // ReadOnlyEnforcer
	result = append(result, nonNil...)
	result = append(result, chain[1:]...) // FieldFilter, AliasRemapper
	return result
}

// filterMetaFields filters a buildMetaResponse result to only include fields
// visible through the given transformer chain's field filter rules.
func filterMetaFields(resp apiMetaResponse, mt *meta.MetaType, ctx context.Context) apiMetaResponse {
	versionStr := APIVersionFromContext(ctx)
	var version *meta.APIVersion
	if versionStr != "" && mt.APIConfig != nil {
		for i := range mt.APIConfig.Versions {
			if mt.APIConfig.Versions[i].Version == versionStr {
				version = &mt.APIConfig.Versions[i]
				break
			}
		}
	}

	var versionExclude []string
	if version != nil {
		versionExclude = version.ExcludeFields
	}
	ff := NewFieldFilter(mt, versionExclude)

	// Apply alias remapper to field names in meta response.
	var versionMapping map[string]string
	if version != nil {
		versionMapping = version.FieldMapping
	}
	ar := NewAliasRemapper(mt, versionMapping)

	filtered := make([]apiFieldDef, 0, len(resp.Fields))
	for _, fd := range resp.Fields {
		// Skip fields that would be excluded from API responses.
		if ff.exclude[fd.Name] || ff.notInAPI[fd.Name] {
			continue
		}
		// Apply alias to the name in meta response.
		if alias, ok := ar.toAlias[fd.Name]; ok {
			fd.Name = alias
			fd.APIAlias = "" // alias is now the primary name
		}
		filtered = append(filtered, fd)
	}
	resp.Fields = filtered
	return resp
}

// transformError creates a formatted error for transformer failures.
func transformError(phase string, err error) error {
	return fmt.Errorf("%s transform: %w", phase, err)
}
