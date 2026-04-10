package notify

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	ttmpl "text/template"
)

//go:embed templates/*.html
var defaultTemplateFS embed.FS

// TemplateRenderer renders notification email templates.
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer parses all embedded default templates.
func NewTemplateRenderer() (*TemplateRenderer, error) {
	tmpl, err := template.New("").ParseFS(defaultTemplateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("notify: parse templates: %w", err)
	}
	return &TemplateRenderer{templates: tmpl}, nil
}

// Render executes the named template with the given data and returns
// the HTML body and a plain-text version derived by stripping HTML tags.
func (r *TemplateRenderer) Render(name string, data any) (html, text string, err error) {
	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", "", fmt.Errorf("notify: render %q: %w", name, err)
	}
	htmlOut := buf.String()
	return htmlOut, stripHTML(htmlOut), nil
}

// RenderString renders an inline Go template string (used for user-defined
// subject and message templates from NotificationSettings). Uses text/template
// to avoid HTML entity escaping in subjects.
func (r *TemplateRenderer) RenderString(tmplStr string, data any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	t, err := ttmpl.New("inline").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("notify: parse inline template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("notify: render inline template: %w", err)
	}
	return buf.String(), nil
}

// stripHTML removes HTML tags and collapses whitespace to produce a text/plain
// version of an HTML email body.
func stripHTML(s string) string {
	// Remove HTML tags.
	s = reTag.ReplaceAllString(s, " ")
	// Collapse whitespace.
	s = reWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var (
	reTag        = regexp.MustCompile(`<[^>]*>`)
	reWhitespace = regexp.MustCompile(`\s+`)
)
