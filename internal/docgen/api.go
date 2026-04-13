package docgen

import (
	"strings"
)

// GenerateAPIReference returns a static Markdown reference document covering
// all standard Moca REST API endpoints. It does not require a running server.
func GenerateAPIReference() string {
	var b strings.Builder

	writeSection := func(title string, rows [][]string) {
		b.WriteString("### ")
		b.WriteString(title)
		b.WriteString("\n\n")
		b.WriteString("| Method | Endpoint | Description |\n")
		b.WriteString("|--------|----------|-------------|\n")
		for _, row := range rows {
			b.WriteString("| ")
			b.WriteString(row[0])
			b.WriteString(" | ")
			b.WriteString(row[1])
			b.WriteString(" | ")
			b.WriteString(row[2])
			b.WriteString(" |\n")
		}
		b.WriteByte('\n')
	}

	// Document CRUD
	writeSection("Document CRUD", [][]string{
		{"GET", "/api/v1/resource/{doctype}", "List documents of a given DocType"},
		{"POST", "/api/v1/resource/{doctype}", "Create a new document"},
		{"GET", "/api/v1/resource/{doctype}/{name}", "Fetch a single document"},
		{"PUT", "/api/v1/resource/{doctype}/{name}", "Update a document"},
		{"DELETE", "/api/v1/resource/{doctype}/{name}", "Delete a document"},
		{"GET", "/api/v1/resource/{doctype}/{name}/versions", "List document versions"},
		{"GET", "/api/v1/meta/{doctype}", "Fetch DocType metadata (schema)"},
	})

	// Authentication
	writeSection("Authentication", [][]string{
		{"POST", "/api/v1/auth/login", "Authenticate with email and password"},
		{"POST", "/api/v1/auth/refresh", "Refresh an access token"},
		{"POST", "/api/v1/auth/logout", "Invalidate the current session"},
	})

	// SSO / OAuth2 / SAML
	writeSection("SSO / OAuth2 / SAML", [][]string{
		{"GET", "/api/v1/auth/oauth2/authorize", "Initiate OAuth2 authorization flow"},
		{"GET", "/api/v1/auth/oauth2/callback", "OAuth2 authorization code callback"},
		{"GET", "/api/v1/auth/saml/metadata", "SAML Service Provider metadata (XML)"},
		{"POST", "/api/v1/auth/saml/acs", "SAML Assertion Consumer Service endpoint"},
	})

	// Workflow
	writeSection("Workflow", [][]string{
		{"POST", "/api/v1/workflow/{doctype}/{name}/transition", "Trigger a workflow state transition"},
		{"GET", "/api/v1/workflow/{doctype}/{name}/state", "Get current workflow state"},
		{"GET", "/api/v1/workflow/{doctype}/{name}/history", "List workflow transition history"},
		{"GET", "/api/v1/workflow/{doctype}/pending", "List documents pending a workflow action"},
	})

	// Notifications
	writeSection("Notifications", [][]string{
		{"GET", "/api/v1/notifications", "List notifications for the current user"},
		{"GET", "/api/v1/notifications/count", "Count unread notifications"},
		{"POST", "/api/v1/notifications/mark-read", "Mark one or more notifications as read"},
	})

	// Search
	writeSection("Search", [][]string{
		{"GET", "/api/v1/search", "Full-text search across all DocTypes"},
		{"GET", "/api/v1/search/{doctype}", "Full-text search within a specific DocType"},
	})

	// File Upload
	writeSection("File Upload", [][]string{
		{"POST", "/api/v1/files/upload", "Upload a file attachment"},
		{"GET", "/api/v1/files/{file_id}/download", "Download an uploaded file"},
	})

	// Reports & Dashboards
	writeSection("Reports & Dashboards", [][]string{
		{"GET", "/api/v1/reports", "List available reports"},
		{"GET", "/api/v1/reports/{report_name}", "Fetch report metadata"},
		{"POST", "/api/v1/reports/{report_name}/run", "Execute a report with parameters"},
		{"GET", "/api/v1/dashboards", "List available dashboards"},
		{"GET", "/api/v1/dashboards/{dashboard_name}", "Fetch a dashboard definition"},
	})

	// GraphQL
	writeSection("GraphQL", [][]string{
		{"POST", "/api/graphql", "Execute a GraphQL query or mutation"},
		{"GET", "/api/graphql", "GraphQL introspection (GET)"},
	})

	// Server Method Calls
	writeSection("Server Method Calls", [][]string{
		{"POST", "/api/method/{dotted.path}", "Invoke a whitelisted server-side method"},
		{"GET", "/api/method/{dotted.path}", "Invoke a whitelisted server-side method (GET)"},
	})

	// Custom Endpoints
	writeSection("Custom Endpoints", [][]string{
		{"ANY", "/api/v1/custom/{endpoint}", "App-defined custom REST endpoint"},
	})

	// OpenAPI & Docs
	writeSection("OpenAPI & Docs", [][]string{
		{"GET", "/api/openapi.json", "OpenAPI 3.0 specification (JSON)"},
		{"GET", "/api/docs", "Interactive Swagger UI documentation"},
	})

	// Health & Metrics
	writeSection("Health & Metrics", [][]string{
		{"GET", "/health", "Aggregate health check (all subsystems)"},
		{"GET", "/health/ready", "Readiness probe (DB + Redis reachable)"},
		{"GET", "/health/live", "Liveness probe (process alive)"},
		{"GET", "/metrics", "Prometheus metrics endpoint"},
	})

	// Rate Limiting
	b.WriteString("### Rate Limiting\n\n")
	b.WriteString("All API endpoints are subject to rate limiting. Limits are applied per tenant and per user.\n\n")
	b.WriteString("When a request exceeds the configured limit the server responds with HTTP **429 Too Many Requests**.\n\n")
	b.WriteString("The following headers are included in every API response:\n\n")
	b.WriteString("```\n")
	b.WriteString("X-RateLimit-Limit:     1000\n")
	b.WriteString("X-RateLimit-Remaining: 42\n")
	b.WriteString("X-RateLimit-Reset:     1712000000\n")
	b.WriteString("Retry-After:           30\n")
	b.WriteString("```\n\n")

	return b.String()
}
