package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"

	"github.com/osama1998H/moca/internal/config"
)

// EmailSender is the interface for sending email messages.
type EmailSender interface {
	Send(ctx context.Context, msg EmailMessage) error
}

// NewEmailSender creates an EmailSender based on the provider config.
// Returns (nil, nil) when no email configuration is provided, allowing
// the notification system to degrade gracefully to in-app only.
func NewEmailSender(cfg config.EmailConfig) (EmailSender, error) {
	switch cfg.Provider {
	case "smtp", "":
		if cfg.SMTP.Host == "" {
			return nil, nil // no SMTP configured
		}
		return newSMTPSender(cfg.SMTP)
	case "ses":
		if cfg.SES.Region == "" {
			return nil, fmt.Errorf("notify: SES region is required")
		}
		return newSESSender(cfg.SES)
	default:
		return nil, fmt.Errorf("notify: unknown email provider %q", cfg.Provider)
	}
}

// ── SMTP ────────────────────────────────────────────────────────────────────

// SMTPSender sends email via SMTP with optional STARTTLS.
type SMTPSender struct {
	host     string
	addr     string
	user     string
	password string
	fromName string
	fromAddr string
	useTLS   bool
}

func newSMTPSender(cfg config.SMTPConfig) (*SMTPSender, error) {
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	return &SMTPSender{
		host:     cfg.Host,
		addr:     fmt.Sprintf("%s:%d", cfg.Host, port),
		user:     cfg.User,
		password: cfg.Password,
		fromName: cfg.FromName,
		fromAddr: cfg.FromAddr,
		useTLS:   cfg.UseTLS,
	}, nil
}

// Send sends an email message via SMTP.
func (s *SMTPSender) Send(_ context.Context, msg EmailMessage) error {
	from := s.fromAddr
	if msg.Headers != nil {
		if f := msg.Headers["From"]; f != "" {
			from = f
		}
	}

	all := make([]string, 0, len(msg.To)+len(msg.CC)+len(msg.BCC))
	all = append(all, msg.To...)
	all = append(all, msg.CC...)
	all = append(all, msg.BCC...)
	if len(all) == 0 {
		return fmt.Errorf("notify: smtp: no recipients")
	}

	body, err := buildMIMEMessage(s.fromName, from, msg)
	if err != nil {
		return fmt.Errorf("notify: smtp: build message: %w", err)
	}

	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.password, s.host)
	}

	if s.useTLS {
		return s.sendTLS(from, all, body, auth)
	}
	return smtp.SendMail(s.addr, auth, from, all, body)
}

// sendTLS establishes a TLS connection to the SMTP server. It first tries
// STARTTLS; if the server already speaks TLS on connect (e.g., port 465),
// it wraps the raw connection.
func (s *SMTPSender) sendTLS(from string, to []string, body []byte, auth smtp.Auth) error {
	conn, err := tls.Dial("tcp", s.addr, &tls.Config{ServerName: s.host})
	if err != nil {
		// Fallback: try plain connection with STARTTLS upgrade.
		plainConn, dialErr := net.Dial("tcp", s.addr)
		if dialErr != nil {
			return fmt.Errorf("notify: smtp: dial: %w", err)
		}
		client, clientErr := smtp.NewClient(plainConn, s.host)
		if clientErr != nil {
			_ = plainConn.Close()
			return fmt.Errorf("notify: smtp: new client: %w", clientErr)
		}
		if tlsErr := client.StartTLS(&tls.Config{ServerName: s.host}); tlsErr != nil {
			_ = client.Close()
			return fmt.Errorf("notify: smtp: starttls: %w", tlsErr)
		}
		return s.sendViaClient(client, from, to, body, auth)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("notify: smtp: new client: %w", err)
	}
	return s.sendViaClient(client, from, to, body, auth)
}

func (s *SMTPSender) sendViaClient(c *smtp.Client, from string, to []string, body []byte, auth smtp.Auth) error {
	defer func() { _ = c.Quit() }()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("notify: smtp: auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("notify: smtp: mail from: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("notify: smtp: rcpt to %s: %w", addr, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("notify: smtp: data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("notify: smtp: write body: %w", err)
	}
	return w.Close()
}

// buildMIMEMessage constructs an RFC 2045 MIME message with text/plain and
// text/html alternatives, plus optional attachments.
func buildMIMEMessage(fromName, fromAddr string, msg EmailMessage) ([]byte, error) {
	var b strings.Builder

	// From header.
	if fromName != "" {
		fmt.Fprintf(&b, "From: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", fromName), fromAddr)
	} else {
		fmt.Fprintf(&b, "From: %s\r\n", fromAddr)
	}

	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	if len(msg.CC) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(msg.CC, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	b.WriteString("MIME-Version: 1.0\r\n")

	// Custom headers.
	for k, v := range msg.Headers {
		if k == "From" || k == "To" || k == "Cc" || k == "Subject" {
			continue // already set
		}
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}

	hasAttachments := len(msg.Attachments) > 0
	hasAlternative := msg.HTMLBody != "" && msg.TextBody != ""

	const mixedBoundary = "moca-mixed-boundary"
	const altBoundary = "moca-alt-boundary"

	switch {
	case hasAttachments:
		fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", mixedBoundary)
		fmt.Fprintf(&b, "--%s\r\n", mixedBoundary)
		if hasAlternative {
			writeAlternative(&b, altBoundary, msg.TextBody, msg.HTMLBody)
		} else if msg.HTMLBody != "" {
			b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
			b.WriteString(msg.HTMLBody)
			b.WriteString("\r\n")
		} else {
			b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
			b.WriteString(msg.TextBody)
			b.WriteString("\r\n")
		}
		for _, att := range msg.Attachments {
			fmt.Fprintf(&b, "--%s\r\n", mixedBoundary)
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			fmt.Fprintf(&b, "Content-Type: %s; name=%q\r\n", ct, att.Filename)
			b.WriteString("Content-Transfer-Encoding: base64\r\n")
			fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n\r\n", att.Filename)
			b.WriteString(base64.StdEncoding.EncodeToString(att.Data))
			b.WriteString("\r\n")
		}
		fmt.Fprintf(&b, "--%s--\r\n", mixedBoundary)

	case hasAlternative:
		writeAlternative(&b, altBoundary, msg.TextBody, msg.HTMLBody)

	case msg.HTMLBody != "":
		b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
		b.WriteString(msg.HTMLBody)

	default:
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		b.WriteString(msg.TextBody)
	}

	return []byte(b.String()), nil
}

func writeAlternative(b *strings.Builder, boundary, text, html string) {
	fmt.Fprintf(b, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)
	fmt.Fprintf(b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(text)
	fmt.Fprintf(b, "\r\n--%s\r\n", boundary)
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	b.WriteString(html)
	fmt.Fprintf(b, "\r\n--%s--\r\n", boundary)
}

// ── SES ─────────────────────────────────────────────────────────────────────

// SESSender sends email via AWS SES. It uses the AWS SDK v2 HTTP API directly
// to avoid pulling in the full SES dependency. For production SES usage, replace
// with the official SDK client.
type SESSender struct {
	region   string
	fromAddr string
}

func newSESSender(cfg config.SESConfig) (*SESSender, error) {
	return &SESSender{
		region:   cfg.Region,
		fromAddr: cfg.FromAddr,
	}, nil
}

// Send sends an email via SES. This is a stub — full SES SDK integration is
// deferred per the MS-22 plan to avoid pulling in the AWS SDK dependency chain.
// SMTP is the primary and required provider.
func (s *SESSender) Send(_ context.Context, _ EmailMessage) error {
	return fmt.Errorf("notify: SES sender not yet implemented — use SMTP provider")
}
