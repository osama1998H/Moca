package document

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VersionRecord represents a single version entry from tab_version.
type VersionRecord struct {
	Data       map[string]any `json:"data"`
	Creation   time.Time      `json:"creation"`
	Name       string         `json:"name"`
	RefDoctype string         `json:"ref_doctype"`
	DocName    string         `json:"docname"`
	Owner      string         `json:"owner"`
}

// generateVersionUUID produces an RFC 4122 version-4 UUID using crypto/rand.
func generateVersionUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("version: uuid rand.Read: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// buildVersionDiff constructs a field-level diff map from the document's
// original and current values for the given modified fields. System timestamp
// fields (modified, modified_by) are excluded.
// Returns nil if no meaningful changes exist.
func buildVersionDiff(doc *DynamicDoc, modifiedFields []string) map[string]any {
	if len(modifiedFields) == 0 {
		return nil
	}
	diff := make(map[string]any, len(modifiedFields))
	for _, f := range modifiedFields {
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
	return diff
}

// buildVersionData marshals the version payload containing the field-level
// diff and a full document snapshot into JSON.
func buildVersionData(changed map[string]any, snapshot map[string]any) ([]byte, error) {
	data := map[string]any{
		"changed":  changed,
		"snapshot": snapshot,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("version: marshal data: %w", err)
	}
	return b, nil
}

// insertVersion writes a version record inside an active transaction.
// changed may be nil for the first version (Insert). snapshot is the full
// document state at the time of the write.
func insertVersion(ctx context.Context, tx pgx.Tx, doctype, docname, uid string, changed map[string]any, snapshot map[string]any) error {
	versionName, err := generateVersionUUID()
	if err != nil {
		return err
	}
	data, err := buildVersionData(changed, snapshot)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO tab_version ("name","ref_doctype","docname","data","owner","creation") VALUES ($1,$2,$3,$4,$5,NOW())`,
		versionName, doctype, docname, data, uid,
	)
	if err != nil {
		return fmt.Errorf("crud: insert version (doctype=%q docname=%q): %w", doctype, docname, err)
	}
	return nil
}

// GetVersions retrieves paginated version history for a document, ordered by
// creation descending (newest first).
func (m *DocManager) GetVersions(ctx *DocContext, doctype, docname string, limit, offset int) ([]VersionRecord, int, error) {
	pool, err := sitePool(ctx)
	if err != nil {
		return nil, 0, err
	}
	return fetchVersions(ctx, pool, doctype, docname, limit, offset)
}

// fetchVersions queries tab_version for a given doctype/docname with pagination.
func fetchVersions(ctx context.Context, pool *pgxpool.Pool, doctype, docname string, limit, offset int) ([]VersionRecord, int, error) {
	// Count total matching records.
	var total int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tab_version WHERE "ref_doctype" = $1 AND "docname" = $2`,
		doctype, docname,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: count versions (doctype=%q docname=%q): %w", doctype, docname, err)
	}

	if total == 0 {
		return []VersionRecord{}, 0, nil
	}

	// Fetch paginated records.
	rows, err := pool.Query(ctx,
		`SELECT "name","ref_doctype","docname","data","owner","creation" FROM tab_version WHERE "ref_doctype" = $1 AND "docname" = $2 ORDER BY "creation" DESC LIMIT $3 OFFSET $4`,
		doctype, docname, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: query versions (doctype=%q docname=%q): %w", doctype, docname, err)
	}
	defer rows.Close()

	var records []VersionRecord
	for rows.Next() {
		var rec VersionRecord
		var dataBytes []byte
		if err := rows.Scan(&rec.Name, &rec.RefDoctype, &rec.DocName, &dataBytes, &rec.Owner, &rec.Creation); err != nil {
			return nil, 0, fmt.Errorf("crud: scan version row: %w", err)
		}
		if err := json.Unmarshal(dataBytes, &rec.Data); err != nil {
			return nil, 0, fmt.Errorf("crud: unmarshal version data: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("crud: version rows iteration: %w", err)
	}

	return records, total, nil
}
