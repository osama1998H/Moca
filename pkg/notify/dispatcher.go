package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
)

const (
	notifyHookPriority = 850 // after business logic (500), before webhooks (900)
	notifyAppName      = "moca_notifications"
)

// notificationEvents maps NotificationSettings.Event values to DocEvent constants.
var notificationEvents = map[string]document.DocEvent{
	"on_create": document.EventAfterInsert,
	"on_update": document.EventAfterSave,
	"on_submit": document.EventOnSubmit,
	"on_cancel": document.EventOnCancel,
}

// NotificationDispatcher listens to document lifecycle events and creates
// notifications based on NotificationSettings rules. It follows the same
// pattern as WebhookDispatcher: global hooks, queue-based email delivery,
// best-effort dispatch (errors logged, not propagated).
type NotificationDispatcher struct {
	producer    *queue.Producer
	db          *orm.DBManager
	notifier    *InAppNotifier
	renderer    *TemplateRenderer
	redisPubSub *redis.Client
	emailSender EmailSender // may be nil if email is not configured
	logger      *slog.Logger
}

// NewNotificationDispatcher creates a NotificationDispatcher.
func NewNotificationDispatcher(
	producer *queue.Producer,
	db *orm.DBManager,
	notifier *InAppNotifier,
	renderer *TemplateRenderer,
	redisPubSub *redis.Client,
	emailSender EmailSender,
	logger *slog.Logger,
) *NotificationDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationDispatcher{
		producer:    producer,
		db:          db,
		notifier:    notifier,
		renderer:    renderer,
		redisPubSub: redisPubSub,
		emailSender: emailSender,
		logger:      logger,
	}
}

// RegisterHooks registers global document event hooks on the HookRegistry
// for every event that notifications can trigger on.
func (nd *NotificationDispatcher) RegisterHooks(registry *hooks.HookRegistry) {
	for eventName, docEvent := range notificationEvents {
		registry.RegisterGlobal(docEvent, hooks.PrioritizedHandler{
			Handler:  nd.makeHookHandler(eventName),
			AppName:  notifyAppName,
			Priority: notifyHookPriority,
		})
	}
	nd.logger.Info("notification hooks registered",
		slog.Int("events", len(notificationEvents)),
	)
}

// makeHookHandler returns a DocEventHandler for the given event name.
// The handler loads matching NotificationSettings, resolves recipients,
// renders templates, creates in-app notifications, and enqueues email jobs.
// It never returns an error — notifications must not block document lifecycle.
func (nd *NotificationDispatcher) makeHookHandler(eventName string) hooks.DocEventHandler {
	return func(ctx *document.DocContext, doc document.Document) error {
		if ctx == nil || ctx.Site == nil || ctx.Site.Pool == nil {
			return nil
		}

		site := ctx.Site.Name
		schema := ctx.Site.DBSchema
		pool := ctx.Site.Pool
		mt := doc.Meta()
		if mt == nil {
			return nil
		}

		settings, err := nd.loadSettings(ctx, pool, schema, mt.Name, eventName)
		if err != nil {
			nd.logger.Error("notify: load settings failed",
				slog.String("site", site),
				slog.String("doctype", mt.Name),
				slog.String("event", eventName),
				slog.String("error", err.Error()),
			)
			return nil
		}
		if len(settings) == 0 {
			return nil
		}

		// Build template data from document fields.
		tmplData := nd.buildTemplateData(ctx, doc, eventName)

		for _, setting := range settings {
			nd.processSetting(ctx, pool, schema, site, doc, setting, tmplData)
		}

		return nil
	}
}

// processSetting handles a single NotificationSetting: resolves recipients,
// renders templates, creates notifications and enqueues emails.
func (nd *NotificationDispatcher) processSetting(
	ctx context.Context,
	pool *pgxpool.Pool,
	schema, site string,
	doc document.Document,
	setting NotificationSetting,
	tmplData map[string]any,
) {
	if setting.Recipients == "" {
		return
	}

	// Resolve recipients: expand roles to user emails.
	recipients, err := nd.resolveRecipients(ctx, pool, schema, setting.Recipients)
	if err != nil {
		nd.logger.Error("notify: resolve recipients failed",
			slog.String("site", site),
			slog.String("setting", setting.Name),
			slog.String("error", err.Error()),
		)
		return
	}
	if len(recipients) == 0 {
		return
	}

	// Render subject and message templates.
	subject, err := nd.renderer.RenderString(setting.SubjectTemplate, tmplData)
	if err != nil {
		nd.logger.Error("notify: render subject failed",
			slog.String("setting", setting.Name),
			slog.String("error", err.Error()),
		)
		subject = fmt.Sprintf("%s: %s", tmplData["DocType"], tmplData["Name"])
	}
	if subject == "" {
		subject = fmt.Sprintf("%s: %s", tmplData["DocType"], tmplData["Name"])
	}

	message, err := nd.renderer.RenderString(setting.MessageTemplate, tmplData)
	if err != nil {
		nd.logger.Error("notify: render message failed",
			slog.String("setting", setting.Name),
			slog.String("error", err.Error()),
		)
		message = ""
	}

	docType := ""
	docName := ""
	if mt := doc.Meta(); mt != nil {
		docType = mt.Name
	}
	docName = doc.Name()

	for _, userEmail := range recipients {
		// Create in-app notification.
		if setting.SendNotification {
			notif := Notification{
				ForUser:      userEmail,
				Type:         "info",
				Subject:      subject,
				Message:      message,
				DocumentType: docType,
				DocumentName: docName,
				EmailSent:    setting.SendEmail && nd.emailSender != nil,
			}
			if _, err := nd.notifier.Create(ctx, pool, schema, notif); err != nil {
				nd.logger.Error("notify: create in-app notification failed",
					slog.String("user", userEmail),
					slog.String("error", err.Error()),
				)
			}

			// Publish real-time notification via Redis PubSub.
			nd.publishRealTime(ctx, site, userEmail, subject, message, docType, docName)
		}

		// Enqueue email delivery job.
		if setting.SendEmail && nd.emailSender != nil {
			nd.enqueueEmail(ctx, site, userEmail, subject, message, docType, docName)
		}
	}
}

// buildTemplateData constructs the data map available in notification templates.
func (nd *NotificationDispatcher) buildTemplateData(ctx *document.DocContext, doc document.Document, event string) map[string]any {
	data := map[string]any{
		"Event": event,
		"Name":  doc.Name(),
	}

	if mt := doc.Meta(); mt != nil {
		data["DocType"] = mt.Name
	}
	if ctx.User != nil {
		data["User"] = ctx.User.Email
	}
	if ctx.Site != nil {
		data["Site"] = ctx.Site.Name
	}

	// Include all document field values.
	if dm, ok := doc.(*document.DynamicDoc); ok {
		for k, v := range dm.AsMap() {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	return data
}

// loadSettings queries tab_notification_settings for enabled rules matching
// the given doctype and event.
func (nd *NotificationDispatcher) loadSettings(
	ctx context.Context,
	pool *pgxpool.Pool,
	schema, doctype, event string,
) ([]NotificationSetting, error) {
	table := pgx.Identifier{schema, "tab_notification_settings"}.Sanitize()
	query := fmt.Sprintf(`SELECT "name", "document_type", "event", "recipients",
		"subject_template", "message_template", "send_email", "send_notification", "enabled"
		FROM %s WHERE "document_type" = $1 AND "event" = $2 AND "enabled" = true`, table)

	rows, err := pool.Query(ctx, query, doctype, event)
	if err != nil {
		return nil, fmt.Errorf("query notification settings: %w", err)
	}
	defer rows.Close()

	var settings []NotificationSetting
	for rows.Next() {
		var s NotificationSetting
		if err := rows.Scan(
			&s.Name, &s.DocumentType, &s.Event, &s.Recipients,
			&s.SubjectTemplate, &s.MessageTemplate,
			&s.SendEmail, &s.SendNotification, &s.Enabled,
		); err != nil {
			return nil, fmt.Errorf("scan notification setting: %w", err)
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

// resolveRecipients expands a comma-separated recipients string into email
// addresses. Tokens containing "@" are treated as direct email addresses;
// all others are treated as role names and expanded by querying tab_user
// joined with tab_has_role.
func (nd *NotificationDispatcher) resolveRecipients(
	ctx context.Context,
	pool *pgxpool.Pool,
	schema, recipientsStr string,
) ([]string, error) {
	tokens := strings.Split(recipientsStr, ",")
	seen := make(map[string]struct{})
	var result []string

	var roles []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "@") {
			if _, ok := seen[tok]; !ok {
				seen[tok] = struct{}{}
				result = append(result, tok)
			}
		} else {
			roles = append(roles, tok)
		}
	}

	// Expand roles to user emails.
	for _, role := range roles {
		emails, err := nd.expandRole(ctx, pool, schema, role)
		if err != nil {
			nd.logger.Warn("notify: expand role failed",
				slog.String("role", role),
				slog.String("error", err.Error()),
			)
			continue
		}
		for _, email := range emails {
			if _, ok := seen[email]; !ok {
				seen[email] = struct{}{}
				result = append(result, email)
			}
		}
	}

	return result, nil
}

// expandRole returns enabled user emails that have the given role.
func (nd *NotificationDispatcher) expandRole(
	ctx context.Context,
	pool *pgxpool.Pool,
	schema, role string,
) ([]string, error) {
	userTable := pgx.Identifier{schema, "tab_user"}.Sanitize()
	hrTable := pgx.Identifier{schema, "tab_has_role"}.Sanitize()

	query := fmt.Sprintf(`SELECT DISTINCT u."name"
		FROM %s u
		JOIN %s hr ON hr."parent" = u."name" AND hr."parenttype" = 'User'
		WHERE hr."role" = $1 AND u."enabled" = true`, userTable, hrTable)

	rows, err := pool.Query(ctx, query, role)
	if err != nil {
		return nil, fmt.Errorf("expand role %q: %w", role, err)
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scan user email: %w", err)
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// publishRealTime publishes a notification event to Redis PubSub for real-time
// delivery to the Desk notification bell via WebSocket.
func (nd *NotificationDispatcher) publishRealTime(
	ctx context.Context,
	site, user, subject, message, docType, docName string,
) {
	if nd.redisPubSub == nil {
		return
	}

	channel := fmt.Sprintf("pubsub:notify:%s:%s", site, user)
	payload, err := json.Marshal(map[string]any{
		"type":          "notification",
		"subject":       subject,
		"message":       message,
		"document_type": docType,
		"document_name": docName,
	})
	if err != nil {
		nd.logger.Error("notify: marshal realtime payload",
			slog.String("error", err.Error()),
		)
		return
	}

	if err := nd.redisPubSub.Publish(ctx, channel, payload).Err(); err != nil {
		nd.logger.Warn("notify: redis publish failed (best-effort)",
			slog.String("channel", channel),
			slog.String("error", err.Error()),
		)
	}
}

// enqueueEmail enqueues an email delivery job to the critical queue.
func (nd *NotificationDispatcher) enqueueEmail(
	ctx context.Context,
	site, to, subject, message, docType, docName string,
) {
	if nd.producer == nil {
		return
	}

	jobID, err := generateJobID()
	if err != nil {
		nd.logger.Error("notify: generate email job id",
			slog.String("error", err.Error()),
		)
		return
	}

	payload := map[string]any{
		"to":            []any{to},
		"subject":       subject,
		"html_body":     message,
		"text_body":     stripHTML(message),
		"site":          site,
		"document_type": docType,
		"document_name": docName,
	}

	job := queue.Job{
		ID:         jobID,
		Site:       site,
		Type:       JobTypeEmailDelivery,
		Payload:    payload,
		CreatedAt:  time.Now().UTC(),
		MaxRetries: 3,
		Timeout:    30 * time.Second,
	}

	if _, err := nd.producer.Enqueue(ctx, site, queue.QueueCritical, job); err != nil {
		nd.logger.Error("notify: enqueue email failed (best-effort)",
			slog.String("to", to),
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
	}
}

// EmailDeliveryHandler is the queue.JobHandler that processes email delivery
// jobs from the background worker pool. Register it on the worker pool with
// Handle(JobTypeEmailDelivery, dispatcher.EmailDeliveryHandler).
func (nd *NotificationDispatcher) EmailDeliveryHandler(ctx context.Context, job queue.Job) error {
	if nd.emailSender == nil {
		nd.logger.Warn("notify: email sender not configured, dropping email job",
			slog.String("job_id", job.ID),
		)
		return nil // ACK — don't retry if sender is not configured
	}

	p := job.Payload
	subject, _ := p["subject"].(string)
	htmlBody, _ := p["html_body"].(string)
	textBody, _ := p["text_body"].(string)

	var toAddrs []string
	if toList, ok := p["to"].([]any); ok {
		for _, t := range toList {
			if s, ok := t.(string); ok {
				toAddrs = append(toAddrs, s)
			}
		}
	}

	if len(toAddrs) == 0 {
		nd.logger.Warn("notify: email job has no recipients",
			slog.String("job_id", job.ID),
		)
		return nil
	}

	msg := EmailMessage{
		To:       toAddrs,
		Subject:  subject,
		HTMLBody: htmlBody,
		TextBody: textBody,
	}

	if err := nd.emailSender.Send(ctx, msg); err != nil {
		nd.logger.Error("notify: email delivery failed",
			slog.String("job_id", job.ID),
			slog.String("to", strings.Join(toAddrs, ",")),
			slog.String("error", err.Error()),
		)
		return err // return error so the job stays in PEL for retry/DLQ
	}

	nd.logger.Info("notify: email delivered",
		slog.String("job_id", job.ID),
		slog.String("to", strings.Join(toAddrs, ",")),
		slog.String("subject", subject),
	)
	return nil
}

func generateJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
