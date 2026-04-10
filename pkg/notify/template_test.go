package notify

import (
	"strings"
	"testing"
)

func TestNewTemplateRenderer(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}
	if r == nil {
		t.Fatal("NewTemplateRenderer() returned nil")
	}
	if r.templates == nil {
		t.Fatal("templates is nil")
	}
}

func TestRender_NotificationEmail(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}

	data := map[string]any{
		"Subject":      "Test Notification",
		"Message":      "Hello World",
		"DocumentType": "SalesOrder",
		"DocumentName": "SO-001",
	}

	html, text, err := r.Render("notification_email.html", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if !strings.Contains(html, "Test Notification") {
		t.Error("HTML should contain subject")
	}
	if !strings.Contains(html, "Hello World") {
		t.Error("HTML should contain message")
	}
	if !strings.Contains(html, "SalesOrder") {
		t.Error("HTML should contain document type")
	}
	if !strings.Contains(html, "SO-001") {
		t.Error("HTML should contain document name")
	}

	if text == "" {
		t.Error("text body should not be empty")
	}
	if strings.Contains(text, "<") {
		t.Error("text body should not contain HTML tags")
	}
}

func TestRender_PasswordReset(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}

	data := map[string]any{
		"FullName":  "John Doe",
		"ResetLink": "https://example.com/reset?token=abc",
		"ExpiresIn": "1 hour",
	}

	html, _, err := r.Render("password_reset.html", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(html, "John Doe") {
		t.Error("should contain full name")
	}
	if !strings.Contains(html, "reset?token=abc") {
		t.Error("should contain reset link")
	}
}

func TestRender_Welcome(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}

	data := map[string]any{
		"FullName": "Jane Doe",
		"Email":    "jane@example.com",
		"LoginURL": "https://example.com/login",
	}

	html, _, err := r.Render("welcome.html", data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(html, "Jane Doe") {
		t.Error("should contain full name")
	}
	if !strings.Contains(html, "jane@example.com") {
		t.Error("should contain email")
	}
}

func TestRender_MissingTemplate(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}

	_, _, err = r.Render("nonexistent.html", nil)
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestRenderString(t *testing.T) {
	r, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}

	tests := []struct {
		name   string
		tmpl   string
		data   any
		want   string
		hasErr bool
	}{
		{
			name: "simple substitution",
			tmpl: "{{.DocType}}: {{.Name}} was created",
			data: map[string]any{"DocType": "SalesOrder", "Name": "SO-001"},
			want: "SalesOrder: SO-001 was created",
		},
		{
			name: "empty template",
			tmpl: "",
			data: nil,
			want: "",
		},
		{
			name:   "invalid template syntax",
			tmpl:   "{{.Broken",
			data:   nil,
			hasErr: true,
		},
		{
			name: "missing field uses zero value",
			tmpl: "Hello {{.Name}}",
			data: map[string]any{},
			want: "Hello <no value>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.RenderString(tt.tmpl, tt.data)
			if tt.hasErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<b>Bold</b> and <i>italic</i>", "Bold and italic"},
		{"  spaces  between  ", "spaces between"},
		{"no tags", "no tags"},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripHTML(tt.input)
		if got != tt.want {
			t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
