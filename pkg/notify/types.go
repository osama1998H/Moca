package notify

import "time"

// JobTypeEmailDelivery is the queue job type for background email sending.
const JobTypeEmailDelivery = "notification.email"

// EmailMessage is the payload for sending an email.
type EmailMessage struct {
	Headers     map[string]string
	Subject     string
	HTMLBody    string
	TextBody    string
	To          []string
	CC          []string
	BCC         []string
	Attachments []Attachment
}

// Attachment is a file attached to an email.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Notification represents an in-app notification record.
type Notification struct {
	Creation     time.Time `json:"creation"`
	Name         string    `json:"name"`
	ForUser      string    `json:"for_user"`
	Type         string    `json:"type"` // info, warning, error, success
	Subject      string    `json:"subject"`
	Message      string    `json:"message"`
	DocumentType string    `json:"document_type,omitempty"`
	DocumentName string    `json:"document_name,omitempty"`
	Read         bool      `json:"read"`
	EmailSent    bool      `json:"email_sent"`
}

// NotificationSetting represents a notification dispatch rule loaded from
// the tab_notification_settings table.
type NotificationSetting struct {
	Name             string `json:"name"`
	DocumentType     string `json:"document_type"`
	Event            string `json:"event"` // on_create, on_update, on_submit, on_cancel
	Recipients       string `json:"recipients"`
	SubjectTemplate  string `json:"subject_template"`
	MessageTemplate  string `json:"message_template"`
	SendEmail        bool   `json:"send_email"`
	SendNotification bool   `json:"send_notification"`
	Enabled          bool   `json:"enabled"`
}
