package meta

import "time"

// PermRule defines a role-based permission rule for a MetaType.
// DocTypePerm is a bitmask encoding read, write, create, delete, submit, cancel, and amend.
// The permission engine evaluates these rules to determine access.
//
// Completed in MS-14 (Permission Engine).
type PermRule struct {
	Role            string   `json:"role"`
	MatchField      string   `json:"match_field,omitempty"`
	MatchValue      string   `json:"match_value,omitempty"`
	CustomRule      string   `json:"custom_rule,omitempty"`
	FieldLevelRead  []string `json:"field_level_read,omitempty"`
	FieldLevelWrite []string `json:"field_level_write,omitempty"`
	DocTypePerm     int      `json:"doctype_perm"`
}

// APIScopePerm controls what an API key is permitted to access,
// narrowing permissions to specific DocTypes, operations, and filter conditions.
//
// Completed in MS-14 (Permission Engine).
type APIScopePerm struct {
	Filters    map[string]any `json:"filters"`
	Scope      string         `json:"scope"`
	DocTypes   []string       `json:"doc_types"`
	Operations []string       `json:"operations"`
}

// WorkflowMeta defines a state-machine workflow attached to a MetaType.
// The workflow engine executes transitions, enforces guards, and tracks SLA deadlines.
//
// Completed in MS-09 (Workflow Engine).
type WorkflowMeta struct {
	Name        string          `json:"name"`
	DocType     string          `json:"doc_type"`
	States      []WorkflowState `json:"states"`
	Transitions []Transition    `json:"transitions"`
	SLARules    []SLARule       `json:"sla_rules,omitempty"`
	IsActive    bool            `json:"is_active"`
}

// WorkflowState represents a single state in a workflow state machine.
//
// Completed in MS-09 (Workflow Engine).
type WorkflowState struct {
	Name        string `json:"name"`
	Style       string `json:"style"`
	AllowEdit   string `json:"allow_edit"`
	UpdateField string `json:"update_field"`
	UpdateValue string `json:"update_value"`
	DocStatus   int    `json:"doc_status"`
}

// Transition represents a directed edge between two workflow states.
//
// Completed in MS-09 (Workflow Engine).
type Transition struct {
	From           string   `json:"from"`
	To             string   `json:"to"`
	Action         string   `json:"action"`
	Condition      string   `json:"condition"`
	AutoAction     string   `json:"auto_action"`
	AllowedRoles   []string `json:"allowed_roles"`
	RequireComment bool     `json:"require_comment"`
}

// SLARule defines a deadline and escalation policy for a workflow state.
//
// Completed in MS-09 (Workflow Engine).
type SLARule struct {
	State            string        `json:"state"`
	EscalationRole   string        `json:"escalation_role"`
	EscalationAction string        `json:"escalation_action"`
	MaxDuration      time.Duration `json:"max_duration"`
}

// DocHookDefs holds per-MetaType document lifecycle hook registrations.
// This is a placeholder; the full field set is defined in MS-07 (Hook Registry).
//
// Completed in MS-07 (Hook Registry).
type DocHookDefs struct{}

// ViewMeta holds UI rendering configuration for a MetaType including
// list views, form layouts, and dashboard widgets.
// This is a placeholder; the full field set is defined in MS-08 (React UI).
//
// Completed in MS-08 (React UI).
type ViewMeta struct{}

// LayoutHint provides UI layout guidance for a field within a form,
// such as column span, section placement, and conditional visibility.
// This is a placeholder; the full field set is defined in MS-08 (React UI).
//
// Completed in MS-08 (React UI).
type LayoutHint struct{}

// RateLimitConfig defines rate limiting parameters for API endpoints.
// Window is the sliding window duration, MaxRequests is the cap within
// that window, and BurstSize allows short bursts above MaxRequests.
//
// Completed in MS-06 (REST API Layer).
type RateLimitConfig struct {
	Window      time.Duration `json:"window"`       // sliding window size (e.g. 1*time.Minute)
	MaxRequests int           `json:"max_requests"` // maximum requests allowed per window
	BurstSize   int           `json:"burst_size"`   // burst allowance above MaxRequests
}

// APIConfig controls per-MetaType API behavior: endpoint exposure, rate limiting,
// pagination, response shaping, webhooks, and custom endpoints.
//
// Completed in MS-06 (REST API Layer).
type APIConfig struct {
	RateLimit       *RateLimitConfig `json:"rate_limit,omitempty"`
	BasePath        string           `json:"base_path"`
	AlwaysInclude   []string         `json:"always_include"`
	CustomEndpoints []CustomEndpoint `json:"custom_endpoints,omitempty"`
	Webhooks        []WebhookConfig  `json:"webhooks,omitempty"`
	Middleware      []string         `json:"middleware"`
	ComputedFields  []ComputedField  `json:"computed_fields"`
	ExcludeFields   []string         `json:"exclude_fields"`
	DefaultFields   []string         `json:"default_fields"`
	Versions        []APIVersion     `json:"versions"`
	DefaultPageSize int              `json:"default_page_size"`
	MaxPageSize     int              `json:"max_page_size"`
	AllowDelete     bool             `json:"allow_delete"`
	AllowCount      bool             `json:"allow_count"`
	AllowBulk       bool             `json:"allow_bulk"`
	Enabled         bool             `json:"enabled"`
	AllowList       bool             `json:"allow_list"`
	AllowUpdate     bool             `json:"allow_update"`
	AllowCreate     bool             `json:"allow_create"`
	AllowGet        bool             `json:"allow_get"`
}

// APIVersion defines version-specific API behavior and field mappings,
// enabling backwards-compatible evolution of the API surface.
//
// Completed in MS-06 (REST API Layer).
type APIVersion struct {
	Version       string            `json:"version"` // "v1", "v2"
	Status        string            `json:"status"`  // "active", "deprecated", "sunset"
	SunsetDate    *time.Time        `json:"sunset_date,omitempty"`
	FieldMapping  map[string]string `json:"field_mapping"` // v2 field name -> internal field
	ExcludeFields []string          `json:"exclude_fields"`
	AddedFields   []ComputedField   `json:"added_fields"`
}

// ComputedField defines a server-computed field that is added to API responses
// but not stored in the database.
//
// Completed in MS-06 (REST API Layer).
type ComputedField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Expression string `json:"expression"` // Go expression or registered function name
}

// CustomEndpoint defines a non-standard API route for a MetaType,
// backed by a registered handler function.
//
// Completed in MS-06 (REST API Layer).
type CustomEndpoint struct {
	RateLimit  *RateLimitConfig `json:"rate_limit,omitempty"`
	Method     string           `json:"method"`
	Path       string           `json:"path"`
	Handler    string           `json:"handler"`
	Middleware []string         `json:"middleware"`
}

// WebhookConfig defines an outgoing webhook triggered by document lifecycle events.
// The webhook is signed with Secret using HMAC-SHA256 for receiver verification.
//
// Completed in MS-06 (REST API Layer).
type WebhookConfig struct {
	Headers    map[string]string `json:"headers"`
	Filters    map[string]any    `json:"filters"`
	Event      string            `json:"event"`
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Secret     string            `json:"secret"`
	RetryCount int               `json:"retry_count"`
}
