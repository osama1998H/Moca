package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/process"
)

// processNames lists the 5 Moca process types in display order.
var processNames = []string{
	"moca-server",
	"moca-worker",
	"moca-scheduler",
	"moca-outbox",
	"moca-search-sync",
}

// Status gathers current deployment and process state.
func Status(_ context.Context, projectRoot string, cfg *config.ProjectConfig) (*StatusResult, error) {
	result := &StatusResult{}

	// Latest deployment from history.
	latest, _ := LatestDeployment(projectRoot)
	if latest != nil {
		result.CurrentDeployment = latest.ID
		if latest.Status == StatusSuccess {
			result.Uptime = time.Since(latest.CompletedAt)
		}
	}

	// Process states.
	for _, name := range processNames {
		info := checkProcess(projectRoot, name)
		result.Processes = append(result.Processes, info)
	}

	// Count sites.
	result.SiteCount = countSites(projectRoot)

	return result, nil
}

// checkProcess reads the PID file and checks whether the process is alive.
func checkProcess(projectRoot, name string) ProcessInfo {
	info := ProcessInfo{Name: name, State: StateStopped}

	pid, err := readPID(projectRoot, name)
	if err != nil {
		// No PID file means stopped or not configured.
		if pidFileExists(projectRoot, name) {
			info.State = StateFailed
		}
		return info
	}

	if process.IsRunning(pid) {
		info.PID = pid
		info.State = StateRunning
		// Approximate uptime from PID file mtime.
		if mtime, err := pidFileMtime(projectRoot, name); err == nil {
			info.Uptime = formatDuration(time.Since(mtime))
		}
	} else {
		info.State = StateStopped
	}

	return info
}

// readPID reads and parses the PID from .moca/{name}.pid.
func readPID(projectRoot, name string) (int, error) {
	path := filepath.Join(projectRoot, ".moca", name+".pid")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// pidFileExists checks whether the PID file exists.
func pidFileExists(projectRoot, name string) bool {
	path := filepath.Join(projectRoot, ".moca", name+".pid")
	_, err := os.Stat(path)
	return err == nil
}

// pidFileMtime returns the modification time of the PID file.
func pidFileMtime(projectRoot, name string) (time.Time, error) {
	path := filepath.Join(projectRoot, ".moca", name+".pid")
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// countSites counts site directories under sites/.
func countSites(projectRoot string) int {
	sitesDir := filepath.Join(projectRoot, "sites")
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

// formatDuration formats a duration as a human-readable string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strings.TrimSuffix(d.Truncate(time.Minute).String(), "0s")
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return pluralize(h, "hour")
		}
		return pluralize(h, "hour") + " " + pluralize(m, "min")
	default:
		days := int(d.Hours()) / 24
		return pluralize(days, "day")
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
