package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/osama1998H/moca/internal/config"
)

func TestNewEmailSender_SMTPValid(t *testing.T) {
	cfg := config.EmailConfig{
		Provider: "smtp",
		SMTP: config.SMTPConfig{
			Host:     "smtp.example.com",
			Port:     587,
			FromAddr: "noreply@example.com",
		},
	}
	sender, err := NewEmailSender(cfg)
	if err != nil {
		t.Fatalf("NewEmailSender() error = %v", err)
	}
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if _, ok := sender.(*SMTPSender); !ok {
		t.Fatalf("expected *SMTPSender, got %T", sender)
	}
}

func TestNewEmailSender_SMTPEmptyHost(t *testing.T) {
	cfg := config.EmailConfig{
		Provider: "smtp",
		SMTP:     config.SMTPConfig{Host: ""},
	}
	sender, err := NewEmailSender(cfg)
	if err != nil {
		t.Fatalf("NewEmailSender() error = %v", err)
	}
	if sender != nil {
		t.Fatal("expected nil sender for empty SMTP host")
	}
}

func TestNewEmailSender_EmptyProvider(t *testing.T) {
	cfg := config.EmailConfig{
		Provider: "",
		SMTP: config.SMTPConfig{
			Host:     "smtp.example.com",
			Port:     587,
			FromAddr: "noreply@example.com",
		},
	}
	sender, err := NewEmailSender(cfg)
	if err != nil {
		t.Fatalf("NewEmailSender() error = %v", err)
	}
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

func TestNewEmailSender_SESMissingRegion(t *testing.T) {
	cfg := config.EmailConfig{
		Provider: "ses",
		SES:      config.SESConfig{Region: ""},
	}
	_, err := NewEmailSender(cfg)
	if err == nil {
		t.Fatal("expected error for missing SES region")
	}
}

func TestNewEmailSender_SESValid(t *testing.T) {
	cfg := config.EmailConfig{
		Provider: "ses",
		SES:      config.SESConfig{Region: "us-east-1", FromAddr: "noreply@example.com"},
	}
	sender, err := NewEmailSender(cfg)
	if err != nil {
		t.Fatalf("NewEmailSender() error = %v", err)
	}
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

func TestNewEmailSender_UnknownProvider(t *testing.T) {
	cfg := config.EmailConfig{Provider: "unknown"}
	_, err := NewEmailSender(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestSMTPSender_DefaultPort(t *testing.T) {
	sender, err := newSMTPSender(config.SMTPConfig{
		Host:     "smtp.example.com",
		FromAddr: "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("newSMTPSender() error = %v", err)
	}
	if sender.addr != "smtp.example.com:587" {
		t.Errorf("addr = %q, want %q", sender.addr, "smtp.example.com:587")
	}
}

func TestSMTPSender_NoRecipients(t *testing.T) {
	sender, _ := newSMTPSender(config.SMTPConfig{
		Host:     "smtp.example.com",
		Port:     587,
		FromAddr: "noreply@example.com",
	})
	err := sender.Send(context.Background(), EmailMessage{
		Subject: "Test",
	})
	if err == nil {
		t.Fatal("expected error for no recipients")
	}
	if !strings.Contains(err.Error(), "no recipients") {
		t.Errorf("error = %q, want to contain 'no recipients'", err.Error())
	}
}

func TestSESSender_NotImplemented(t *testing.T) {
	sender, _ := newSESSender(config.SESConfig{
		Region:   "us-east-1",
		FromAddr: "noreply@example.com",
	})
	err := sender.Send(context.Background(), EmailMessage{
		To:      []string{"user@example.com"},
		Subject: "Test",
	})
	if err == nil {
		t.Fatal("expected error for SES stub")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error = %q, want to contain 'not yet implemented'", err.Error())
	}
}

func TestBuildMIMEMessage_TextOnly(t *testing.T) {
	msg := EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "Test Subject",
		TextBody: "Hello World",
	}
	body, err := buildMIMEMessage("", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "text/plain") {
		t.Error("should contain text/plain content type")
	}
	if !strings.Contains(s, "Hello World") {
		t.Error("should contain body text")
	}
}

func TestBuildMIMEMessage_HTMLOnly(t *testing.T) {
	msg := EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "Test",
		HTMLBody: "<p>Hello</p>",
	}
	body, err := buildMIMEMessage("", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "text/html") {
		t.Error("should contain text/html content type")
	}
}

func TestBuildMIMEMessage_Alternative(t *testing.T) {
	msg := EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "Test",
		TextBody: "Hello",
		HTMLBody: "<p>Hello</p>",
	}
	body, err := buildMIMEMessage("", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("should use multipart/alternative")
	}
	if !strings.Contains(s, "text/plain") {
		t.Error("should contain text/plain part")
	}
	if !strings.Contains(s, "text/html") {
		t.Error("should contain text/html part")
	}
}

func TestBuildMIMEMessage_WithAttachment(t *testing.T) {
	msg := EmailMessage{
		To:       []string{"user@example.com"},
		Subject:  "Test",
		TextBody: "See attached",
		Attachments: []Attachment{
			{
				Filename:    "report.pdf",
				ContentType: "application/pdf",
				Data:        []byte("fake pdf content"),
			},
		},
	}
	body, err := buildMIMEMessage("", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "multipart/mixed") {
		t.Error("should use multipart/mixed for attachments")
	}
	if !strings.Contains(s, "report.pdf") {
		t.Error("should contain attachment filename")
	}
}

func TestBuildMIMEMessage_FromName(t *testing.T) {
	msg := EmailMessage{
		To:      []string{"user@example.com"},
		Subject: "Test",
	}
	body, err := buildMIMEMessage("Moca System", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "Moca System") {
		t.Error("should contain from name")
	}
}

func TestBuildMIMEMessage_CC(t *testing.T) {
	msg := EmailMessage{
		To:      []string{"to@example.com"},
		CC:      []string{"cc@example.com"},
		Subject: "Test",
	}
	body, err := buildMIMEMessage("", "noreply@example.com", msg)
	if err != nil {
		t.Fatalf("buildMIMEMessage() error = %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "Cc: cc@example.com") {
		t.Error("should contain CC header")
	}
}
