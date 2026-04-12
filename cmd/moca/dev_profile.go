package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// newDevProfileCmd creates the "moca dev profile" command for capturing pprof
// profiles from a running moca-server and optionally generating SVG flamegraphs.
func newDevProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Profile a request or operation",
		Long: `Capture CPU/memory/goroutine/block profiles from a running moca-server.

Connects to the server's /debug/pprof/ endpoints and downloads a profile.
By default, generates an SVG flamegraph using 'go tool pprof'. Use --raw
to save the binary pprof format instead.

Profile types:
  cpu       - CPU profile (sampled over --duration)
  mem       - Heap memory profile (snapshot)
  goroutine - Goroutine stack dump (snapshot)
  block     - Blocking profile (snapshot)
  mutex     - Mutex contention profile (snapshot)

The server must have pprof enabled (development.enable_pprof: true in moca.yaml).`,
		RunE: runDevProfile,
		Example: `  moca dev profile --type cpu --duration 30s
  moca dev profile --type mem --output heap.svg
  moca dev profile --type goroutine --raw --output goroutines.pb.gz
  moca dev profile --type cpu --port 9000 --duration 10s`,
	}

	f := cmd.Flags()
	f.String("type", "cpu", "Profile type: cpu, mem, goroutine, block, mutex")
	f.String("duration", "30s", "Profile duration (for cpu/block)")
	f.String("output", "profile.svg", "Output file path")
	f.Int("port", 0, "Server port (defaults to development.port from config)")
	f.Bool("raw", false, "Output raw pprof binary instead of SVG")

	return cmd
}

// validProfileTypes lists accepted --type values and their pprof endpoint paths.
var validProfileTypes = map[string]string{
	"cpu":       "profile",
	"mem":       "heap",
	"goroutine": "goroutine",
	"block":     "block",
	"mutex":     "mutex",
}

func runDevProfile(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	profType, _ := cmd.Flags().GetString("type")
	durationStr, _ := cmd.Flags().GetString("duration")
	outputPath, _ := cmd.Flags().GetString("output")
	port, _ := cmd.Flags().GetInt("port")
	raw, _ := cmd.Flags().GetBool("raw")

	// Validate profile type.
	if _, ok := validProfileTypes[profType]; !ok {
		return output.NewCLIError("Invalid profile type").
			WithCause(fmt.Sprintf("Unknown type %q", profType)).
			WithFix("Use one of: cpu, mem, goroutine, block, mutex")
	}

	// Parse duration.
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return output.NewCLIError("Invalid duration").
			WithErr(err).
			WithFix("Use Go duration format: 30s, 1m, 2m30s, etc.")
	}

	// Resolve server port.
	if port == 0 {
		port = defaultDevPort
		if cliCtx.Project != nil && cliCtx.Project.Development.Port > 0 {
			port = cliCtx.Project.Development.Port
		}
	}

	// Build pprof URL.
	pprofURL := buildPprofURL(port, profType, duration)

	w.Print("Fetching %s profile from localhost:%d...", profType, port)
	if profType == "cpu" || profType == "block" {
		w.Print("Duration: %s (waiting for profile collection...)", durationStr)
	}

	// Fetch profile from server with a generous timeout.
	client := &http.Client{Timeout: duration + 30*time.Second}
	req, err := http.NewRequest("GET", pprofURL, nil)
	if err != nil {
		return output.NewCLIError("Invalid pprof URL").WithErr(err)
	}
	// Pprof endpoints go through the full middleware chain — add a site header
	// so tenant resolution doesn't reject the request.
	if cliCtx.Site != "" {
		req.Header.Set("X-Moca-Site", cliCtx.Site)
	}
	resp, err := client.Do(req)
	if err != nil {
		return output.NewCLIError("Cannot fetch profile").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Ensure moca-server is running with pprof enabled (development.enable_pprof: true in moca.yaml).")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return output.NewCLIError("Profile endpoint returned error").
			WithCause(fmt.Sprintf("HTTP %d from %s", resp.StatusCode, pprofURL)).
			WithFix("Ensure development.enable_pprof is true in moca.yaml and the server is running.")
	}

	profileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read profile data: %w", err)
	}

	if len(profileData) == 0 {
		return output.NewCLIError("Empty profile received").
			WithFix("The server returned an empty profile. Try increasing --duration or checking server load.")
	}

	// Raw mode: write the pprof binary directly.
	if raw {
		rawPath := outputPath
		if strings.HasSuffix(rawPath, ".svg") {
			rawPath = strings.TrimSuffix(rawPath, ".svg") + ".pb.gz"
		}
		if writeErr := os.WriteFile(rawPath, profileData, 0o644); writeErr != nil {
			return fmt.Errorf("write profile: %w", writeErr)
		}
		w.PrintSuccess(fmt.Sprintf("Raw profile saved to %s (%d bytes)", rawPath, len(profileData)))
		return nil
	}

	// Write profile to temp file for SVG generation.
	tmpFile, err := os.CreateTemp("", "moca-profile-*.pb.gz")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, writeErr := tmpFile.Write(profileData); writeErr != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp profile: %w", writeErr)
	}
	_ = tmpFile.Close()

	// Generate SVG via go tool pprof.
	w.Debugf("Generating SVG via: go tool pprof -svg %s", tmpFile.Name())
	goCmd := exec.Command("go", "tool", "pprof", "-svg", tmpFile.Name())
	svgData, err := goCmd.Output()
	if err != nil {
		// Fallback: save raw profile if SVG generation fails.
		fallbackPath := strings.TrimSuffix(outputPath, ".svg") + ".pb.gz"
		if writeErr := os.WriteFile(fallbackPath, profileData, 0o644); writeErr == nil {
			w.PrintWarning(fmt.Sprintf("SVG generation failed. Raw profile saved to %s", fallbackPath))
		}
		return output.NewCLIError("SVG generation failed").
			WithErr(err).
			WithFix("Ensure 'go tool pprof' is available and graphviz is installed for SVG support.")
	}

	if err := os.WriteFile(outputPath, svgData, 0o644); err != nil {
		return fmt.Errorf("write SVG: %w", err)
	}

	w.PrintSuccess(fmt.Sprintf("Profile saved to %s", outputPath))
	return nil
}

// buildPprofURL constructs the full pprof endpoint URL.
func buildPprofURL(port int, profType string, duration time.Duration) string {
	endpoint := validProfileTypes[profType]
	base := fmt.Sprintf("http://localhost:%d/debug/pprof/%s", port, endpoint)

	// CPU and block profiles accept a seconds parameter.
	if profType == "cpu" || profType == "block" {
		seconds := int(duration.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%s?seconds=%d", base, seconds)
	}

	return base
}
