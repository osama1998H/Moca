package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/console"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/peterh/liner"
	"github.com/spf13/cobra"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// newDevConsoleCmd creates the "moca dev console" command — an interactive Go
// REPL with the framework stdlib available.
func newDevConsoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Interactive Go REPL with framework loaded",
		Long: `Start an interactive Go REPL (powered by yaegi) with the Moca framework
services available.

The REPL evaluates Go expressions using the standard library. A 'moca' console
helper is pre-initialized and printed at startup for reference. Use 'moca dev execute'
for framework-specific one-off operations that import Moca packages.

Type :quit or :exit to leave the REPL. Press Ctrl+D (EOF) to exit.`,
		RunE:    runDevConsole,
		Example: `  moca dev console --site mysite.localhost`,
	}

	cmd.Flags().String("site", "", "Target site (uses current site if not specified)")
	cmd.Flags().String("user", "Administrator", "Console user identity")

	return cmd
}

func runDevConsole(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Resolve site name.
	siteName, _ := cmd.Flags().GetString("site")
	if siteName == "" {
		if cliCtx.Site != "" {
			siteName = cliCtx.Site
		} else {
			// Fall back to first active site.
			sites, listErr := listActiveSites(cmd.Context(), svc)
			if listErr != nil {
				return output.NewCLIError("Cannot list active sites").WithErr(listErr)
			}
			if len(sites) == 0 {
				return output.NewCLIError("No active sites found").
					WithFix("Create a site with 'moca site create' or pass --site.")
			}
			siteName = sites[0]
		}
	}

	// Acquire site's pool and build SiteContext.
	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(err).
			WithContext("site: " + siteName).
			WithFix("Ensure the site exists and PostgreSQL is running.")
	}

	// Fetch schema name for the site.
	siteInfo, err := svc.Sites.GetSiteInfo(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Cannot load site info").
			WithErr(err).
			WithContext("site: " + siteName)
	}

	siteCtx := &tenancy.SiteContext{
		Name:          siteInfo.Name,
		DBSchema:      siteInfo.DBSchema,
		Status:        siteInfo.Status,
		Pool:          pool,
		InstalledApps: siteInfo.Apps,
	}

	userEmail, _ := cmd.Flags().GetString("user")
	consoleUser := &auth.User{Email: userEmail}

	// Build the Console helper.
	con := &console.Console{
		DocManager: svc.DocManager,
		Registry:   svc.Registry,
		Pool:       pool,
		Site:       siteCtx,
		User:       consoleUser,
	}

	_ = con // available for future symbol injection

	// Initialize yaegi interpreter with stdlib.
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)

	// Print banner.
	w.Print("Moca Dev Console — site: %s, user: %s", siteName, userEmail)
	w.Print("Type :quit or :exit to leave. Press Ctrl+D (EOF) to exit.")
	w.Print("Evaluate Go expressions using the standard library.")
	w.Print("For framework operations use: moca dev execute '<expression>'")
	w.Print("")

	// Set up liner for readline support.
	line := liner.NewLiner()
	defer line.Close()

	line.SetCtrlCAborts(true)

	historyFile := ""
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
		historyFile = homeDir + "/.moca_console_history"
		if f, ferr := os.Open(historyFile); ferr == nil {
			_, _ = line.ReadHistory(f)
			_ = f.Close()
		}
	}

	// REPL loop.
	var inputBuf strings.Builder
	prompt := ">>> "
	contPrompt := "... "

	for {
		var input string
		var readErr error

		if inputBuf.Len() == 0 {
			input, readErr = line.Prompt(prompt)
		} else {
			input, readErr = line.Prompt(contPrompt)
		}

		if readErr != nil {
			if readErr == liner.ErrPromptAborted {
				// Ctrl+C — clear the buffer and continue.
				inputBuf.Reset()
				continue
			}
			if readErr == io.EOF {
				// Ctrl+D — exit gracefully.
				fmt.Fprintln(os.Stdout)
				break
			}
			return fmt.Errorf("console readline: %w", readErr)
		}

		trimmed := strings.TrimSpace(input)

		// REPL metacommands.
		if inputBuf.Len() == 0 {
			switch trimmed {
			case ":quit", ":exit", ":q":
				break
			case "":
				continue
			}
			if trimmed == ":quit" || trimmed == ":exit" || trimmed == ":q" {
				break
			}
		}

		if inputBuf.Len() > 0 {
			inputBuf.WriteString("\n")
		}
		inputBuf.WriteString(input)

		// Track history for non-empty input.
		if trimmed != "" {
			line.AppendHistory(input)
		}

		// Attempt evaluation. If the input ends with an open brace/paren we
		// wait for more. Otherwise we evaluate immediately.
		src := inputBuf.String()
		if isIncomplete(src) {
			// Continue collecting input.
			continue
		}

		// Evaluate.
		v, evalErr := i.Eval(src)
		inputBuf.Reset()

		if evalErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", evalErr)
			continue
		}

		// Print non-zero values.
		if v.IsValid() && !v.IsZero() {
			fmt.Printf("%v\n", v)
		}
	}

	// Persist readline history.
	if historyFile != "" {
		if f, ferr := os.Create(historyFile); ferr == nil {
			_, _ = line.WriteHistory(f)
			_ = f.Close()
		}
	}

	return nil
}

// isIncomplete returns true when the Go source likely needs more input
// (unbalanced braces, parens, or backtick strings).
func isIncomplete(src string) bool {
	var braces, parens, brackets int
	inString := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(src)
	n := len(runes)

	for idx := 0; idx < n; idx++ {
		ch := runes[idx]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && idx+1 < n && runes[idx+1] == '/' {
				inBlockComment = false
				idx++
			}
			continue
		}
		if inBacktick {
			if ch == '`' {
				inBacktick = false
			}
			continue
		}
		if inString {
			if ch == '\\' {
				idx++ // skip escaped char
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		switch {
		case ch == '/' && idx+1 < n && runes[idx+1] == '/':
			inLineComment = true
			idx++
		case ch == '/' && idx+1 < n && runes[idx+1] == '*':
			inBlockComment = true
			idx++
		case ch == '`':
			inBacktick = true
		case ch == '"':
			inString = true
		case ch == '{':
			braces++
		case ch == '}':
			braces--
		case ch == '(':
			parens++
		case ch == ')':
			parens--
		case ch == '[':
			brackets++
		case ch == ']':
			brackets--
		}
	}

	return inString || inBacktick || inBlockComment || braces > 0 || parens > 0 || brackets > 0
}
