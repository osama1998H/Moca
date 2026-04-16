package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/output"
)

// NewLogCommand returns the "moca log" command group with all subcommands.
func NewLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Log viewing",
		Long:  "Tail, search, and export application logs.",
	}

	cmd.AddCommand(
		newLogTailCmd(),
		newLogSearchCmd(),
		newLogExportCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Shared types and helpers
// ---------------------------------------------------------------------------

// logProcessNames lists the known Moca process names that write log files.
var logProcessNames = []string{"server", "worker", "scheduler"}

// logEntry represents a single parsed structured log line.
type logEntry struct {
	Time      time.Time      `json:"time"`
	Level     string         `json:"level"`
	Msg       string         `json:"msg"`
	Site      string         `json:"site,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Process   string         `json:"process"`
	Fields    map[string]any `json:"fields,omitempty"`
	Raw       string         `json:"-"`
}

// logFilter holds all active filter criteria for log commands.
type logFilter struct {
	Since     time.Time
	Until     time.Time
	Level     string
	Site      string
	RequestID string
	Process   string
	Query     string
}

// matches returns true if the entry passes all active filters.
func (f *logFilter) matches(e *logEntry) bool {
	if f.Level != "" && !levelAtLeast(e.Level, f.Level) {
		return false
	}
	if f.Site != "" && !strings.EqualFold(e.Site, f.Site) {
		return false
	}
	if f.RequestID != "" && e.RequestID != f.RequestID {
		return false
	}
	if f.Process != "" && f.Process != "all" && !strings.EqualFold(e.Process, f.Process) {
		return false
	}
	if !f.Since.IsZero() && e.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Time.After(f.Until) {
		return false
	}
	if f.Query != "" && !strings.Contains(strings.ToLower(e.Msg), strings.ToLower(f.Query)) {
		return false
	}
	return true
}

// logLevelRank maps slog level strings to numeric ranks for comparison.
var logLevelRank = map[string]int{
	"DEBUG": 0,
	"INFO":  1,
	"WARN":  2,
	"ERROR": 3,
}

// levelAtLeast returns true if entryLevel >= minimumLevel.
func levelAtLeast(entryLevel, minimumLevel string) bool {
	entry := logLevelRank[strings.ToUpper(entryLevel)]
	minimum := logLevelRank[strings.ToUpper(minimumLevel)]
	return entry >= minimum
}

// parseLogLine parses a single JSON log line (slog format) into a logEntry.
// The process field is set from the filename context (not from the JSON).
func parseLogLine(line, process string) (*logEntry, error) {
	if strings.TrimSpace(line) == "" {
		return nil, fmt.Errorf("empty line")
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	e := &logEntry{
		Process: process,
		Raw:     line,
		Fields:  make(map[string]any),
	}

	// Extract known fields.
	if t, ok := raw["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			e.Time = parsed
		}
	}
	if lvl, ok := raw["level"].(string); ok {
		e.Level = strings.ToUpper(lvl)
	}
	if msg, ok := raw["msg"].(string); ok {
		e.Msg = msg
	}
	if site, ok := raw["site"].(string); ok {
		e.Site = site
	}
	if rid, ok := raw["request_id"].(string); ok {
		e.RequestID = rid
	}

	// Store remaining fields.
	knownKeys := map[string]bool{"time": true, "level": true, "msg": true, "site": true, "request_id": true}
	for k, v := range raw {
		if !knownKeys[k] {
			e.Fields[k] = v
		}
	}

	return e, nil
}

// formatLogEntry formats a log entry for human-readable TTY display.
// Example: 15:04:12 WARN  [worker] acme  Slow query...
func formatLogEntry(e *logEntry, cc *output.ColorConfig) string {
	ts := e.Time.Format("15:04:05")
	level := fmt.Sprintf("%-5s", e.Level)
	proc := fmt.Sprintf("[%s]", e.Process)
	site := e.Site

	// Apply level-specific color.
	var coloredLevel string
	switch e.Level {
	case "ERROR":
		coloredLevel = cc.Error(level)
	case "WARN":
		coloredLevel = cc.Warning(level)
	case "INFO":
		coloredLevel = cc.Success(level)
	case "DEBUG":
		coloredLevel = cc.Info(level)
	default:
		coloredLevel = level
	}

	if site != "" {
		return fmt.Sprintf("%s %s %s %s  %s",
			cc.Muted(ts), coloredLevel, cc.Muted(proc), site, e.Msg)
	}
	return fmt.Sprintf("%s %s %s  %s",
		cc.Muted(ts), coloredLevel, cc.Muted(proc), e.Msg)
}

// resolveLogDir returns the log directory path. Uses config override if set,
// otherwise defaults to {projectRoot}/logs/.
func resolveLogDir(cfg *config.ProjectConfig, projectRoot string) string {
	if cfg != nil && cfg.Development.LogDir != "" {
		dir := cfg.Development.LogDir
		if filepath.IsAbs(dir) {
			return dir
		}
		return filepath.Join(projectRoot, dir)
	}
	return filepath.Join(projectRoot, "logs")
}

// logFileInfo holds metadata about a discovered log file.
type logFileInfo struct {
	Path    string
	Process string
}

// resolveLogFiles returns the list of log files matching the process filter.
// If processFlag is empty or "all", all known process log files that exist are returned.
func resolveLogFiles(logDir, processFlag string) ([]logFileInfo, error) {
	var processes []string
	if processFlag == "" || strings.EqualFold(processFlag, "all") {
		processes = logProcessNames
	} else {
		p := strings.ToLower(strings.TrimSpace(processFlag))
		valid := false
		for _, name := range logProcessNames {
			if name == p {
				valid = true
				break
			}
		}
		if !valid {
			return nil, output.NewCLIError(fmt.Sprintf("Unknown process %q", processFlag)).
				WithFix(fmt.Sprintf("Valid processes: %s", strings.Join(logProcessNames, ", ")))
		}
		processes = []string{p}
	}

	var files []logFileInfo
	for _, proc := range processes {
		path := filepath.Join(logDir, proc+".log")
		if _, err := os.Stat(path); err == nil {
			files = append(files, logFileInfo{Path: path, Process: proc})
		}
	}

	if len(files) == 0 {
		return nil, output.NewCLIError("No log files found").
			WithContext(fmt.Sprintf("Looked in %s for: %s", logDir, strings.Join(processes, ", "))).
			WithFix("Ensure Moca processes are running and writing logs to the logs/ directory.")
	}

	return files, nil
}

// parseTimeFlagAsDuration interprets a --since flag as either a duration (e.g. "1h", "7d")
// relative to now, or an absolute RFC3339 / date-only timestamp.
func parseTimeFlagAsDuration(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}

	// Try absolute RFC3339 first.
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	// Try date-only format.
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}

	// Try as duration relative to now.
	dur, err := parseDuration(value)
	if err != nil {
		return time.Time{}, output.NewCLIError(fmt.Sprintf("Cannot parse time %q", value)).
			WithErr(err).
			WithFix("Use a duration (e.g. \"1h\", \"7d\") or an absolute time (e.g. \"2024-01-01\", RFC3339).")
	}
	return time.Now().Add(-dur), nil
}

// ---------------------------------------------------------------------------
// log tail
// ---------------------------------------------------------------------------

func newLogTailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Tail logs in real-time (with filters)",
		Long: `Tail log files in real-time with rich filtering.
Parses structured JSON log lines and displays them in a human-readable format.
Press Ctrl+C to stop.`,
		RunE: runLogTail,
	}

	f := cmd.Flags()
	f.String("process", "all", `Filter by process: "server", "worker", "scheduler", "all"`)
	f.String("level", "", `Minimum level: "debug", "info", "warn", "error"`)
	f.String("site", "", "Filter by site")
	f.String("request-id", "", "Follow a specific request")
	f.Bool("no-color", false, "Disable color output")
	f.Bool("follow", true, "Follow log output continuously")

	return cmd
}

// tailState tracks the read position for a single log file.
type tailState struct {
	handle *os.File
	file   logFileInfo
	offset int64
}

func runLogTail(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	logDir := resolveLogDir(ctx.Project, ctx.ProjectRoot)

	processFlag, _ := cmd.Flags().GetString("process")
	levelFlag, _ := cmd.Flags().GetString("level")
	siteFlag, _ := cmd.Flags().GetString("site")
	requestID, _ := cmd.Flags().GetString("request-id")
	files, err := resolveLogFiles(logDir, processFlag)
	if err != nil {
		return err
	}

	filter := &logFilter{
		Level:     levelFlag,
		Site:      siteFlag,
		RequestID: requestID,
		Process:   processFlag,
	}

	jsonMode := w.Mode() == output.ModeJSON

	// Open files and seek to end.
	states := make([]*tailState, 0, len(files))
	for _, lf := range files {
		fh, openErr := os.Open(lf.Path)
		if openErr != nil {
			return output.NewCLIError(fmt.Sprintf("Cannot open log file %s", lf.Path)).
				WithErr(openErr)
		}
		// Seek to end.
		off, seekErr := fh.Seek(0, io.SeekEnd)
		if seekErr != nil {
			_ = fh.Close()
			return fmt.Errorf("seek %s: %w", lf.Path, seekErr)
		}
		states = append(states, &tailState{
			file:   lf,
			handle: fh,
			offset: off,
		})
	}
	defer func() {
		for _, s := range states {
			_ = s.handle.Close()
		}
	}()

	// Signal handling for Ctrl+C.
	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	out := cmd.OutOrStdout()

	for {
		for _, s := range states {
			readTailLines(s, filter, jsonMode, w.Color(), out)
		}

		select {
		case <-sigCtx.Done():
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// readTailLines reads new lines from a tail state and prints matching entries.
func readTailLines(s *tailState, filter *logFilter, jsonMode bool, cc *output.ColorConfig, out io.Writer) {
	// Check for file rotation: if file shrank, reopen from start.
	info, statErr := s.handle.Stat()
	if statErr != nil {
		return
	}
	if info.Size() < s.offset {
		_ = s.handle.Close()
		fh, openErr := os.Open(s.file.Path)
		if openErr != nil {
			return
		}
		s.handle = fh
		s.offset = 0
	}

	// Seek to last known position and read new lines.
	if _, seekErr := s.handle.Seek(s.offset, io.SeekStart); seekErr != nil {
		return
	}
	scanner := bufio.NewScanner(s.handle)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		entry, parseErr := parseLogLine(line, s.file.Process)
		if parseErr != nil {
			continue // skip unparseable lines
		}
		if !filter.matches(entry) {
			continue
		}
		if jsonMode {
			_, _ = fmt.Fprintln(out, entry.Raw)
		} else {
			_, _ = fmt.Fprintln(out, formatLogEntry(entry, cc))
		}
	}

	// Update offset.
	newOff, seekErr := s.handle.Seek(0, io.SeekCurrent)
	if seekErr == nil {
		s.offset = newOff
	}
}

// ---------------------------------------------------------------------------
// log search
// ---------------------------------------------------------------------------

func newLogSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [QUERY]",
		Short: "Search through log files",
		Long: `Search through log files using structured queries.
QUERY is an optional substring to match against log messages.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runLogSearch,
	}

	f := cmd.Flags()
	f.String("process", "", `Filter by process: "server", "worker", "scheduler", "all"`)
	f.String("level", "", "Minimum level")
	f.String("since", "", `Time filter (e.g., "1h", "2d", "2024-01-01")`)
	f.String("site", "", "Filter by site")
	f.String("request-id", "", "Find logs for a specific request")
	f.Int("limit", 100, "Max results")

	return cmd
}

func runLogSearch(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	logDir := resolveLogDir(ctx.Project, ctx.ProjectRoot)

	processFlag, _ := cmd.Flags().GetString("process")
	levelFlag, _ := cmd.Flags().GetString("level")
	sinceFlag, _ := cmd.Flags().GetString("since")
	siteFlag, _ := cmd.Flags().GetString("site")
	requestID, _ := cmd.Flags().GetString("request-id")
	limit, _ := cmd.Flags().GetInt("limit")

	var query string
	if len(args) > 0 {
		query = args[0]
	}

	sinceTime, err := parseTimeFlagAsDuration(sinceFlag)
	if err != nil {
		return err
	}

	files, err := resolveLogFiles(logDir, processFlag)
	if err != nil {
		return err
	}

	filter := &logFilter{
		Level:     levelFlag,
		Site:      siteFlag,
		RequestID: requestID,
		Process:   processFlag,
		Query:     query,
		Since:     sinceTime,
	}

	// Collect matching entries.
	var results []*logEntry
	for _, lf := range files {
		entries, scanErr := scanLogFile(lf, filter, limit)
		if scanErr != nil {
			w.PrintWarning(fmt.Sprintf("Error reading %s: %s", lf.Path, scanErr))
			continue
		}
		results = append(results, entries...)
	}

	// Sort by timestamp descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Time.After(results[j].Time)
	})

	// Trim to limit.
	if len(results) > limit {
		results = results[:limit]
	}

	if len(results) == 0 {
		w.PrintInfo("No matching log entries found.")
		return nil
	}

	// Output.
	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(results)
	}

	headers := []string{"TIME", "LEVEL", "PROCESS", "SITE", "MESSAGE"}
	rows := make([][]string, 0, len(results))
	for _, e := range results {
		msg := e.Msg
		if len(msg) > 80 {
			msg = msg[:77] + "..."
		}
		rows = append(rows, []string{
			e.Time.Format("2006-01-02 15:04:05"),
			e.Level,
			e.Process,
			e.Site,
			msg,
		})
	}
	return w.PrintTable(headers, rows)
}

// scanLogFile reads a log file and returns entries matching the filter, up to limit.
func scanLogFile(lf logFileInfo, filter *logFilter, limit int) ([]*logEntry, error) {
	fh, err := os.Open(lf.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()

	var results []*logEntry
	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		entry, parseErr := parseLogLine(scanner.Text(), lf.Process)
		if parseErr != nil {
			continue
		}
		if filter.matches(entry) {
			results = append(results, entry)
			if len(results) >= limit {
				break
			}
		}
	}

	return results, scanner.Err()
}

// ---------------------------------------------------------------------------
// log export
// ---------------------------------------------------------------------------

func newLogExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export logs for a time range",
		Long: `Export logs for a time range to a file.
The --since flag is required to specify the start of the export window.`,
		RunE: runLogExport,
	}

	f := cmd.Flags()
	f.String("since", "", `Start time (required). Duration (e.g. "1h", "7d") or absolute (RFC3339, date)`)
	f.String("until", "", "End time (default: now)")
	f.String("process", "", `Filter by process: "server", "worker", "scheduler"`)
	f.String("site", "", "Filter by site")
	f.String("format", "json", `Output format: "json", "text"`)
	f.String("output", "", "Output file path (default: stdout)")
	f.Bool("compress", false, "Compress output with gzip")

	_ = cmd.MarkFlagRequired("since")

	return cmd
}

func runLogExport(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	logDir := resolveLogDir(ctx.Project, ctx.ProjectRoot)

	sinceFlag, _ := cmd.Flags().GetString("since")
	untilFlag, _ := cmd.Flags().GetString("until")
	processFlag, _ := cmd.Flags().GetString("process")
	siteFlag, _ := cmd.Flags().GetString("site")
	formatFlag, _ := cmd.Flags().GetString("format")
	outputPath, _ := cmd.Flags().GetString("output")
	compress, _ := cmd.Flags().GetBool("compress")

	sinceTime, err := parseTimeFlagAsDuration(sinceFlag)
	if err != nil {
		return err
	}

	var untilTime time.Time
	if untilFlag != "" {
		untilTime, err = parseTimeFlagAsDuration(untilFlag)
		if err != nil {
			return err
		}
	} else {
		untilTime = time.Now()
	}

	files, err := resolveLogFiles(logDir, processFlag)
	if err != nil {
		return err
	}

	filter := &logFilter{
		Site:    siteFlag,
		Process: processFlag,
		Since:   sinceTime,
		Until:   untilTime,
	}

	// Set up output writer.
	dest := cmd.OutOrStdout()
	var outFile *os.File
	if outputPath != "" {
		outFile, err = os.Create(outputPath)
		if err != nil {
			return output.NewCLIError("Cannot create output file").
				WithErr(err).
				WithFix(fmt.Sprintf("Check that the directory for %q exists and is writable.", outputPath))
		}
		defer func() { _ = outFile.Close() }()
		dest = outFile
	}

	var gzWriter *gzip.Writer
	if compress {
		gzWriter = gzip.NewWriter(dest)
		defer func() { _ = gzWriter.Close() }()
		dest = gzWriter
	}

	// Process log files.
	exported, err := exportLogFiles(files, filter, formatFlag, dest, w)
	if err != nil {
		return err
	}

	// Flush gzip if needed.
	if gzWriter != nil {
		if closeErr := gzWriter.Close(); closeErr != nil {
			return fmt.Errorf("close gzip writer: %w", closeErr)
		}
	}

	// Summary to stderr when writing to file.
	if outputPath != "" {
		suffix := ""
		if compress {
			suffix = " (compressed)"
		}
		fmt.Fprintf(os.Stderr, "Exported %d log entries to %s%s\n", exported, outputPath, suffix)
	}

	if w.Mode() == output.ModeJSON && outputPath != "" {
		return w.PrintJSON(map[string]any{
			"exported": exported,
			"output":   outputPath,
			"compress": compress,
			"since":    sinceTime.Format(time.RFC3339),
			"until":    untilTime.Format(time.RFC3339),
		})
	}

	return nil
}

// exportLogFiles scans log files and writes matching entries to dest.
func exportLogFiles(files []logFileInfo, filter *logFilter, formatFlag string, dest io.Writer, w *output.Writer) (int, error) {
	var exported int
	for _, lf := range files {
		fh, openErr := os.Open(lf.Path)
		if openErr != nil {
			w.PrintWarning(fmt.Sprintf("Cannot open %s: %s", lf.Path, openErr))
			continue
		}

		scanner := bufio.NewScanner(fh)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)

		for scanner.Scan() {
			line := scanner.Text()
			entry, parseErr := parseLogLine(line, lf.Process)
			if parseErr != nil {
				continue
			}
			if !filter.matches(entry) {
				continue
			}

			switch strings.ToLower(formatFlag) {
			case "text":
				_, _ = fmt.Fprintln(dest, formatLogEntry(entry, output.NewColorConfig(true, dest)))
			default: // json
				_, _ = fmt.Fprintln(dest, entry.Raw)
			}
			exported++
		}

		_ = fh.Close()

		if scanErr := scanner.Err(); scanErr != nil {
			w.PrintWarning(fmt.Sprintf("Error reading %s: %s", lf.Path, scanErr))
		}
	}
	return exported, nil
}
