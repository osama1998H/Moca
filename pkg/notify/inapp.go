package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InAppNotifier creates, reads, and updates in-app notification records using
// direct SQL. It bypasses DocManager to avoid circular dispatch — notifications
// are triggered by document lifecycle hooks, and creating them via DocManager
// would re-enter the hook system.
type InAppNotifier struct {
	logger *slog.Logger
}

// NewInAppNotifier creates a new InAppNotifier.
func NewInAppNotifier(logger *slog.Logger) *InAppNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &InAppNotifier{logger: logger}
}

// Create inserts a new in-app notification record. Returns the generated ID.
func (n *InAppNotifier) Create(ctx context.Context, pool *pgxpool.Pool, schema string, notif Notification) (string, error) {
	if pool == nil {
		return "", fmt.Errorf("notify: inapp: nil pool")
	}

	id, err := generateNotifID()
	if err != nil {
		return "", fmt.Errorf("notify: inapp: generate id: %w", err)
	}

	now := time.Now().UTC()
	table := pgx.Identifier{schema, "tab_notification"}.Sanitize()
	query := fmt.Sprintf(`INSERT INTO %s
		("name", "for_user", "type", "subject", "message",
		 "document_type", "document_name", "read", "email_sent",
		 "owner", "creation", "modified", "modified_by", "docstatus")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, $10, 0)`, table)

	owner := "Administrator"
	if notif.ForUser != "" {
		owner = notif.ForUser
	}

	_, err = pool.Exec(ctx, query,
		id,
		notif.ForUser,
		notif.Type,
		notif.Subject,
		notif.Message,
		notif.DocumentType,
		notif.DocumentName,
		false, // read
		notif.EmailSent,
		owner,
		now,
	)
	if err != nil {
		return "", fmt.Errorf("notify: inapp: insert: %w", err)
	}

	n.logger.Debug("in-app notification created",
		slog.String("id", id),
		slog.String("for_user", notif.ForUser),
		slog.String("subject", notif.Subject),
	)
	return id, nil
}

// MarkRead marks notifications as read for the given user.
func (n *InAppNotifier) MarkRead(ctx context.Context, pool *pgxpool.Pool, schema, user string, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	if pool == nil {
		return fmt.Errorf("notify: inapp: nil pool")
	}

	table := pgx.Identifier{schema, "tab_notification"}.Sanitize()

	// Mark all as read for user.
	if len(names) == 1 && names[0] == "*" {
		query := fmt.Sprintf(`UPDATE %s SET "read" = true, "modified" = NOW() WHERE "for_user" = $1 AND "read" = false`, table)
		_, err := pool.Exec(ctx, query, user)
		if err != nil {
			return fmt.Errorf("notify: inapp: mark all read: %w", err)
		}
		return nil
	}

	query := fmt.Sprintf(`UPDATE %s SET "read" = true, "modified" = NOW() WHERE "for_user" = $1 AND "name" = ANY($2)`, table)
	_, err := pool.Exec(ctx, query, user, names)
	if err != nil {
		return fmt.Errorf("notify: inapp: mark read: %w", err)
	}
	return nil
}

// GetUnread returns unread notifications for a user, ordered by creation desc.
// It also returns the total unread count regardless of the limit.
func (n *InAppNotifier) GetUnread(ctx context.Context, pool *pgxpool.Pool, schema, user string, limit int) ([]Notification, int, error) {
	if pool == nil {
		return nil, 0, fmt.Errorf("notify: inapp: nil pool")
	}

	table := pgx.Identifier{schema, "tab_notification"}.Sanitize()

	// Count total unread.
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE "for_user" = $1 AND "read" = false`, table)
	var total int
	if err := pool.QueryRow(ctx, countQuery, user).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("notify: inapp: count unread: %w", err)
	}

	if total == 0 || limit == 0 {
		return nil, total, nil
	}

	if limit < 0 {
		limit = 20
	}

	// Fetch unread notifications.
	fetchQuery := fmt.Sprintf(`SELECT "name", "for_user", "type", "subject", "message",
		"document_type", "document_name", "read", "email_sent", "creation"
		FROM %s WHERE "for_user" = $1 AND "read" = false
		ORDER BY "creation" DESC LIMIT $2`, table)

	rows, err := pool.Query(ctx, fetchQuery, user, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("notify: inapp: fetch unread: %w", err)
	}
	defer rows.Close()

	var result []Notification
	for rows.Next() {
		var notif Notification
		var docType, docName *string
		if err := rows.Scan(
			&notif.Name, &notif.ForUser, &notif.Type,
			&notif.Subject, &notif.Message,
			&docType, &docName,
			&notif.Read, &notif.EmailSent, &notif.Creation,
		); err != nil {
			return nil, 0, fmt.Errorf("notify: inapp: scan row: %w", err)
		}
		if docType != nil {
			notif.DocumentType = *docType
		}
		if docName != nil {
			notif.DocumentName = *docName
		}
		result = append(result, notif)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("notify: inapp: rows error: %w", err)
	}

	return result, total, nil
}

func generateNotifID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
