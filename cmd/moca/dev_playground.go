package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// newDevPlaygroundCmd creates the "moca dev playground" command — a lightweight
// HTTP server that provides a landing page linking to and proxying Swagger UI
// and GraphiQL from the running moca-server.
func newDevPlaygroundCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playground",
		Short: "Start interactive API playground",
		Long: `Start a lightweight HTTP server that proxies Swagger UI and GraphiQL
from the running moca-server.

The playground serves:
  /         - Landing page with links to both API explorers
  /swagger  - Swagger UI (reverse proxy to {server}/api/docs)
  /graphql  - GraphiQL (reverse proxy to {server}/api/graphql/playground)
  /api/     - API pass-through (so Swagger/GraphiQL can execute requests)

The moca-server must be running (moca serve) for the proxied endpoints to work.`,
		RunE: runDevPlayground,
		Example: `  moca dev playground
  moca dev playground --port 8001
  moca dev playground --site mysite.localhost --no-open`,
	}

	f := cmd.Flags()
	f.Int("port", 8001, "Playground server port")
	f.String("site", "", "Target site")
	f.Bool("no-open", false, "Do not open browser automatically")

	return cmd
}

// playgroundPageData holds template data for the landing page.
type playgroundPageData struct {
	SiteName  string
	ServerURL string
	SwaggerURL  string
	GraphQLURL  string
}

// playgroundPageTmpl is the inline HTML template for the landing page.
const playgroundPageTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Moca API Playground</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: #0f172a;
      color: #e2e8f0;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 2rem;
    }
    .container { max-width: 640px; width: 100%; }
    h1 { font-size: 1.875rem; font-weight: 700; color: #f8fafc; margin-bottom: 0.5rem; }
    .subtitle { color: #94a3b8; font-size: 0.9375rem; margin-bottom: 2.5rem; }
    .site-badge {
      display: inline-block;
      background: #1e293b;
      border: 1px solid #334155;
      border-radius: 9999px;
      padding: 0.25rem 0.875rem;
      font-size: 0.8125rem;
      color: #7dd3fc;
      margin-bottom: 2.5rem;
    }
    .cards { display: grid; grid-template-columns: 1fr 1fr; gap: 1.25rem; margin-bottom: 2.5rem; }
    .card {
      display: block;
      background: #1e293b;
      border: 1px solid #334155;
      border-radius: 0.75rem;
      padding: 1.5rem;
      text-decoration: none;
      color: inherit;
      transition: border-color 0.15s, transform 0.15s;
    }
    .card:hover { border-color: #60a5fa; transform: translateY(-2px); }
    .card-icon { font-size: 2rem; margin-bottom: 0.75rem; }
    .card-title { font-size: 1.0625rem; font-weight: 600; color: #f1f5f9; margin-bottom: 0.375rem; }
    .card-desc { font-size: 0.8125rem; color: #64748b; line-height: 1.5; }
    .info {
      background: #1e293b;
      border: 1px solid #1e3a5f;
      border-radius: 0.5rem;
      padding: 1rem 1.25rem;
      font-size: 0.8125rem;
      color: #94a3b8;
    }
    .info-row { display: flex; justify-content: space-between; padding: 0.25rem 0; }
    .info-label { color: #64748b; }
    .info-value { color: #7dd3fc; font-family: "SF Mono", "Fira Code", monospace; }
    .footer { margin-top: 2rem; text-align: center; font-size: 0.75rem; color: #475569; }
  </style>
</head>
<body>
  <div class="container">
    <h1>Moca API Playground</h1>
    <p class="subtitle">Interactive API explorer powered by Swagger UI and GraphiQL.</p>
    {{if .SiteName}}<span class="site-badge">site: {{.SiteName}}</span>{{end}}
    <div class="cards">
      <a class="card" href="{{.SwaggerURL}}">
        <div class="card-icon">&#128196;</div>
        <div class="card-title">Swagger UI</div>
        <div class="card-desc">Browse and test REST API endpoints via the OpenAPI spec.</div>
      </a>
      <a class="card" href="{{.GraphQLURL}}">
        <div class="card-icon">&#128312;</div>
        <div class="card-title">GraphiQL</div>
        <div class="card-desc">Compose and execute GraphQL queries interactively.</div>
      </a>
    </div>
    <div class="info">
      <div class="info-row">
        <span class="info-label">Server</span>
        <span class="info-value">{{.ServerURL}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">REST docs</span>
        <span class="info-value">{{.ServerURL}}/api/docs</span>
      </div>
      <div class="info-row">
        <span class="info-label">GraphQL</span>
        <span class="info-value">{{.ServerURL}}/api/graphql/playground</span>
      </div>
      <div class="info-row">
        <span class="info-label">OpenAPI spec</span>
        <span class="info-value">{{.ServerURL}}/api/v1/openapi.json</span>
      </div>
    </div>
    <p class="footer">Moca Framework &mdash; <a style="color:#475569" href="{{.ServerURL}}">{{.ServerURL}}</a></p>
  </div>
</body>
</html>`

func runDevPlayground(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	playgroundPort, _ := cmd.Flags().GetInt("port")
	site, _ := cmd.Flags().GetString("site")
	noOpen, _ := cmd.Flags().GetBool("no-open")

	// Resolve the running moca-server's port from config (same pattern as dev_profile.go).
	serverPort := defaultDevPort
	if cliCtx.Project != nil && cliCtx.Project.Development.Port > 0 {
		serverPort = cliCtx.Project.Development.Port
	}

	serverURL := fmt.Sprintf("http://localhost:%d", serverPort)
	playgroundURL := fmt.Sprintf("http://localhost:%d", playgroundPort)

	// Resolve site name from flag or context.
	if site == "" && cliCtx.Site != "" {
		site = cliCtx.Site
	}

	// Build reverse proxy to the moca-server.
	targetURL, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parse server URL: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Compile the landing page template.
	tmpl, err := template.New("playground").Parse(playgroundPageTmpl)
	if err != nil {
		return fmt.Errorf("parse playground template: %w", err)
	}

	data := playgroundPageData{
		SiteName:   site,
		ServerURL:  serverURL,
		SwaggerURL: "/swagger",
		GraphQLURL: "/graphql",
	}

	mux := http.NewServeMux()

	// GET / — HTML landing page.
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		if execErr := tmpl.Execute(rw, data); execErr != nil {
			http.Error(rw, "template error", http.StatusInternalServerError)
		}
	})

	// GET /swagger and /swagger/* — proxy to {server}/api/docs.
	swaggerHandler := func(rw http.ResponseWriter, r *http.Request) {
		suffix := strings.TrimPrefix(r.URL.Path, "/swagger")
		r.URL.Path = "/api/docs" + suffix
		proxy.ServeHTTP(rw, r)
	}
	mux.HandleFunc("/swagger", swaggerHandler)
	mux.HandleFunc("/swagger/", swaggerHandler)

	// GET /graphql — proxy to {server}/api/graphql/playground.
	mux.HandleFunc("/graphql", func(rw http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/api/graphql/playground"
		proxy.ServeHTTP(rw, r)
	})

	// GET /api/ — pass-through proxy for API calls (Swagger/GraphiQL need this).
	mux.HandleFunc("/api/", func(rw http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(rw, r)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", playgroundPort),
		Handler: mux,
	}

	// Print startup banner.
	w.Print("Moca API Playground")
	w.Print("===================")
	w.Print("  URL:      %s", playgroundURL)
	w.Print("  Server:   %s", serverURL)
	if site != "" {
		w.Print("  Site:     %s", site)
	}
	w.Print("  Swagger:  %s/swagger", playgroundURL)
	w.Print("  GraphiQL: %s/graphql", playgroundURL)
	w.Print("")
	w.Print("Press Ctrl+C to stop.")
	w.Print("")

	// Open browser unless --no-open.
	if !noOpen {
		openBrowser(playgroundURL)
	}

	// Listen for Ctrl+C and shut down gracefully.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if listenErr := srv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			errCh <- listenErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		w.Print("Playground stopped.")
	case listenErr := <-errCh:
		if listenErr != nil {
			return output.NewCLIError("Playground server error").
				WithErr(listenErr).
				WithCause(listenErr.Error()).
				WithFix(fmt.Sprintf("Ensure port %d is not already in use.", playgroundPort))
		}
	}

	return nil
}

// openBrowser opens the given URL in the system's default browser.
// Supports macOS (open) and Linux (xdg-open).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	// Best-effort; ignore errors.
	_ = cmd.Start()
}
