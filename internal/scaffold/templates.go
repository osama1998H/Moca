// Package scaffold provides an app scaffolding engine for creating new Moca
// applications with the correct directory structure, manifest, hooks, and Go module.
package scaffold

// templateData holds all values available to templates.
type templateData struct {
	AppName      string // snake_case: "my_app"
	PackageName  string // Go package name (same as AppName)
	ModuleName   string // TitleCase: "MyApp"
	ModuleSnake  string // snake_case: "my_app"
	Title        string // "My App"
	Publisher    string
	License      string
	GoModulePath string // full module path: "github.com/osama1998H/moca/apps/my_app"
	DocType      string // optional: "Task"
	DocTypeSnake string // optional: "task"
	IncludeDesk  bool   // scaffold desk/ directory with desk-manifest.json
}

const manifestTmpl = `name: {{.AppName}}
title: "{{.Title}}"
version: "0.1.0"
publisher: "{{.Publisher}}"
license: "{{.License}}"
description: "{{.Title}} Moca application"
moca_version: ">=0.1.0"
modules:
  - name: {{.ModuleName}}
    label: {{.ModuleName}}
    doctypes:{{if .DocType}}
      - {{.DocType}}{{end}}
`

const hooksTmpl = `package {{.PackageName}}

import (
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

// Initialize registers controllers and hooks for the {{.Title}} app.
func Initialize(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {
	// Register your document controllers here:
	// cr.RegisterOverride("MyDocType", NewMyDocTypeController)

	_ = cr
	_ = hr
}
`

const goModTmpl = `module {{.GoModulePath}}

go 1.26.1

require (
	github.com/osama1998H/moca v0.0.0
)

replace github.com/osama1998H/moca => ../..
`

const readmeTmpl = `# {{.Title}}

{{.Title}} Moca application.

## Getting Started

This app was scaffolded with ` + "`moca app new {{.AppName}}`" + `.

## Structure

- ` + "`manifest.yaml`" + ` - App metadata and module definitions
- ` + "`hooks.go`" + ` - Hook and controller registration
- ` + "`modules/`" + ` - Modules containing doctypes, pages, and reports
- ` + "`go.mod`" + ` - Go module definition
{{if .IncludeDesk}}- ` + "`desk/`" + ` - Desk UI extensions (custom field types, pages, widgets)
  - ` + "`desk-manifest.json`" + ` - Declares desk extensions for this app
{{end}}`

const migrationTmpl = `-- Initial migration for {{.AppName}}
-- Add your table definitions here.
`

const setupTestTmpl = `package {{.PackageName}}_test

import "testing"

func TestPlaceholder(t *testing.T) {
	// TODO: Add integration tests for {{.Title}}.
	t.Log("{{.Title}} test suite initialized")
}
`

const apiControllerTmpl = `package {{.ModuleSnake}}

import (
	"net/http"
)

// HandleList is a sample API handler for listing records.
func HandleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(` + "`" + `{"data": []}` + "`" + `))
}
`

const docTypeTmpl = `{
  "name": "{{.DocType}}",
  "module": "{{.ModuleName}}",
  "label": "{{.DocType}}",
  "description": "{{.DocType}} document type.",
  "naming_rule": {
    "rule": "autoincrement"
  },
  "title_field": "name",
  "sort_field": "creation",
  "sort_order": "desc",
  "fields": [
    {
      "name": "title",
      "field_type": "Data",
      "label": "Title",
      "required": true,
      "in_list_view": true,
      "searchable": true
    },
    {
      "name": "status",
      "field_type": "Select",
      "label": "Status",
      "options": "Open\nClosed",
      "default": "Open",
      "in_list_view": true,
      "in_filter": true
    }
  ],
  "permissions": [
    {
      "role": "System Manager",
      "doctype_perm": 63
    }
  ]
}
`

// ---------------------------------------------------------------------------
// Desk extension scaffold templates (Task 13/14)
// ---------------------------------------------------------------------------

const deskManifestTmpl = `{
  "$schema": "https://moca.dev/schemas/desk-manifest.schema.json",
  "app": "{{.AppName}}",
  "version": "0.1.0",
  "extensions": {
    "field_types": {},
    "pages": [],
    "sidebar_items": [],
    "dashboard_widgets": []
  }
}
`

const deskExampleFieldTmpl = `// Example custom field type for the {{.Title}} app.
// Uncomment and rename to create a custom field component.
//
// import type { FieldProps } from "@moca/desk";
//
// export default function ExampleField({ fieldDef, value, onChange, readOnly }: FieldProps) {
//   return (
//     <div>
//       <label>{fieldDef.label}</label>
//       <input
//         value={value ?? ""}
//         onChange={(e) => onChange?.(e.target.value)}
//         readOnly={readOnly}
//       />
//     </div>
//   );
// }

export {};
`
