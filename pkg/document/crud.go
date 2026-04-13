package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// ─── DynamicDoc internal helpers (same-package access) ───────────────────────

// resetDirtyState snapshots the current values map as the new clean baseline.
// Call this after a successful database write to clear the dirty-tracking state.
func (d *DynamicDoc) resetDirtyState() {
	d.original = deepCopyMap(d.values)
	// Also reset child dirty state.
	for _, rows := range d.children {
		for _, child := range rows {
			child.resetDirtyState()
		}
	}
}

// markPersisted transitions the document from the "new" state to the
// "persisted" state. Call this after a successful INSERT.
func (d *DynamicDoc) markPersisted() {
	d.isNew = false
	for _, rows := range d.children {
		for _, child := range rows {
			child.markPersisted()
		}
	}
}

// ─── Error types ─────────────────────────────────────────────────────────────

// DocNotFoundError is returned by Get when no document with the given name
// exists for the given doctype. Use errors.As to distinguish it from other
// database errors.
type DocNotFoundError struct {
	Doctype string
	Name    string
}

func (e *DocNotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Doctype, e.Name)
}

// ─── ListOptions ─────────────────────────────────────────────────────────────

// ListOptions configures a GetList query. All fields are optional.
type ListOptions struct {
	// OrderBy is the column to sort by. Defaults to "modified".
	// Must be a known field name or standard column to prevent injection.
	// Ignored when OrderByMulti is non-empty.
	OrderBy string

	// OrderDir is "ASC" or "DESC". Defaults to "DESC".
	// Ignored when OrderByMulti is non-empty.
	OrderDir string

	// Filters is a map of field name -> value for equality filters.
	// Each key is validated against the MetaType's known fields.
	// These are converted internally to orm.Filter with OpEqual.
	Filters map[string]any

	// AdvancedFilters provides operator-aware filters (e.g. >, IN, LIKE).
	// Applied in addition to equality Filters above.
	AdvancedFilters []orm.Filter

	// Fields specifies which columns to SELECT. When empty, all columns are returned.
	Fields []string

	// GroupBy specifies GROUP BY columns.
	GroupBy []string

	// OrderByMulti specifies multiple ORDER BY clauses.
	// Takes precedence over OrderBy/OrderDir when non-empty.
	OrderByMulti []orm.OrderClause

	// Limit is the maximum number of rows to return. Defaults to 20, capped at 100.
	Limit int

	// Offset is the number of rows to skip. Defaults to 0.
	Offset int
}

// ─── SQL builder helpers ──────────────────────────────────────────────────────

// buildDocColumns returns the ordered list of column names for a MetaType's
// document table. The order matches the CREATE TABLE column order:
//   - standard prefix columns (before _extra)
//   - user-defined storable, non-Table fields
//   - standard suffix columns (_extra and after)
func buildDocColumns(mt *meta.MetaType) []string {
	var stdCols []meta.StandardColumnDef
	if mt.IsChildTable {
		stdCols = meta.ChildStandardColumns()
	} else {
		stdCols = meta.StandardColumns()
	}

	// Split standard columns at the _extra insertion point.
	var before, after []meta.StandardColumnDef
	foundExtra := false
	for _, col := range stdCols {
		if col.Name == "_extra" {
			foundExtra = true
		}
		if !foundExtra {
			before = append(before, col)
		} else {
			after = append(after, col)
		}
	}

	var columns []string
	for _, col := range before {
		columns = append(columns, col.Name)
	}
	for _, f := range mt.Fields {
		if meta.ColumnType(f.FieldType) == "" {
			continue // skip Table, TableMultiSelect, and layout-only fields
		}
		columns = append(columns, f.Name)
	}
	for _, col := range after {
		columns = append(columns, col.Name)
	}
	return columns
}

// quoteIdents returns a slice of sanitized quoted identifiers for the given
// column names. Uses pgx.Identifier to prevent SQL injection.
func quoteIdents(columns []string) []string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = pgx.Identifier{col}.Sanitize()
	}
	return quoted
}

// buildInsertSQL returns a parameterized INSERT SQL statement and the ordered
// column list for the given MetaType. The columns list is used by
// extractValues to build the matching argument slice.
func buildInsertSQL(mt *meta.MetaType) (string, []string) {
	tableName := meta.TableName(mt.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()
	columns := buildDocColumns(mt)

	quotedCols := quoteIdents(columns)
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotedTable,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sql, columns
}

// buildUpdateSQL returns a parameterized UPDATE SQL statement and the ordered
// column list for the given set of fields to update. The WHERE clause always
// matches on name, appended as the last parameter.
//
// modifiedFields must not include "name" (the PK) or "creation" (immutable).
func buildUpdateSQL(mt *meta.MetaType, modifiedFields []string) (string, []string) {
	tableName := meta.TableName(mt.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()

	// Deduplicate and filter out immutable columns.
	seen := make(map[string]bool)
	var columns []string
	for _, f := range modifiedFields {
		if f == "name" || f == "creation" || seen[f] {
			continue
		}
		seen[f] = true
		columns = append(columns, f)
	}
	// Always include modified and modified_by.
	if !seen["modified"] {
		columns = append(columns, "modified")
	}
	if !seen["modified_by"] {
		columns = append(columns, "modified_by")
	}

	setClauses := make([]string, len(columns))
	for i, col := range columns {
		setClauses[i] = fmt.Sprintf("%s = $%d", pgx.Identifier{col}.Sanitize(), i+1)
	}

	sql := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d",
		quotedTable,
		strings.Join(setClauses, ", "),
		pgx.Identifier{"name"}.Sanitize(),
		len(columns)+1,
	)
	return sql, columns
}

// buildSelectSQL returns a SELECT SQL statement that fetches all columns of a
// document by name, along with the ordered column list for scanning.
func buildSelectSQL(mt *meta.MetaType) (string, []string) {
	tableName := meta.TableName(mt.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()
	columns := buildDocColumns(mt)
	quotedCols := quoteIdents(columns)

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
		strings.Join(quotedCols, ", "),
		quotedTable,
		pgx.Identifier{"name"}.Sanitize(),
	)
	return sql, columns
}

// extractValues returns the document's field values in the same order as
// columns, suitable for use as pgx query arguments. Nil values are replaced
// with typed defaults for NOT NULL columns.
func extractValues(doc *DynamicDoc, columns []string) []any {
	vals := make([]any, len(columns))
	for i, col := range columns {
		v := doc.values[col]
		// Supply typed defaults for NOT NULL columns to avoid pgx null violations.
		if v == nil {
			switch col {
			case "_extra":
				v = map[string]any{}
			case "docstatus":
				v = int16(0)
			case "idx":
				v = int32(0)
			case "owner", "modified_by":
				v = ""
			}
		}
		vals[i] = v
	}
	return vals
}

// normalizeDBValue converts pgx-specific types returned by rows.Values() into
// standard Go primitives so that doc.values contains clean, serialisable data.
func normalizeDBValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case float32:
		return float64(val)
	case []byte:
		// JSONB columns are returned as raw JSON bytes. Unmarshal to Go types.
		var m any
		if err := json.Unmarshal(val, &m); err == nil {
			return m
		}
		return string(val)
	case pgtype.Numeric:
		if !val.Valid || val.NaN {
			return nil
		}
		if val.Int == nil {
			return 0.0
		}
		f := new(big.Float).SetPrec(128).SetInt(val.Int)
		if val.Exp != 0 {
			expAbs := val.Exp
			if expAbs < 0 {
				expAbs = -expAbs
			}
			base := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(expAbs)), nil)
			expFloat := new(big.Float).SetPrec(128).SetInt(base)
			if val.Exp > 0 {
				f.Mul(f, expFloat)
			} else {
				f.Quo(f, expFloat)
			}
		}
		result, _ := f.Float64()
		return result
	default:
		return v
	}
}

// applyValues sets the top-level scalar fields on doc, and rebuilds child
// rows for any Table fields found in values. This handles the mixed payload
// format accepted by Insert and Update:
//
//	values = map[string]any{
//	    "customer": "Alice",          // scalar field
//	    "items":    []any{...},       // child table rows
//	}
func applyValues(doc *DynamicDoc, values map[string]any) error {
	for field, value := range values {
		// Table fields require special handling to rebuild doc.children.
		if _, isTable := doc.tableFields[field]; isTable {
			if value == nil {
				doc.children[field] = nil
				continue
			}
			rows, ok := value.([]any)
			if !ok {
				return fmt.Errorf("crud: field %q expects []any (child table rows) on doctype %q, got %T",
					field, doc.metaDef.Name, value)
			}
			childMeta, hasMeta := doc.childMetas[field]
			if !hasMeta {
				return fmt.Errorf("crud: no child MetaType for table field %q on doctype %q",
					field, doc.metaDef.Name)
			}
			children := make([]*DynamicDoc, 0, len(rows))
			for idx, rowAny := range rows {
				rowMap, ok := rowAny.(map[string]any)
				if !ok {
					return fmt.Errorf("crud: child row %d for field %q must be map[string]any on doctype %q",
						idx, field, doc.metaDef.Name)
				}
				child := NewDynamicDoc(childMeta, nil, true)
				child.values["idx"] = idx
				for k, val := range rowMap {
					if err := child.Set(k, val); err != nil {
						// Ignore unknown fields in child rows (caller may send extra keys).
						continue
					}
				}
				children = append(children, child)
			}
			doc.children[field] = children
			continue
		}
		if err := doc.Set(field, value); err != nil {
			// Unknown fields in the values map are silently ignored so callers
			// can pass a superset of fields without errors.
			continue
		}
	}
	return nil
}

// ─── PermResolver interface ──────────────────────────────────────────────────

// PermResolver resolves effective permissions for a user on a doctype.
// This interface decouples DocManager from the concrete auth.CachedPermissionResolver.
type PermResolver interface {
	Resolve(ctx context.Context, site string, user *auth.User, doctype string) (*auth.EffectivePerms, error)
}

// ─── DocManager ──────────────────────────────────────────────────────────────

// DocManager is the main entry point for all document CRUD operations. It
// wires together the MetaType registry, database connection manager, naming
// engine, validator, and controller registry to provide a complete document
// lifecycle.
//
// All public methods are safe for concurrent use.
// PostLoadTransformer transforms documents after loading from the database.
// Used for transparent field decryption of sensitive fields.
type PostLoadTransformer interface {
	TransformAfterLoad(ctx *DocContext, doc *DynamicDoc) error
}

type DocManager struct {
	registry            *meta.Registry
	db                  *orm.DBManager
	queryAdapter        orm.MetaProvider
	naming              *NamingEngine
	validator           *Validator
	controllers         *ControllerRegistry
	hookDispatcher      HookDispatcher      // nil = no hooks
	permResolver        PermResolver        // nil = no row-level filtering
	postLoadTransformer PostLoadTransformer // nil = no transform
	virtualSources      *VirtualSourceRegistry
	logger              *slog.Logger
}

// SetHookDispatcher configures an optional hook dispatcher that fires
// registered hooks after controller dispatch at each lifecycle point.
// Pass nil to disable hooks.
func (m *DocManager) SetHookDispatcher(d HookDispatcher) {
	m.hookDispatcher = d
}

// SetPostLoadTransformer configures an optional transformer that runs after
// documents are loaded from the database. Used for transparent field decryption.
// Pass nil to disable post-load transformation.
func (m *DocManager) SetPostLoadTransformer(t PostLoadTransformer) {
	m.postLoadTransformer = t
}

// SetPermResolver configures an optional permission resolver for row-level
// filtering. When set, CRUD operations enforce row-level match conditions.
// Pass nil to disable row-level filtering.
func (m *DocManager) SetPermResolver(r PermResolver) {
	m.permResolver = r
}

// SetVirtualSourceRegistry configures the registry for virtual doctypes.
func (m *DocManager) SetVirtualSourceRegistry(r *VirtualSourceRegistry) {
	m.virtualSources = r
}

// resolveRowLevelFilters returns ORM filters for row-level permission matching.
// Returns nil filters if no resolver is set, user is Administrator, or no
// match conditions exist.
func (m *DocManager) resolveRowLevelFilters(ctx *DocContext, doctype string) ([]orm.Filter, *auth.EffectivePerms, error) {
	if m.permResolver == nil || ctx.User == nil || auth.IsAdministrator(ctx.User) {
		return nil, nil, nil
	}
	ep, err := m.permResolver.Resolve(ctx, ctx.Site.Name, ctx.User, doctype)
	if err != nil {
		return nil, nil, fmt.Errorf("crud: resolve row-level perms for %q: %w", doctype, err)
	}
	filters := auth.RowLevelFilters(ep, ctx.User)
	return filters, ep, nil
}

// checkRowLevelAccessForDoc verifies a loaded document passes row-level checks.
// Returns DocNotFoundError (not 403) to avoid information leakage.
func (m *DocManager) checkRowLevelAccessForDoc(ctx *DocContext, doctype, name string, doc *DynamicDoc) error {
	if m.permResolver == nil || ctx.User == nil || auth.IsAdministrator(ctx.User) {
		return nil
	}
	ep, err := m.permResolver.Resolve(ctx, ctx.Site.Name, ctx.User, doctype)
	if err != nil {
		return fmt.Errorf("crud: resolve row-level perms for %q: %w", doctype, err)
	}
	if !auth.CheckRowLevelAccess(ep, ctx.User, doc.AsMap()) {
		return &DocNotFoundError{Doctype: doctype, Name: name}
	}
	return nil
}

// dispatchHooks calls the hook dispatcher if one is configured.
// Returns nil when no dispatcher is set (hooks are optional).
func (m *DocManager) dispatchHooks(ctx *DocContext, doc Document, doctype string, event DocEvent) error {
	if m.hookDispatcher == nil {
		return nil
	}
	return m.hookDispatcher.Dispatch(ctx, doc, doctype, event)
}

// NewDocManager constructs a DocManager. All parameters are required.
func NewDocManager(
	registry *meta.Registry,
	db *orm.DBManager,
	naming *NamingEngine,
	validator *Validator,
	controllers *ControllerRegistry,
	logger *slog.Logger,
) *DocManager {
	return &DocManager{
		registry:     registry,
		db:           db,
		queryAdapter: meta.NewQueryMetaAdapter(registry),
		naming:       naming,
		validator:    validator,
		controllers:  controllers,
		logger:       logger,
	}
}

// resolveChildMetas loads the MetaType for each Table field in mt from the
// registry, returning a map keyed by field name.
func (m *DocManager) resolveChildMetas(ctx context.Context, site string, mt *meta.MetaType) (map[string]*meta.MetaType, error) {
	childMetas := make(map[string]*meta.MetaType)
	for _, f := range mt.Fields {
		if f.FieldType != meta.FieldTypeTable && f.FieldType != meta.FieldTypeTableMultiSelect {
			continue
		}
		if f.Options == "" {
			continue
		}
		childMeta, err := m.registry.Get(ctx, site, f.Options)
		if err != nil {
			return nil, fmt.Errorf("crud: resolve child meta %q for field %q on %q: %w",
				f.Options, f.Name, mt.Name, err)
		}
		childMetas[f.Name] = childMeta
	}
	return childMetas, nil
}

// sitePool returns the pgxpool for the site in ctx.
func sitePool(ctx *DocContext) (*pgxpool.Pool, error) {
	if ctx.Site == nil || ctx.Site.Pool == nil {
		return nil, fmt.Errorf("crud: DocContext.Site.Pool is nil; a tenant pool is required for CRUD operations")
	}
	return ctx.Site.Pool, nil
}

// userID returns the email of the authenticated user or "system" as fallback.
func userID(ctx *DocContext) string {
	if ctx.User != nil && ctx.User.Email != "" {
		return ctx.User.Email
	}
	return "system"
}

// isTruthyFlag returns true if ctx.Flags[key] is a truthy value.
func isTruthyFlag(ctx *DocContext, key string) bool {
	v, ok := ctx.Flags[key]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	default:
		return v != nil
	}
}

// ─── Transactional helpers ────────────────────────────────────────────────────

func buildDocumentEvent(
	ctx *DocContext,
	eventType, doctype, docname string,
	data, prevData any,
) (events.DocumentEvent, error) {
	site := ""
	if ctx.Site != nil {
		site = ctx.Site.Name
	}
	return events.NewDocumentEvent(
		eventType,
		site,
		doctype,
		docname,
		userID(ctx),
		ctx.RequestID,
		data,
		prevData,
	)
}

// insertOutbox writes a canonical document event inside an active transaction.
func insertOutbox(ctx context.Context, tx pgx.Tx, event events.DocumentEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("crud: marshal outbox event: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO tab_outbox ("event_type","topic","partition_key","payload") VALUES ($1,$2,$3,$4)`,
		event.EventType,
		events.TopicDocumentEvents,
		events.PartitionKey(event.Site, event.DocType),
		payload,
	)
	if err != nil {
		return fmt.Errorf("crud: insert outbox (event_type=%q topic=%q): %w", event.EventType, events.TopicDocumentEvents, err)
	}
	return nil
}

// EventLogRow represents a single row written to tab_event_log when a
// MetaType has EventSourcing enabled. It stores a full event envelope so
// the event store can be replayed independently of the transactional outbox.
type EventLogRow struct {
	DocType   string          `json:"doctype"`
	DocName   string          `json:"docname"`
	EventType string          `json:"event_type"`
	UserID    string          `json:"user_id"`
	RequestID string          `json:"request_id"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
	PrevData  json.RawMessage `json:"prev_data,omitempty"`
	ID        int64           `json:"id"`
}

// insertEventLog writes a canonical event-log row inside an active transaction.
func insertEventLog(ctx context.Context, tx pgx.Tx, row EventLogRow) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO tab_event_log ("doctype","docname","event_type","payload","prev_data","user_id","request_id","created_at")
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		row.DocType, row.DocName, row.EventType, row.Payload, row.PrevData, row.UserID, row.RequestID, row.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("crud: insert event_log (doctype=%q docname=%q): %w", row.DocType, row.DocName, err)
	}
	return nil
}

// buildEventLogRow constructs an EventLogRow from a DocumentEvent envelope.
func buildEventLogRow(event events.DocumentEvent) (EventLogRow, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return EventLogRow{}, fmt.Errorf("crud: marshal event log payload: %w", err)
	}
	var prevData json.RawMessage
	if event.PrevData != nil {
		prevData, err = json.Marshal(event.PrevData)
		if err != nil {
			return EventLogRow{}, fmt.Errorf("crud: marshal event log prev_data: %w", err)
		}
	}
	return EventLogRow{
		DocType:   event.DocType,
		DocName:   event.DocName,
		EventType: event.EventType,
		Payload:   payload,
		PrevData:  prevData,
		UserID:    event.User,
		RequestID: event.RequestID,
		CreatedAt: event.Timestamp,
	}, nil
}

// insertAuditLog writes a single audit record inside an active transaction.
// action is one of "Create", "Update", "Delete".
// changes is a JSONB-encoded diff (may be nil for Create/Delete).
func insertAuditLog(ctx context.Context, tx pgx.Tx, doctype, docname, action, uid string, changes []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO tab_audit_log ("doctype","docname","action","user_id","timestamp","changes") VALUES ($1,$2,$3,$4,NOW(),$5)`,
		doctype, docname, action, uid, changes,
	)
	if err != nil {
		return fmt.Errorf("crud: insert audit_log (action=%q doctype=%q docname=%q): %w",
			action, doctype, docname, err)
	}
	return nil
}

// insertChildRows INSERTs all current children of doc inside tx.
// parentName, parentType, and parentField are set on each child row.
func (m *DocManager) insertChildRows(ctx context.Context, tx pgx.Tx, doc *DynamicDoc, uid string) error {
	now := time.Now()
	for _, f := range doc.metaDef.Fields {
		if f.FieldType != meta.FieldTypeTable && f.FieldType != meta.FieldTypeTableMultiSelect {
			continue
		}
		children := doc.children[f.Name]
		if len(children) == 0 {
			continue
		}
		childMeta, ok := doc.childMetas[f.Name]
		if !ok {
			continue
		}
		insertSQL, columns := buildInsertSQL(childMeta)
		for i, child := range children {
			// Ensure all required parent-link and system fields are set.
			childName := child.Name()
			if childName == "" {
				uuid, err := m.naming.generateUUID()
				if err != nil {
					return fmt.Errorf("crud: generate child name for field %q row %d: %w", f.Name, i, err)
				}
				child.values["name"] = uuid
			}
			child.values["parent"] = doc.Name()
			child.values["parenttype"] = doc.metaDef.Name
			child.values["parentfield"] = f.Name
			child.values["idx"] = i
			child.values["owner"] = uid
			child.values["modified_by"] = uid
			if child.values["creation"] == nil {
				child.values["creation"] = now
			}
			if child.values["modified"] == nil {
				child.values["modified"] = now
			}

			args := extractValues(child, columns)
			if _, err := tx.Exec(ctx, insertSQL, args...); err != nil {
				return fmt.Errorf("crud: insert child row for field %q row %d (doctype=%q): %w",
					f.Name, i, childMeta.Name, err)
			}
		}
	}
	return nil
}

// deleteChildRows deletes all child rows for doc across every Table field.
func (m *DocManager) deleteChildRows(ctx context.Context, tx pgx.Tx, doc *DynamicDoc) error {
	for _, f := range doc.metaDef.Fields {
		if f.FieldType != meta.FieldTypeTable && f.FieldType != meta.FieldTypeTableMultiSelect {
			continue
		}
		childMeta, ok := doc.childMetas[f.Name]
		if !ok {
			// No registered child meta; nothing to delete.
			continue
		}
		childTable := meta.TableName(childMeta.Name)
		_, err := tx.Exec(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE %s = $1 AND %s = $2",
				pgx.Identifier{childTable}.Sanitize(),
				pgx.Identifier{"parent"}.Sanitize(),
				pgx.Identifier{"parenttype"}.Sanitize(),
			),
			doc.Name(), doc.metaDef.Name,
		)
		if err != nil {
			return fmt.Errorf("crud: delete children for field %q (doctype=%q, parent=%q): %w",
				f.Name, childMeta.Name, doc.Name(), err)
		}
	}
	return nil
}

// syncChildRows performs a delete-all + reinsert for all Table fields of doc.
// This is the simplest correct strategy for v1; per-row diffing is a future optimisation.
func (m *DocManager) syncChildRows(ctx context.Context, tx pgx.Tx, doc *DynamicDoc, uid string) error {
	if err := m.deleteChildRows(ctx, tx, doc); err != nil {
		return err
	}
	return m.insertChildRows(ctx, tx, doc, uid)
}

// ─── CRUD Operations ─────────────────────────────────────────────────────────

// Insert creates a new document of the given doctype with the provided values.
// values may include child table rows as []any under the Table field names.
//
// Lifecycle events fired (in order):
//
//	BeforeInsert → BeforeValidate → controller.Validate → field validation →
//	BeforeSave → [TX: INSERT] → AfterInsert → AfterSave → OnChange (logged)
func (m *DocManager) Insert(ctx *DocContext, doctype string, values map[string]any) (*DynamicDoc, error) {
	pool, err := sitePool(ctx)
	if err != nil {
		return nil, err
	}
	uid := userID(ctx)

	// 1. Load MetaType and resolve child MetaTypes.
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: load MetaType: %w", doctype, err)
	}
	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.insertVirtual(ctx, mt, values)
	}

	// 2. Create document and apply incoming values.
	doc := NewDynamicDoc(mt, childMetas, true)
	err = applyValues(doc, values)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: apply values: %w", doctype, err)
	}

	// 3. Resolve controller and dispatch BeforeInsert.
	ctrl := m.controllers.Resolve(doctype)
	err = dispatchEvent(ctrl, EventBeforeInsert, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeInsert: %w", doctype, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventBeforeInsert); err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeInsert hook: %w", doctype, err)
	}

	// 4. Generate document name (outside TX so sequences are never rolled back).
	name, err := m.naming.GenerateName(ctx, doc, pool)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: naming: %w", doctype, err)
	}
	err = doc.Set("name", name)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: set name: %w", doctype, err)
	}

	// 5. Set system fields.
	now := time.Now()
	_ = doc.Set("owner", uid)
	_ = doc.Set("modified_by", uid)
	_ = doc.Set("creation", now)
	_ = doc.Set("modified", now)
	if doc.Get("docstatus") == nil {
		_ = doc.Set("docstatus", int16(0))
	}

	// 6. Pre-save lifecycle: BeforeValidate → controller.Validate → field validation → BeforeSave.
	err = dispatchEvent(ctrl, EventBeforeValidate, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeValidate: %w", doctype, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventBeforeValidate); err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeValidate hook: %w", doctype, err)
	}
	err = dispatchEvent(ctrl, EventValidate, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: Validate: %w", doctype, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventValidate); err != nil {
		return nil, fmt.Errorf("crud: Insert %q: Validate hook: %w", doctype, err)
	}
	if !isTruthyFlag(ctx, "skip_validation") {
		err = m.validator.ValidateDoc(ctx, doc, pool)
		if err != nil {
			return nil, fmt.Errorf("crud: Insert %q: validation: %w", doctype, err)
		}
	}
	err = dispatchEvent(ctrl, EventBeforeSave, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeSave: %w", doctype, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventBeforeSave); err != nil {
		return nil, fmt.Errorf("crud: Insert %q: BeforeSave hook: %w", doctype, err)
	}

	outboxEvent, err := buildDocumentEvent(ctx, events.EventTypeDocCreated, doctype, name, doc.AsMap(), nil)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q: build outbox event: %w", doctype, err)
	}

	// 8. Database transaction: INSERT parent + children + outbox + audit.
	insertSQL, columns := buildInsertSQL(mt)
	args := extractValues(doc, columns)

	txErr := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		var txErr error
		_, txErr = tx.Exec(txCtx, insertSQL, args...)
		if txErr != nil {
			return fmt.Errorf("INSERT %s: %w", meta.TableName(doctype), txErr)
		}
		txErr = m.insertChildRows(txCtx, tx, doc, uid)
		if txErr != nil {
			return txErr
		}
		txErr = insertOutbox(txCtx, tx, outboxEvent)
		if txErr != nil {
			return txErr
		}
		if mt.EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
		if mt.TrackChanges {
			if txErr = insertVersion(txCtx, tx, doctype, name, uid, nil, doc.AsMap()); txErr != nil {
				return txErr
			}
		}
		return insertAuditLog(txCtx, tx, doctype, name, "Create", uid, nil)
	})
	if txErr != nil {
		return nil, fmt.Errorf("crud: Insert %q %q: %w", doctype, name, txErr)
	}

	// 9. Mark as persisted and reset dirty state.
	doc.markPersisted()
	doc.resetDirtyState()

	// 10. Post-commit hooks (fatal).
	err = dispatchEvent(ctrl, EventAfterInsert, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q %q: AfterInsert: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventAfterInsert); err != nil {
		return nil, fmt.Errorf("crud: Insert %q %q: AfterInsert hook: %w", doctype, name, err)
	}
	err = dispatchEvent(ctrl, EventAfterSave, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert %q %q: AfterSave: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventAfterSave); err != nil {
		return nil, fmt.Errorf("crud: Insert %q %q: AfterSave hook: %w", doctype, name, err)
	}

	// 11. OnChange is fire-and-forget: errors are logged, not returned.
	err = dispatchEvent(ctrl, EventOnChange, ctx, doc)
	if err != nil {
		m.logger.Warn("crud: Insert OnChange error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventOnChange); err != nil {
		m.logger.Warn("crud: Insert OnChange hook error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}

	return doc, nil
}

// Update applies the provided values to an existing document and persists the
// changes. values may include child table rows ([]any) under Table field names.
//
// Lifecycle events fired (in order):
//
//	BeforeValidate → controller.Validate → field validation → BeforeSave →
//	OnUpdate → [TX: UPDATE] → AfterSave → OnChange (logged)
func (m *DocManager) Update(ctx *DocContext, doctype, name string, values map[string]any) (*DynamicDoc, error) {
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q: load MetaType: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.updateVirtual(ctx, mt, name, values)
	}

	pool, err := sitePool(ctx)
	if err != nil {
		return nil, err
	}
	uid := userID(ctx)

	// 1. Load current state from DB.
	doc, err := m.Get(ctx, doctype, name)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: load: %w", doctype, name, err)
	}
	prevData := doc.AsMap()

	// 2. Apply incoming values (scalar + child table rows).
	err = applyValues(doc, values)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: apply values: %w", doctype, name, err)
	}

	// 3. Quick no-op check: if nothing changed, return early.
	hasChildChange := hasTableFieldKey(values, doc)
	if !doc.IsModified() && !hasChildChange {
		return doc, nil
	}

	// 4. Set system timestamps.
	now := time.Now()
	_ = doc.Set("modified", now)
	_ = doc.Set("modified_by", uid)

	// 5. Capture modified fields and build audit diff before dispatching hooks
	//    (hooks may further modify the doc).
	modifiedBeforeHooks := doc.ModifiedFields()

	// 6. Resolve controller.
	ctrl := m.controllers.Resolve(doctype)

	// 7. Pre-save lifecycle.
	err = dispatchEvent(ctrl, EventBeforeValidate, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: BeforeValidate: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventBeforeValidate); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: BeforeValidate hook: %w", doctype, name, err)
	}
	err = dispatchEvent(ctrl, EventValidate, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: Validate: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventValidate); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: Validate hook: %w", doctype, name, err)
	}
	if !isTruthyFlag(ctx, "skip_validation") {
		err = m.validator.ValidateDoc(ctx, doc, pool)
		if err != nil {
			return nil, fmt.Errorf("crud: Update %q %q: validation: %w", doctype, name, err)
		}
	}
	err = dispatchEvent(ctrl, EventBeforeSave, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: BeforeSave: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventBeforeSave); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: BeforeSave hook: %w", doctype, name, err)
	}
	err = dispatchEvent(ctrl, EventOnUpdate, ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: OnUpdate: %w", doctype, name, err)
	}
	if err = m.dispatchHooks(ctx, doc, doctype, EventOnUpdate); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: OnUpdate hook: %w", doctype, name, err)
	}

	// 8. Build audit diff from the fields that were modified before hooks ran.
	changesJSON := buildChangesJSON(doc, modifiedBeforeHooks)

	// 9. Build the final modified field list after all pre-save hooks ran.
	finalModifiedFields := doc.ModifiedFields()

	outboxEvent, err := buildDocumentEvent(ctx, events.EventTypeDocUpdated, doctype, name, doc.AsMap(), prevData)
	if err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: build outbox event: %w", doctype, name, err)
	}

	// 11. Transaction: UPDATE parent + sync children + outbox + audit.
	txErr := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		// Only run the UPDATE if there are actual scalar changes.
		scalarFields := filterScalarFields(doc, finalModifiedFields)
		if len(scalarFields) > 0 {
			updateSQL, updateCols := buildUpdateSQL(doc.metaDef, scalarFields)
			updateArgs := extractValues(doc, updateCols)
			updateArgs = append(updateArgs, name) // WHERE name = $N
			if _, err := tx.Exec(txCtx, updateSQL, updateArgs...); err != nil {
				return fmt.Errorf("UPDATE %s: %w", meta.TableName(doctype), err)
			}
		} else {
			// Even with no scalar changes, update the timestamp if there are child changes.
			if hasChildChange {
				tsSQL := fmt.Sprintf("UPDATE %s SET %s = $1, %s = $2 WHERE %s = $3",
					pgx.Identifier{meta.TableName(doctype)}.Sanitize(),
					pgx.Identifier{"modified"}.Sanitize(),
					pgx.Identifier{"modified_by"}.Sanitize(),
					pgx.Identifier{"name"}.Sanitize(),
				)
				if _, err := tx.Exec(txCtx, tsSQL, now, uid, name); err != nil {
					return fmt.Errorf("UPDATE modified timestamp %s: %w", meta.TableName(doctype), err)
				}
			}
		}
		if err := m.syncChildRows(txCtx, tx, doc, uid); err != nil {
			return err
		}
		if err := insertOutbox(txCtx, tx, outboxEvent); err != nil {
			return err
		}
		if doc.Meta().EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
		if doc.Meta().TrackChanges {
			versionDiff := buildVersionDiff(doc, modifiedBeforeHooks)
			if err := insertVersion(txCtx, tx, doctype, name, uid, versionDiff, doc.AsMap()); err != nil {
				return err
			}
		}
		return insertAuditLog(txCtx, tx, doctype, name, "Update", uid, changesJSON)
	})
	if txErr != nil {
		return nil, fmt.Errorf("crud: Update %q %q: %w", doctype, name, txErr)
	}

	// 12. Reset dirty state.
	doc.resetDirtyState()

	// 13. Post-commit hooks (fatal).
	if err := dispatchEvent(ctrl, EventAfterSave, ctx, doc); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: AfterSave: %w", doctype, name, err)
	}
	if err := m.dispatchHooks(ctx, doc, doctype, EventAfterSave); err != nil {
		return nil, fmt.Errorf("crud: Update %q %q: AfterSave hook: %w", doctype, name, err)
	}

	// 14. OnChange is fire-and-forget.
	if err := dispatchEvent(ctrl, EventOnChange, ctx, doc); err != nil {
		m.logger.Warn("crud: Update OnChange error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}
	if err := m.dispatchHooks(ctx, doc, doctype, EventOnChange); err != nil {
		m.logger.Warn("crud: Update OnChange hook error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}

	return doc, nil
}

// Delete removes an existing document (and all its child rows) from the database.
//
// Lifecycle events fired:
//
//	OnTrash → [TX: DELETE] → AfterDelete (logged)
func (m *DocManager) Delete(ctx *DocContext, doctype, name string) error {
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return fmt.Errorf("crud: Delete %q: load MetaType: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.deleteVirtual(ctx, mt, name)
	}

	pool, err := sitePool(ctx)
	if err != nil {
		return err
	}
	uid := userID(ctx)

	// 1. Load current state.
	doc, err := m.Get(ctx, doctype, name)
	if err != nil {
		return fmt.Errorf("crud: Delete %q %q: load: %w", doctype, name, err)
	}
	prevData := doc.AsMap()

	// 2. Dispatch OnTrash (fatal: if controller rejects, abort).
	ctrl := m.controllers.Resolve(doctype)
	err = dispatchEvent(ctrl, EventOnTrash, ctx, doc)
	if err != nil {
		return fmt.Errorf("crud: Delete %q %q: OnTrash: %w", doctype, name, err)
	}
	err = m.dispatchHooks(ctx, doc, doctype, EventOnTrash)
	if err != nil {
		return fmt.Errorf("crud: Delete %q %q: OnTrash hook: %w", doctype, name, err)
	}

	// 3. Transaction: DELETE children + parent + outbox + audit.
	tableSQL := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
		pgx.Identifier{meta.TableName(doctype)}.Sanitize(),
		pgx.Identifier{"name"}.Sanitize(),
	)

	outboxEvent, err := buildDocumentEvent(ctx, events.EventTypeDocDeleted, doctype, name, map[string]any{"name": name}, prevData)
	if err != nil {
		return fmt.Errorf("crud: Delete %q %q: build outbox event: %w", doctype, name, err)
	}

	txErr := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if err := m.deleteChildRows(txCtx, tx, doc); err != nil {
			return err
		}
		if _, err := tx.Exec(txCtx, tableSQL, name); err != nil {
			return fmt.Errorf("DELETE %s %q: %w", meta.TableName(doctype), name, err)
		}
		if err := insertOutbox(txCtx, tx, outboxEvent); err != nil {
			return err
		}
		if doc.Meta().EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
		return insertAuditLog(txCtx, tx, doctype, name, "Delete", uid, nil)
	})
	if txErr != nil {
		return fmt.Errorf("crud: Delete %q %q: %w", doctype, name, txErr)
	}

	// 4. AfterDelete is non-fatal (data already deleted; log errors only).
	if err := dispatchEvent(ctrl, EventAfterDelete, ctx, doc); err != nil {
		m.logger.Warn("crud: Delete AfterDelete error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}
	if err := m.dispatchHooks(ctx, doc, doctype, EventAfterDelete); err != nil {
		m.logger.Warn("crud: Delete AfterDelete hook error (non-fatal)",
			"doctype", doctype, "name", name, "error", err)
	}

	return nil
}

// ─── Virtual routing helpers ─────────────────────────────────────────────────

// getVirtual retrieves a document from a VirtualSource.
func (m *DocManager) getVirtual(ctx *DocContext, mt *meta.MetaType, name string) (*DynamicDoc, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, fmt.Errorf("crud: Get virtual %q: no source registered", mt.Name)
	}
	values, err := src.GetOne(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("crud: Get virtual %q %q: %w", mt.Name, name, err)
	}
	if values == nil {
		return nil, &DocNotFoundError{Doctype: mt.Name, Name: name}
	}
	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, fmt.Errorf("crud: Get virtual %q: %w", mt.Name, err)
	}
	doc := NewDynamicDoc(mt, childMetas, false)
	if applyErr := applyValues(doc, values); applyErr != nil {
		return nil, fmt.Errorf("crud: Get virtual %q: apply values: %w", mt.Name, applyErr)
	}
	doc.markPersisted()
	doc.resetDirtyState()
	return doc, nil
}

// getListVirtual retrieves documents from a VirtualSource.
func (m *DocManager) getListVirtual(ctx *DocContext, mt *meta.MetaType, opts ListOptions) ([]*DynamicDoc, int, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, 0, fmt.Errorf("crud: GetList virtual %q: no source registered", mt.Name)
	}
	results, total, err := src.GetList(ctx, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList virtual %q: %w", mt.Name, err)
	}
	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList virtual %q: %w", mt.Name, err)
	}
	docs := make([]*DynamicDoc, 0, len(results))
	for _, values := range results {
		doc := NewDynamicDoc(mt, childMetas, false)
		if applyErr := applyValues(doc, values); applyErr != nil {
			return nil, 0, fmt.Errorf("crud: GetList virtual %q: apply values: %w", mt.Name, applyErr)
		}
		doc.markPersisted()
		doc.resetDirtyState()
		docs = append(docs, doc)
	}
	return docs, total, nil
}

// insertVirtual creates a document via a VirtualSource.
func (m *DocManager) insertVirtual(ctx *DocContext, mt *meta.MetaType, values map[string]any) (*DynamicDoc, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, fmt.Errorf("crud: Insert virtual %q: no source registered", mt.Name)
	}
	name, err := src.Insert(ctx, values)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert virtual %q: %w", mt.Name, err)
	}
	values["name"] = name
	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, fmt.Errorf("crud: Insert virtual %q: %w", mt.Name, err)
	}
	doc := NewDynamicDoc(mt, childMetas, false)
	if applyErr := applyValues(doc, values); applyErr != nil {
		return nil, fmt.Errorf("crud: Insert virtual %q: apply values: %w", mt.Name, applyErr)
	}
	doc.markPersisted()
	doc.resetDirtyState()
	return doc, nil
}

// updateVirtual updates a document via a VirtualSource.
func (m *DocManager) updateVirtual(ctx *DocContext, mt *meta.MetaType, name string, values map[string]any) (*DynamicDoc, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, fmt.Errorf("crud: Update virtual %q: no source registered", mt.Name)
	}
	if err := src.Update(ctx, name, values); err != nil {
		return nil, fmt.Errorf("crud: Update virtual %q %q: %w", mt.Name, name, err)
	}
	// Re-fetch to get the current state.
	return m.getVirtual(ctx, mt, name)
}

// deleteVirtual deletes a document via a VirtualSource.
func (m *DocManager) deleteVirtual(ctx *DocContext, mt *meta.MetaType, name string) error {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return fmt.Errorf("crud: Delete virtual %q: no source registered", mt.Name)
	}
	if err := src.Delete(ctx, name); err != nil {
		return fmt.Errorf("crud: Delete virtual %q %q: %w", mt.Name, name, err)
	}
	return nil
}

// Get retrieves a document by doctype and name. Returns *DocNotFoundError if
// no document with that name exists. Child table rows are eagerly loaded for
// all Table fields.
func (m *DocManager) Get(ctx *DocContext, doctype, name string) (*DynamicDoc, error) {
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, fmt.Errorf("crud: Get %q: load MetaType: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.getVirtual(ctx, mt, name)
	}

	pool, err := sitePool(ctx)
	if err != nil {
		return nil, err
	}

	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, fmt.Errorf("crud: Get %q: %w", doctype, err)
	}

	selectSQL, columns := buildSelectSQL(mt)

	rows, err := pool.Query(ctx, selectSQL, name)
	if err != nil {
		return nil, fmt.Errorf("crud: Get %q %q: query: %w", doctype, name, err)
	}
	defer rows.Close()

	if !rows.Next() {
		err = rows.Err()
		if err != nil {
			return nil, fmt.Errorf("crud: Get %q %q: scan: %w", doctype, name, err)
		}
		return nil, &DocNotFoundError{Doctype: doctype, Name: name}
	}

	vals, err := rows.Values()
	if err != nil {
		return nil, fmt.Errorf("crud: Get %q %q: read values: %w", doctype, name, err)
	}
	rows.Close()

	doc := NewDynamicDoc(mt, childMetas, false)
	for i, col := range columns {
		doc.values[col] = normalizeDBValue(vals[i])
	}
	doc.resetDirtyState()

	// Load child rows for each Table field.
	for _, f := range mt.Fields {
		if f.FieldType != meta.FieldTypeTable && f.FieldType != meta.FieldTypeTableMultiSelect {
			continue
		}
		childMeta, ok := childMetas[f.Name]
		if !ok {
			continue
		}
		children, err := m.loadChildRows(ctx, pool, childMeta, name, f.Name)
		if err != nil {
			return nil, fmt.Errorf("crud: Get %q %q: load children for field %q: %w",
				doctype, name, f.Name, err)
		}
		doc.children[f.Name] = children
	}
	doc.resetDirtyState() // re-snapshot after children are loaded

	// Transparent field decryption (e.g. Password fields encrypted at rest).
	if m.postLoadTransformer != nil {
		if err := m.postLoadTransformer.TransformAfterLoad(ctx, doc); err != nil {
			return nil, fmt.Errorf("crud: Get %q %q: post-load transform: %w", doctype, name, err)
		}
		doc.resetDirtyState() // re-snapshot after decryption
	}

	// Row-level permission check: return 404 (not 403) to avoid info leakage.
	if err := m.checkRowLevelAccessForDoc(ctx, doctype, name, doc); err != nil {
		return nil, err
	}

	return doc, nil
}

// loadChildRows fetches all child rows for a given parent document and field.
func (m *DocManager) loadChildRows(ctx context.Context, pool *pgxpool.Pool, childMeta *meta.MetaType, parentName, parentField string) ([]*DynamicDoc, error) {
	childTable := meta.TableName(childMeta.Name)
	childCols := buildDocColumns(childMeta)
	quotedCols := quoteIdents(childCols)

	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1 AND %s = $2 ORDER BY %s ASC",
		strings.Join(quotedCols, ", "),
		pgx.Identifier{childTable}.Sanitize(),
		pgx.Identifier{"parent"}.Sanitize(),
		pgx.Identifier{"parentfield"}.Sanitize(),
		pgx.Identifier{"idx"}.Sanitize(),
	)

	rows, err := pool.Query(ctx, sql, parentName, parentField)
	if err != nil {
		return nil, fmt.Errorf("query child table %s: %w", childTable, err)
	}
	defer rows.Close()

	var children []*DynamicDoc
	var vals []any
	for rows.Next() {
		vals, err = rows.Values()
		if err != nil {
			return nil, fmt.Errorf("scan child row from %s: %w", childTable, err)
		}
		child := NewDynamicDoc(childMeta, nil, false)
		for i, col := range childCols {
			child.values[col] = normalizeDBValue(vals[i])
		}
		child.resetDirtyState()
		children = append(children, child)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate child rows from %s: %w", childTable, err)
	}
	return children, nil
}

// GetList returns a paginated list of documents matching the given options.
// Child rows are NOT loaded for performance (use Get for full documents).
// Returns the slice of documents, the total count matching the filters, and
// any error.
//
// Filter keys are validated against the MetaType's known columns to prevent
// SQL injection. Values are always parameterised.
func (m *DocManager) GetList(ctx *DocContext, doctype string, opts ListOptions) ([]*DynamicDoc, int, error) {
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: load MetaType: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.getListVirtual(ctx, mt, opts)
	}

	pool, err := sitePool(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Merge legacy equality filters and advanced filters.
	var allFilters []orm.Filter
	if len(opts.Filters) > 0 {
		keys := make([]string, 0, len(opts.Filters))
		for k := range opts.Filters {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic ordering
		for _, k := range keys {
			allFilters = append(allFilters, orm.Filter{
				Field:    k,
				Operator: orm.OpEqual,
				Value:    opts.Filters[k],
			})
		}
	}
	allFilters = append(allFilters, opts.AdvancedFilters...)

	// Row-level permission filters (OR-ed among themselves).
	rowFilters, _, rlErr := m.resolveRowLevelFilters(ctx, doctype)
	if rlErr != nil {
		return nil, 0, rlErr
	}

	// Build a base QueryBuilder with filters and groupBy.
	base := orm.NewQueryBuilder(m.queryAdapter, ctx.Site.Name).For(doctype)
	if len(allFilters) > 0 {
		base = base.Where(allFilters...)
	}
	if len(rowFilters) > 0 {
		base = base.WhereOr(rowFilters...)
	}
	if len(opts.GroupBy) > 0 {
		base = base.GroupBy(opts.GroupBy...)
	}

	// COUNT query.
	countSQL, countArgs, err := base.BuildCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: build count: %w", doctype, err)
	}
	var total int
	err = pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: count: %w", doctype, err)
	}

	// Main SELECT query — rebuild to add fields, order, limit, offset.
	qb := orm.NewQueryBuilder(m.queryAdapter, ctx.Site.Name).For(doctype)
	if len(opts.Fields) > 0 {
		qb = qb.Fields(opts.Fields...)
	}
	if len(allFilters) > 0 {
		qb = qb.Where(allFilters...)
	}
	if len(rowFilters) > 0 {
		qb = qb.WhereOr(rowFilters...)
	}
	if len(opts.GroupBy) > 0 {
		qb = qb.GroupBy(opts.GroupBy...)
	}

	// Order: OrderByMulti takes precedence over legacy OrderBy/OrderDir.
	if len(opts.OrderByMulti) > 0 {
		for _, o := range opts.OrderByMulti {
			qb = qb.OrderBy(o.Field, o.Direction)
		}
	} else if opts.OrderBy != "" {
		dir := strings.ToUpper(opts.OrderDir)
		if dir != "ASC" && dir != "DESC" {
			dir = "DESC"
		}
		qb = qb.OrderBy(opts.OrderBy, dir)
	}
	// When neither is set, QueryBuilder defaults to "modified" DESC.

	qb = qb.Limit(opts.Limit).Offset(opts.Offset)

	mainSQL, mainArgs, err := qb.Build(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: build query: %w", doctype, err)
	}

	rows, err := pool.Query(ctx, mainSQL, mainArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: query: %w", doctype, err)
	}
	defer rows.Close()

	// Derive column names from the query result for flexible scanning.
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = fd.Name
	}

	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: %w", doctype, err)
	}

	var docs []*DynamicDoc
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, 0, fmt.Errorf("crud: GetList %q: scan row: %w", doctype, err)
		}
		doc := NewDynamicDoc(mt, childMetas, false)
		for i, col := range colNames {
			doc.values[col] = normalizeDBValue(vals[i])
		}
		doc.resetDirtyState()
		if m.postLoadTransformer != nil {
			if err := m.postLoadTransformer.TransformAfterLoad(ctx, doc); err != nil {
				return nil, 0, fmt.Errorf("crud: GetList %q: post-load transform: %w", doctype, err)
			}
			doc.resetDirtyState()
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: iterate rows: %w", doctype, err)
	}

	return docs, total, nil
}

// GetSingle retrieves a Single-type document (MetaType.IsSingle == true).
// Values are stored as key-value pairs in tab_singles and reconstructed into
// a DynamicDoc.
func (m *DocManager) GetSingle(ctx *DocContext, doctype string) (*DynamicDoc, error) {
	pool, err := sitePool(ctx)
	if err != nil {
		return nil, err
	}

	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, fmt.Errorf("crud: GetSingle %q: load MetaType: %w", doctype, err)
	}
	if !mt.IsSingle {
		return nil, fmt.Errorf("crud: GetSingle %q: doctype is not a Single", doctype)
	}

	rows, err := pool.Query(ctx,
		`SELECT "field", "value" FROM tab_singles WHERE "doctype" = $1`,
		doctype,
	)
	if err != nil {
		return nil, fmt.Errorf("crud: GetSingle %q: query: %w", doctype, err)
	}
	defer rows.Close()

	doc := NewDynamicDoc(mt, nil, false)
	for rows.Next() {
		var field, value string
		if err := rows.Scan(&field, &value); err != nil {
			return nil, fmt.Errorf("crud: GetSingle %q: scan: %w", doctype, err)
		}
		// Values are stored as TEXT in tab_singles; ignore unknown field names.
		_ = doc.Set(field, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("crud: GetSingle %q: iterate: %w", doctype, err)
	}

	doc.resetDirtyState()

	// Transparent field decryption for Single documents.
	if m.postLoadTransformer != nil {
		if err := m.postLoadTransformer.TransformAfterLoad(ctx, doc); err != nil {
			return nil, fmt.Errorf("crud: GetSingle %q: post-load transform: %w", doctype, err)
		}
		doc.resetDirtyState()
	}

	return doc, nil
}

// SetSingle persists a Single-type document by upserting all storable fields
// into tab_singles. The full lifecycle (BeforeValidate → Validate → validation
// → BeforeSave → AfterSave → OnChange) is executed.
func (m *DocManager) SetSingle(ctx *DocContext, doc *DynamicDoc) error {
	pool, err := sitePool(ctx)
	if err != nil {
		return err
	}

	if !doc.metaDef.IsSingle {
		return fmt.Errorf("crud: SetSingle: doctype %q is not a Single", doc.metaDef.Name)
	}
	doctype := doc.metaDef.Name
	uid := userID(ctx)
	prevData := deepCopyMap(doc.original)

	ctrl := m.controllers.Resolve(doctype)

	err = dispatchEvent(ctrl, EventBeforeValidate, ctx, doc)
	if err != nil {
		return fmt.Errorf("crud: SetSingle %q: BeforeValidate: %w", doctype, err)
	}
	err = dispatchEvent(ctrl, EventValidate, ctx, doc)
	if err != nil {
		return fmt.Errorf("crud: SetSingle %q: Validate: %w", doctype, err)
	}
	if !isTruthyFlag(ctx, "skip_validation") {
		err = m.validator.ValidateDoc(ctx, doc, pool)
		if err != nil {
			return fmt.Errorf("crud: SetSingle %q: validation: %w", doctype, err)
		}
	}
	err = dispatchEvent(ctrl, EventBeforeSave, ctx, doc)
	if err != nil {
		return fmt.Errorf("crud: SetSingle %q: BeforeSave: %w", doctype, err)
	}

	outboxEvent, err := buildDocumentEvent(ctx, events.EventTypeDocUpdated, doctype, doctype, doc.AsMap(), prevData)
	if err != nil {
		return fmt.Errorf("crud: SetSingle %q: build outbox event: %w", doctype, err)
	}

	txErr := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		for _, f := range doc.metaDef.Fields {
			if meta.ColumnType(f.FieldType) == "" {
				continue
			}
			val := doc.Get(f.Name)
			var valStr *string
			if val != nil {
				s := fmt.Sprintf("%v", val)
				valStr = &s
			}
			_, err := tx.Exec(txCtx,
				`INSERT INTO tab_singles ("doctype","field","value") VALUES ($1,$2,$3)
				 ON CONFLICT ("doctype","field") DO UPDATE SET "value" = EXCLUDED."value"`,
				doctype, f.Name, valStr,
			)
			if err != nil {
				return fmt.Errorf("upsert tab_singles %q field %q: %w", doctype, f.Name, err)
			}
		}
		if err := insertOutbox(txCtx, tx, outboxEvent); err != nil {
			return err
		}
		return insertAuditLog(txCtx, tx, doctype, doctype, "Update", uid, nil)
	})
	if txErr != nil {
		return fmt.Errorf("crud: SetSingle %q: %w", doctype, txErr)
	}

	doc.resetDirtyState()

	if err := dispatchEvent(ctrl, EventAfterSave, ctx, doc); err != nil {
		return fmt.Errorf("crud: SetSingle %q: AfterSave: %w", doctype, err)
	}
	if err := dispatchEvent(ctrl, EventOnChange, ctx, doc); err != nil {
		m.logger.Warn("crud: SetSingle OnChange error (non-fatal)",
			"doctype", doctype, "error", err)
	}

	return nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// hasTableFieldKey returns true if values contains any key that is a Table
// field on doc's MetaType. Used to detect incoming child row changes.
func hasTableFieldKey(values map[string]any, doc *DynamicDoc) bool {
	for k := range values {
		if _, ok := doc.tableFields[k]; ok {
			return true
		}
	}
	return false
}

// filterScalarFields returns the subset of modifiedFields that correspond to
// actual database columns (excludes Table/TableMultiSelect fields and layout
// fields which have no column type).
func filterScalarFields(doc *DynamicDoc, modifiedFields []string) []string {
	var scalar []string
	for _, f := range modifiedFields {
		// Standard columns are always scalar.
		if _, isTable := doc.tableFields[f]; isTable {
			continue
		}
		scalar = append(scalar, f)
	}
	return scalar
}

// buildChangesJSON constructs the JSONB audit diff for an Update operation.
// It compares the original snapshot against current values for each field in
// modifiedFields and returns the JSON-encoded diff, or nil if nothing changed.
func buildChangesJSON(doc *DynamicDoc, modifiedFields []string) []byte {
	if len(modifiedFields) == 0 {
		return nil
	}
	diff := make(map[string]any, len(modifiedFields))
	for _, f := range modifiedFields {
		// Skip system timestamp fields in the audit diff.
		if f == "modified" || f == "modified_by" {
			continue
		}
		diff[f] = map[string]any{
			"old": doc.original[f],
			"new": doc.values[f],
		}
	}
	if len(diff) == 0 {
		return nil
	}
	b, err := json.Marshal(diff)
	if err != nil {
		return nil
	}
	return b
}

// isDocNotFound reports whether err wraps a *DocNotFoundError.
func isDocNotFound(err error) bool {
	var e *DocNotFoundError
	return errors.As(err, &e)
}
