package backup

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/robfig/cron/v3"
)

const cronMarkerPrefix = "# moca-backup:"
const disabledPrefix = "# DISABLED: "

// readCrontab and writeCrontab are package-level function variables to allow
// test injection without touching the real system crontab.
var readCrontab = defaultReadCrontab
var writeCrontab = defaultWriteCrontab

func defaultReadCrontab(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "crontab", "-l")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// On many systems "crontab -l" exits 1 with "no crontab for user".
		// Treat that as an empty crontab rather than a fatal error.
		if strings.Contains(stderr.String(), "no crontab for") {
			return "", nil
		}
		return "", fmt.Errorf("backup/schedule: read crontab: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func defaultWriteCrontab(ctx context.Context, content string) error {
	cmd := exec.CommandContext(ctx, "crontab", "-")
	cmd.Stdin = strings.NewReader(content)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup/schedule: write crontab: %w: %s", err, stderr.String())
	}
	return nil
}

// cronMarker returns the marker comment for a project, e.g. "# moca-backup:my-erp".
func cronMarker(projectName string) string {
	return cronMarkerPrefix + projectName
}

// cronCommand returns the command line for a scheduled backup.
func cronCommand(projectRoot string) string {
	return "cd " + projectRoot + " && moca backup create --compress"
}

// validateCronExpr validates a cron expression using robfig/cron/v3 with the
// standard five-field format (Minute | Hour | Dom | Month | Dow).
func validateCronExpr(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(expr); err != nil {
		return fmt.Errorf("backup/schedule: invalid cron expression %q: %w", expr, err)
	}
	return nil
}

// InstallCronSchedule adds or replaces a backup cron entry for the given project.
// If an entry already exists for projectName it is replaced in place; otherwise
// the new entry is appended at the end.
func InstallCronSchedule(ctx context.Context, cronExpr, projectName, projectRoot string) error {
	if err := validateCronExpr(cronExpr); err != nil {
		return err
	}

	content, err := readCrontab(ctx)
	if err != nil {
		return err
	}

	marker := cronMarker(projectName)
	cmdLine := cronExpr + " " + cronCommand(projectRoot)
	lines := splitLines(content)

	idx := findMarkerIndex(lines, marker)
	if idx >= 0 {
		// Replace existing entry: marker stays, next line is replaced.
		if idx+1 < len(lines) {
			lines[idx+1] = cmdLine
		} else {
			lines = append(lines, cmdLine)
		}
	} else {
		// Append new entry.
		lines = append(lines, marker, cmdLine)
	}

	return writeCrontab(ctx, joinLines(lines))
}

// RemoveCronSchedule removes the backup cron entry for the given project.
// If no entry is found, nil is returned (idempotent).
func RemoveCronSchedule(ctx context.Context, projectName string) error {
	content, err := readCrontab(ctx)
	if err != nil {
		return err
	}

	marker := cronMarker(projectName)
	lines := splitLines(content)

	idx := findMarkerIndex(lines, marker)
	if idx < 0 {
		return nil // not found — idempotent
	}

	// Remove marker line and the following command line.
	end := idx + 1
	if end < len(lines) {
		end = idx + 2
	}
	lines = append(lines[:idx], lines[end:]...)

	return writeCrontab(ctx, joinLines(lines))
}

// EnableCronSchedule un-disables a previously disabled backup cron entry.
func EnableCronSchedule(ctx context.Context, projectName string) error {
	content, err := readCrontab(ctx)
	if err != nil {
		return err
	}

	marker := cronMarker(projectName)
	lines := splitLines(content)

	idx := findMarkerIndex(lines, marker)
	if idx < 0 {
		return fmt.Errorf("backup/schedule: no schedule found for project %q", projectName)
	}
	if idx+1 >= len(lines) {
		return fmt.Errorf("backup/schedule: malformed crontab entry for project %q", projectName)
	}

	cmdLine := lines[idx+1]
	if !strings.HasPrefix(cmdLine, disabledPrefix) {
		return fmt.Errorf("backup/schedule: schedule for project %q is already enabled", projectName)
	}

	lines[idx+1] = strings.TrimPrefix(cmdLine, disabledPrefix)

	return writeCrontab(ctx, joinLines(lines))
}

// DisableCronSchedule disables the backup cron entry for the given project by
// prefixing the command line with "# DISABLED: ".
func DisableCronSchedule(ctx context.Context, projectName string) error {
	content, err := readCrontab(ctx)
	if err != nil {
		return err
	}

	marker := cronMarker(projectName)
	lines := splitLines(content)

	idx := findMarkerIndex(lines, marker)
	if idx < 0 {
		return fmt.Errorf("backup/schedule: no schedule found for project %q", projectName)
	}
	if idx+1 >= len(lines) {
		return fmt.Errorf("backup/schedule: malformed crontab entry for project %q", projectName)
	}

	cmdLine := lines[idx+1]
	if strings.HasPrefix(cmdLine, disabledPrefix) {
		return fmt.Errorf("backup/schedule: schedule for project %q is already disabled", projectName)
	}

	lines[idx+1] = disabledPrefix + cmdLine

	return writeCrontab(ctx, joinLines(lines))
}

// ShowSchedule reads the crontab and returns the current schedule state for the
// given project. If no entry is installed, a ScheduleInfo with Installed=false
// is returned.
func ShowSchedule(ctx context.Context, projectName string) (*ScheduleInfo, error) {
	content, err := readCrontab(ctx)
	if err != nil {
		return nil, err
	}

	marker := cronMarker(projectName)
	lines := splitLines(content)

	idx := findMarkerIndex(lines, marker)
	if idx < 0 {
		return &ScheduleInfo{
			ProjectName: projectName,
			Installed:   false,
		}, nil
	}

	info := &ScheduleInfo{
		ProjectName: projectName,
		Installed:   true,
		Enabled:     true,
	}

	if idx+1 < len(lines) {
		cmdLine := lines[idx+1]
		disabled := strings.HasPrefix(cmdLine, disabledPrefix)
		if disabled {
			info.Enabled = false
			cmdLine = strings.TrimPrefix(cmdLine, disabledPrefix)
		}
		cronExpr, projectRoot := parseCronLine(cmdLine)
		info.CronExpr = cronExpr
		info.ProjectRoot = projectRoot
	}

	return info, nil
}

// parseCronLine extracts the cron expression and project root from a command line
// such as "0 2 * * * cd /path/to/project && moca backup create --compress".
// It expects a standard 5-field cron expression followed by the cd command.
func parseCronLine(line string) (cronExpr string, projectRoot string) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return line, ""
	}
	cronExpr = strings.Join(fields[:5], " ")

	// Look for "cd <path>" in the remaining fields.
	rest := fields[5:]
	for i, f := range rest {
		if f == "cd" && i+1 < len(rest) {
			projectRoot = rest[i+1]
			break
		}
	}
	return cronExpr, projectRoot
}

// findMarkerIndex returns the index of the marker line for the given project
// name, or -1 if not found.
func findMarkerIndex(lines []string, marker string) int {
	for i, line := range lines {
		if line == marker {
			return i
		}
	}
	return -1
}

// splitLines splits crontab content into lines, preserving the structure.
// An empty input returns an empty slice.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	// Remove trailing newline to avoid a phantom empty element.
	content = strings.TrimRight(content, "\n")
	return strings.Split(content, "\n")
}

// joinLines joins lines back into crontab content, ensuring a trailing newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
