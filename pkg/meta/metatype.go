package meta

import "time"

// NamingRule determines how document names (primary keys) are generated
// when a new document is inserted.
type NamingRule string

// Naming rule constants. The naming engine (MS-04) uses PostgreSQL sequences
// for pattern-based naming to ensure uniqueness under concurrency.
const (
	NamingAutoIncrement NamingRule = "autoincrement"
	NamingByPattern     NamingRule = "pattern" // e.g., SO-0001, SO-0002
	NamingByField       NamingRule = "field"   // use a specific field value as the name
	NamingByHash        NamingRule = "hash"    // short hash derived from document content
	NamingUUID          NamingRule = "uuid"
	NamingCustom        NamingRule = "custom" // call a registered naming function
)

// NamingStrategy configures how document names are generated for a MetaType.
// Rule is required; Pattern, FieldName, and CustomFunc are rule-specific.
type NamingStrategy struct {
	Rule       NamingRule `json:"rule"`
	Pattern    string     `json:"pattern,omitempty"`     // for NamingByPattern, e.g. "SO-.####"
	FieldName  string     `json:"field_name,omitempty"`  // for NamingByField
	CustomFunc string     `json:"custom_func,omitempty"` // for NamingCustom: registered function name
}

// MetaType is the central metadata definition in MOCA. A single MetaType
// drives database schema generation, CRUD API routes, GraphQL schema,
// Meilisearch index configuration, and React form/list views.
//
// See MOCA_SYSTEM_DESIGN.md section 3.1.1 for the canonical definition.
type MetaType struct {
	Hooks         DocHookDefs    `json:"hooks"`
	ViewConfig    ViewMeta       `json:"view_config"`
	CreatedAt     time.Time      `json:"created_at"`
	ModifiedAt    time.Time      `json:"modified_at"`
	APIConfig     *APIConfig     `json:"api_config,omitempty"`
	Workflow      *WorkflowMeta  `json:"workflow,omitempty"`
	NamingRule    NamingStrategy `json:"naming_rule"`
	Name          string         `json:"name"`
	ImageField    string         `json:"image_field"`
	SortField     string         `json:"sort_field"`
	SortOrder     string         `json:"sort_order"`
	TitleField    string         `json:"title_field"`
	Description   string         `json:"description"`
	Label         string         `json:"label"`
	Module        string         `json:"module"`
	SearchFields  []string       `json:"search_fields"`
	Permissions   []PermRule     `json:"permissions"`
	Fields        []FieldDef          `json:"fields"`
	Layout        *LayoutTree         `json:"layout,omitempty"`
	FieldsMap     map[string]FieldDef `json:"-"`
	Version       int                 `json:"version"`
	IsSubmittable bool           `json:"is_submittable"`
	IsVirtual     bool           `json:"is_virtual"`
	IsChildTable  bool           `json:"is_child_table"`
	TrackChanges  bool           `json:"track_changes"`
	IsSingle      bool           `json:"is_single"`
	EventSourcing bool           `json:"event_sourcing"`
	CDCEnabled    bool           `json:"cdc_enabled"`
}
