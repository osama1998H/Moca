//go:build integration

package notify

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/queue"
)

// ── connection defaults ─────────────────────────────────────────────────────

const (
	notifyTestHost     = "localhost"
	notifyTestPort     = 5433
	notifyTestUser     = "moca"
	notifyTestPassword = "moca_test"
	notifyTestDB       = "moca_test"
	notifyTestSchema   = "tenant_notify_integ"
	notifySiteName     = "notify_integ"
	notifyRedisPort    = 6380
)

var (
	notifyPool   *pgxpool.Pool
	notifyRedis  *redis.Client
	notifyLogger *slog.Logger
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = notifyTestHost
	}
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable&search_path=%s",
			notifyTestUser, notifyTestPassword, host, notifyTestPort,
			notifyTestDB, notifyTestSchema,
		)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create pool: %v\n", err)
		os.Exit(0)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Create the schema.
	schema := pgx.Identifier{notifyTestSchema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create schema: %v\n", err)
		os.Exit(1)
	}

	// Create required tables.
	for _, ddl := range []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			"name"          TEXT PRIMARY KEY,
			"for_user"      TEXT NOT NULL,
			"type"          TEXT NOT NULL DEFAULT 'info',
			"subject"       TEXT NOT NULL DEFAULT '',
			"message"       TEXT NOT NULL DEFAULT '',
			"document_type" TEXT NOT NULL DEFAULT '',
			"document_name" TEXT NOT NULL DEFAULT '',
			"read"          BOOLEAN NOT NULL DEFAULT false,
			"email_sent"    BOOLEAN NOT NULL DEFAULT false,
			"creation"      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, pgx.Identifier{notifyTestSchema, "tab_notification"}.Sanitize()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			"name"             TEXT PRIMARY KEY,
			"document_type"    TEXT NOT NULL,
			"event"            TEXT NOT NULL,
			"recipients"       TEXT NOT NULL DEFAULT '',
			"subject_template" TEXT NOT NULL DEFAULT '',
			"message_template" TEXT NOT NULL DEFAULT '',
			"send_email"       BOOLEAN NOT NULL DEFAULT false,
			"send_notification" BOOLEAN NOT NULL DEFAULT true,
			"enabled"          BOOLEAN NOT NULL DEFAULT true
		)`, pgx.Identifier{notifyTestSchema, "tab_notification_settings"}.Sanitize()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			"name"      TEXT PRIMARY KEY,
			"full_name" TEXT NOT NULL DEFAULT '',
			"enabled"   BOOLEAN NOT NULL DEFAULT true
		)`, pgx.Identifier{notifyTestSchema, "tab_user"}.Sanitize()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			"name"        TEXT PRIMARY KEY,
			"parent"      TEXT NOT NULL,
			"parenttype"  TEXT NOT NULL DEFAULT 'User',
			"parentfield" TEXT NOT NULL DEFAULT 'roles',
			"role"        TEXT NOT NULL
		)`, pgx.Identifier{notifyTestSchema, "tab_has_role"}.Sanitize()),
	} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: create table: %v\n", err)
			os.Exit(1)
		}
	}

	notifyPool = pool
	notifyLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Redis.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisHost := os.Getenv("REDIS_HOST")
		if redisHost == "" {
			redisHost = "localhost"
		}
		redisAddr = fmt.Sprintf("%s:%d", redisHost, notifyRedisPort)
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 1}) // DB 1 = queue
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: Redis unavailable at %s: %v — queue tests skipped\n", redisAddr, err)
	} else {
		notifyRedis = rc
	}

	exitCode := m.Run()

	// Teardown.
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema: %v\n", err)
	}
	if notifyRedis != nil {
		notifyRedis.Close()
	}

	os.Exit(exitCode)
}

// ── Tests ───────────────────────────────────────────────────────────────────

// TestNotifyInteg_InAppCRUD tests Create, GetUnread, and MarkRead against PG.
func TestNotifyInteg_InAppCRUD(t *testing.T) {
	ctx := context.Background()
	notifier := NewInAppNotifier(notifyLogger)

	// Create a notification.
	notif := Notification{
		ForUser:      "alice@test.dev",
		Type:         "info",
		Subject:      "New Order",
		Message:      "Order ORD-001 created",
		DocumentType: "Order",
		DocumentName: "ORD-001",
	}

	name, err := notifier.Create(ctx, notifyPool, notifyTestSchema, notif)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if name == "" {
		t.Fatal("Create returned empty name")
	}

	// Get unread for the user.
	unread, count, err := notifier.GetUnread(ctx, notifyPool, notifyTestSchema, "alice@test.dev", 10)
	if err != nil {
		t.Fatalf("GetUnread: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least 1 unread notification")
	}

	found := false
	for _, n := range unread {
		if n.Name == name {
			found = true
			if n.Subject != "New Order" {
				t.Errorf("subject = %q, want %q", n.Subject, "New Order")
			}
			if n.Read {
				t.Error("notification should be unread")
			}
		}
	}
	if !found {
		t.Errorf("notification %q not found in unread list", name)
	}

	// Mark as read.
	if err := notifier.MarkRead(ctx, notifyPool, notifyTestSchema, "alice@test.dev", name); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// Verify it's marked as read.
	unread, count, err = notifier.GetUnread(ctx, notifyPool, notifyTestSchema, "alice@test.dev", 10)
	if err != nil {
		t.Fatalf("GetUnread after mark: %v", err)
	}
	for _, n := range unread {
		if n.Name == name {
			t.Error("notification should no longer appear in unread list after MarkRead")
		}
	}
	_ = count
}

// TestNotifyInteg_EmailDeliveryHandler verifies that the email delivery job
// handler calls the EmailSender with correct parameters.
func TestNotifyInteg_EmailDeliveryHandler(t *testing.T) {
	mock := &mockEmailSender{}
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, mock, notifyLogger)

	job := queue.Job{
		ID:   "test-job-001",
		Site: notifySiteName,
		Type: JobTypeEmailDelivery,
		Payload: map[string]any{
			"to":            []any{"bob@test.dev"},
			"subject":       "Test Subject",
			"html_body":     "<p>Hello</p>",
			"text_body":     "Hello",
			"document_type": "Order",
			"document_name": "ORD-002",
		},
	}

	err := nd.EmailDeliveryHandler(context.Background(), job)
	if err != nil {
		t.Fatalf("EmailDeliveryHandler: %v", err)
	}

	if !mock.called {
		t.Fatal("expected Send to be called")
	}

	msg := mock.lastMsg
	if len(msg.To) != 1 || msg.To[0] != "bob@test.dev" {
		t.Errorf("To = %v, want [bob@test.dev]", msg.To)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Test Subject")
	}
	if msg.HTMLBody != "<p>Hello</p>" {
		t.Errorf("HTMLBody = %q, want %q", msg.HTMLBody, "<p>Hello</p>")
	}
}

// TestNotifyInteg_EmailDeliveryHandler_NoSender verifies that the handler
// gracefully ACKs when no email sender is configured (returns nil, not error).
func TestNotifyInteg_EmailDeliveryHandler_NoSender(t *testing.T) {
	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, notifyLogger)

	job := queue.Job{
		ID:   "test-job-nosender",
		Site: notifySiteName,
		Type: JobTypeEmailDelivery,
		Payload: map[string]any{
			"to":      []any{"bob@test.dev"},
			"subject": "Should be dropped",
		},
	}

	// Should return nil (ACK), not an error.
	err := nd.EmailDeliveryHandler(context.Background(), job)
	if err != nil {
		t.Errorf("expected nil error for missing sender, got: %v", err)
	}
}

// TestNotifyInteg_LoadSettings verifies that loadSettings queries
// NotificationSettings from the database correctly.
func TestNotifyInteg_LoadSettings(t *testing.T) {
	ctx := context.Background()

	// Insert a notification setting.
	_, err := notifyPool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s ("name", "document_type", "event", "recipients", "subject_template", "message_template", "send_email", "send_notification", "enabled")
		 VALUES ('ns_load_test', 'Order', 'on_create', 'admin@test.dev', 'New {{.DocType}}', 'Order {{.Name}} created', true, true, true)
		 ON CONFLICT ("name") DO NOTHING`,
		pgx.Identifier{notifyTestSchema, "tab_notification_settings"}.Sanitize(),
	))
	if err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, notifyLogger)

	settings, err := nd.loadSettings(ctx, notifyPool, notifyTestSchema, "Order", "on_create")
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if len(settings) == 0 {
		t.Fatal("expected at least 1 setting")
	}

	s := settings[0]
	if s.DocumentType != "Order" {
		t.Errorf("DocumentType = %q, want %q", s.DocumentType, "Order")
	}
	if s.Event != "on_create" {
		t.Errorf("Event = %q, want %q", s.Event, "on_create")
	}
	if s.Recipients != "admin@test.dev" {
		t.Errorf("Recipients = %q, want %q", s.Recipients, "admin@test.dev")
	}
	if !s.SendEmail {
		t.Error("SendEmail should be true")
	}

	// Query for a non-matching event should return empty.
	settings, err = nd.loadSettings(ctx, notifyPool, notifyTestSchema, "Order", "on_cancel")
	if err != nil {
		t.Fatalf("loadSettings for on_cancel: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected 0 settings for on_cancel, got %d", len(settings))
	}
}

// TestNotifyInteg_ResolveRecipients verifies that role-based recipient
// expansion works against a real database.
func TestNotifyInteg_ResolveRecipients(t *testing.T) {
	ctx := context.Background()

	// Insert a user with a role.
	for _, stmt := range []string{
		`INSERT INTO "tab_user" ("name", "full_name", "enabled") VALUES ('resolve-user@test.dev', 'Resolve User', true) ON CONFLICT DO NOTHING`,
		`INSERT INTO "tab_has_role" ("name", "parent", "parenttype", "parentfield", "role") VALUES ('resolve-user_Reviewer', 'resolve-user@test.dev', 'User', 'roles', 'Reviewer') ON CONFLICT DO NOTHING`,
	} {
		if _, err := notifyPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("insert fixture: %v", err)
		}
	}

	nd := NewNotificationDispatcher(nil, nil, nil, nil, nil, nil, notifyLogger)

	// Direct email recipient.
	recipients, err := nd.resolveRecipients(ctx, notifyPool, notifyTestSchema, "direct@test.dev")
	if err != nil {
		t.Fatalf("resolveRecipients (direct): %v", err)
	}
	if len(recipients) != 1 || recipients[0] != "direct@test.dev" {
		t.Errorf("direct: got %v, want [direct@test.dev]", recipients)
	}

	// Role-based expansion.
	recipients, err = nd.resolveRecipients(ctx, notifyPool, notifyTestSchema, "Reviewer")
	if err != nil {
		t.Fatalf("resolveRecipients (role): %v", err)
	}
	found := false
	for _, r := range recipients {
		if r == "resolve-user@test.dev" {
			found = true
		}
	}
	if !found {
		t.Errorf("role expansion: expected resolve-user@test.dev in %v", recipients)
	}

	// Mixed: direct email + role.
	recipients, err = nd.resolveRecipients(ctx, notifyPool, notifyTestSchema, "other@test.dev, Reviewer")
	if err != nil {
		t.Fatalf("resolveRecipients (mixed): %v", err)
	}
	if len(recipients) < 2 {
		t.Errorf("mixed: expected at least 2 recipients, got %d", len(recipients))
	}
}

// TestNotifyInteg_EnqueueEmail verifies that the dispatcher's enqueueEmail
// writes a job to the Redis stream.
func TestNotifyInteg_EnqueueEmail(t *testing.T) {
	if notifyRedis == nil {
		t.Skip("Redis unavailable — skipping queue test")
	}

	ctx := context.Background()
	producer := queue.NewProducer(notifyRedis, notifyLogger)

	nd := NewNotificationDispatcher(producer, nil, nil, nil, nil, nil, notifyLogger)

	// Flush the stream.
	streamKey := queue.StreamKey(notifySiteName, queue.QueueCritical)
	notifyRedis.Del(ctx, streamKey)

	// Enqueue.
	nd.enqueueEmail(ctx, notifySiteName, "enqueue-test@test.dev", "Test Subject", "<p>Body</p>", "Order", "ORD-001")

	// Check Redis stream.
	entries, err := notifyRedis.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 entry in the stream")
	}
}

// mockEmailSender is defined in dispatcher_test.go and reused here.
