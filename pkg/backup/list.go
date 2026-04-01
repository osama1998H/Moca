package backup

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// backupFilePattern matches backup filenames: bk_{site}_{YYYYMMDD}_{HHMMSS}.sql[.gz]
var backupFilePattern = regexp.MustCompile(`^(bk_.+_\d{8}_\d{6})\.sql(\.gz)?$`)

// List scans the backup directory for a site and returns metadata for each backup found.
// Results are sorted newest-first.
func List(_ context.Context, site, projectRoot string) ([]BackupInfo, error) {
	backupDir := filepath.Join(projectRoot, "sites", site, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var backups []BackupInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		matches := backupFilePattern.FindStringSubmatch(name)
		if matches == nil {
			continue
		}

		backupID := matches[1]
		compressed := matches[2] == ".gz"

		info, err := e.Info()
		if err != nil {
			continue
		}

		createdAt := parseTimestampFromID(backupID)

		backups = append(backups, BackupInfo{
			ID:         backupID,
			Site:       site,
			Type:       "full",
			Path:       filepath.Join(backupDir, name),
			Size:       info.Size(),
			CreatedAt:  createdAt,
			Compressed: compressed,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// parseTimestampFromID extracts the timestamp from a backup ID like "bk_acme_20260402_143022".
func parseTimestampFromID(id string) time.Time {
	// The timestamp is the last two underscore-separated segments.
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return time.Time{}
	}
	dateStr := parts[len(parts)-2]
	timeStr := parts[len(parts)-1]
	t, err := time.Parse("20060102150405", dateStr+timeStr)
	if err != nil {
		return time.Time{}
	}
	return t
}
