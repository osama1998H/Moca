package document

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventLogQueryOpts configures event log queries.
type EventLogQueryOpts struct {
	Limit     int
	Offset    int
	Since     time.Time // zero = no lower bound
	Until     time.Time // zero = no upper bound
	EventType string    // empty = all
}

// GetHistory returns the ordered event stream for a specific document.
func GetHistory(ctx context.Context, pool *pgxpool.Pool, doctype, docname string, opts EventLogQueryOpts) ([]EventLogRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT "id","doctype","docname","event_type","payload","prev_data","user_id","request_id","created_at"
		FROM tab_event_log
		WHERE "doctype" = $1 AND "docname" = $2`
	args := []any{doctype, docname}
	argIdx := 3

	if !opts.Since.IsZero() {
		query += fmt.Sprintf(` AND "created_at" >= $%d`, argIdx)
		args = append(args, opts.Since)
		argIdx++
	}
	if !opts.Until.IsZero() {
		query += fmt.Sprintf(` AND "created_at" <= $%d`, argIdx)
		args = append(args, opts.Until)
		argIdx++
	}
	if opts.EventType != "" {
		query += fmt.Sprintf(` AND "event_type" = $%d`, argIdx)
		args = append(args, opts.EventType)
		argIdx++
	}

	query += ` ORDER BY "created_at" ASC, "id" ASC`
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, limit, opts.Offset)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eventlog: query history: %w", err)
	}
	defer rows.Close()

	var result []EventLogRow
	for rows.Next() {
		var row EventLogRow
		if err := rows.Scan(
			&row.ID, &row.DocType, &row.DocName, &row.EventType,
			&row.Payload, &row.PrevData, &row.UserID, &row.RequestID, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("eventlog: scan row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: iterate rows: %w", err)
	}
	return result, nil
}

// Replay reconstructs the current state of a document by applying its event
// log in order. Useful for auditing, not for hot-path reads.
func Replay(ctx context.Context, pool *pgxpool.Pool, doctype, docname string) (map[string]any, error) {
	evts, err := GetHistory(ctx, pool, doctype, docname, EventLogQueryOpts{Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("eventlog: replay: %w", err)
	}
	if len(evts) == 0 {
		return nil, fmt.Errorf("eventlog: replay %q %q: no events found", doctype, docname)
	}

	state := make(map[string]any)
	for _, ev := range evts {
		var envelope map[string]any
		if err := json.Unmarshal(ev.Payload, &envelope); err != nil {
			return nil, fmt.Errorf("eventlog: replay unmarshal event %d: %w", ev.ID, err)
		}
		if d, ok := envelope["data"]; ok {
			if m, ok := d.(map[string]any); ok {
				for k, v := range m {
					state[k] = v
				}
			}
		}
	}
	return state, nil
}
