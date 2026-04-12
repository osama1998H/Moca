package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// NewDevCommand returns the "moca dev" command group with all subcommands.
func NewDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Developer tools",
		Long:  "Interactive consoles, benchmarks, profiling, and development utilities.",
	}

	cmd.AddCommand(
		newSubcommand("console", "Interactive Go REPL with framework loaded"),
		newSubcommand("shell", "Open a shell with Moca env vars set"),
		newDevExecuteCmd(),
		newDevRequestCmd(),
		newDevBenchCmd(),
		newDevProfileCmd(),
		newSubcommand("watch", "Watch and rebuild assets on change"),
		newSubcommand("playground", "Start interactive API playground"),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// dev execute
// ---------------------------------------------------------------------------

func newDevExecuteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute EXPRESSION",
		Short: "Run a one-off Go function/expression",
		Long: `Run a one-off Go expression in the framework context.

The expression is wrapped in a generated main.go and executed via 'go run'
within the project's Go workspace. All framework and app packages are
available for import.

If goimports is available on PATH, it will be used to resolve imports
automatically. Otherwise, use full import paths in your expression.`,
		Args: cobra.ExactArgs(1),
		RunE: runDevExecute,
		Example: `  moca dev execute 'fmt.Println("hello")'
  moca dev execute 'fmt.Println(document.Count(ctx, "SalesOrder", nil))'`,
	}

	cmd.Flags().String("site", "", "Target site")

	return cmd
}

func runDevExecute(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	expr := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot

	// Create temp directory for generated code.
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return fmt.Errorf("generate random dir name: %w", err)
	}
	tmpDir := filepath.Join(projectRoot, ".moca", "tmp", "exec-"+hex.EncodeToString(randBytes))
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return output.NewCLIError("Cannot create temp directory").
			WithErr(err).
			WithCause(err.Error())
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Generate main.go.
	mainGo := generateExecMain(expr)
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainGo), 0o644); err != nil {
		return fmt.Errorf("write generated main.go: %w", err)
	}

	// Run goimports if available to resolve imports automatically.
	if goimportsPath, lookErr := exec.LookPath("goimports"); lookErr == nil {
		fmtCmd := exec.Command(goimportsPath, "-w", mainPath)
		fmtCmd.Dir = projectRoot
		_ = fmtCmd.Run() // best-effort; if it fails, go run will report errors
	}

	// Execute via go run.
	goCmd := exec.Command("go", "run", mainPath)
	goCmd.Dir = projectRoot
	goCmd.Env = ensureGoWork(os.Environ(), projectRoot)
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr

	if runErr := goCmd.Run(); runErr != nil {
		// The compilation/runtime error has already been printed to stderr.
		return output.NewCLIError("Expression execution failed").
			WithErr(runErr).
			WithFix("Check the expression syntax. Use full import paths if goimports is not installed.")
	}

	_ = w // writer available if needed for future enhancements
	return nil
}

// generateExecMain produces a Go source file that executes the given expression.
func generateExecMain(expr string) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"fmt\"\n")
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tctx := context.Background()\n")
	b.WriteString("\t_ = ctx\n")
	b.WriteString("\t_ = fmt.Sprintf\n")
	b.WriteString("\t" + expr + "\n")
	b.WriteString("}\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// dev request
// ---------------------------------------------------------------------------

func newDevRequestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request METHOD URL",
		Short: "Make an HTTP request as a user",
		Long: `Make an HTTP request to the Moca API as a specific user.

Sends an authenticated HTTP request to the running moca-server. The server
URL is resolved from moca.yaml development config. Authentication uses the
X-Moca-Dev-User header (dev-mode bypass; full JWT auth comes in MS-14).`,
		Args: cobra.ExactArgs(2),
		RunE: runDevRequest,
		Example: `  moca dev request GET /api/v1/resource/SalesOrder
  moca dev request POST /api/v1/resource/SalesOrder --data '{"customer_name":"Acme"}'
  moca dev request GET /api/v1/resource/User --site acme.localhost --verbose`,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("user", "Administrator", "Request as user")
	f.String("data", "", "Request body (JSON)")
	f.StringSlice("headers", nil, "Extra headers (key:value)")
	f.Bool("verbose", false, "Show full request/response headers")
	f.Bool("json", false, "Output response as structured JSON")

	return cmd
}

const defaultDevPort = 8000

func runDevRequest(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	method := strings.ToUpper(args[0])
	rawURL := args[1]

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	site, _ := cmd.Flags().GetString("site")
	user, _ := cmd.Flags().GetString("user")
	data, _ := cmd.Flags().GetString("data")
	headers, _ := cmd.Flags().GetStringSlice("headers")
	verbose, _ := cmd.Flags().GetBool("verbose")
	jsonMode, _ := cmd.Flags().GetBool("json")

	// Resolve server base URL from config.
	fullURL := resolveRequestURL(rawURL, cliCtx)

	// Build HTTP request.
	var body io.Reader
	if data != "" {
		body = strings.NewReader(data)
	}

	req, err := http.NewRequestWithContext(cmd.Context(), method, fullURL, body)
	if err != nil {
		return output.NewCLIError("Invalid request").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check the HTTP method and URL format.")
	}

	// Set auth and site headers.
	req.Header.Set("X-Moca-Dev-User", user)
	if site != "" {
		req.Header.Set("X-Moca-Site", site)
	}
	if data != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add custom headers.
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	if verbose {
		printRequestHeaders(w, req)
	}

	// Send request.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return output.NewCLIError("Request failed").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Ensure moca-server is running (moca serve).")
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	// JSON output mode.
	if jsonMode || w.Mode() == output.ModeJSON {
		return printResponseJSON(w, resp, respBody)
	}

	// TTY output.
	if verbose {
		printResponseHeaders(w, resp)
	}

	w.Print("%s %s", resp.Proto, resp.Status)

	// Pretty-print JSON response body if possible.
	if len(respBody) > 0 {
		w.Print("") // blank line
		printPrettyBody(w, respBody)
	}

	return nil
}

// resolveRequestURL prepends the server base URL if the given URL starts with "/".
func resolveRequestURL(rawURL string, ctx *clicontext.CLIContext) string {
	if !strings.HasPrefix(rawURL, "/") {
		return rawURL
	}
	port := defaultDevPort
	if ctx.Project != nil && ctx.Project.Development.Port > 0 {
		port = ctx.Project.Development.Port
	}
	return fmt.Sprintf("http://localhost:%d%s", port, rawURL)
}

func printRequestHeaders(w *output.Writer, req *http.Request) {
	w.Print("> %s %s %s", req.Method, req.URL.RequestURI(), req.Proto)
	w.Print("> Host: %s", req.URL.Host)
	keys := sortedHeaderKeys(req.Header)
	for _, k := range keys {
		for _, v := range req.Header[k] {
			w.Print("> %s: %s", k, v)
		}
	}
	w.Print(">")
}

func printResponseHeaders(w *output.Writer, resp *http.Response) {
	keys := sortedHeaderKeys(resp.Header)
	for _, k := range keys {
		for _, v := range resp.Header[k] {
			w.Print("< %s: %s", k, v)
		}
	}
	w.Print("<")
}

func printPrettyBody(w *output.Writer, body []byte) {
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		w.Print("%s", pretty.String())
	} else {
		w.Print("%s", string(body))
	}
}

func printResponseJSON(w *output.Writer, resp *http.Response, body []byte) error {
	// Try to parse body as JSON for structured output.
	var parsedBody any
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		parsedBody = string(body)
	}

	headers := make(map[string]string)
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ", ")
	}

	return w.PrintJSON(map[string]any{
		"status":      resp.StatusCode,
		"status_text": resp.Status,
		"headers":     headers,
		"body":        parsedBody,
	})
}

func sortedHeaderKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
