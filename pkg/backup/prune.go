package backup

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

const (
	// safetyWindow protects very recent backups from pruning.
	safetyWindow = 24 * time.Hour

	// Age boundaries for retention buckets.
	dailyBoundary  = 7 * 24 * time.Hour  // 0-7 days
	weeklyBoundary = 30 * 24 * time.Hour // 7-30 days
)

// Prune classifies existing backups by age and deletes those exceeding
// the retention policy. Backups younger than 24 hours are never pruned.
func Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	// List local backups.
	localBackups, err := List(ctx, opts.Site, opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("backup/prune: list local: %w", err)
	}

	// Optionally merge remote-only backups.
	var remoteByID map[string]string // backup ID -> remote key
	if opts.Remote != nil {
		remoteByID = make(map[string]string)
		remoteBackups, err := opts.Remote.ListRemote(ctx, opts.Site)
		if err != nil {
			return nil, fmt.Errorf("backup/prune: list remote: %w", err)
		}
		for _, rb := range remoteBackups {
			remoteByID[rb.ID] = rb.RemoteKey
		}
	}

	// Classify and select prune candidates.
	daily, weekly, monthly := classifyBackups(localBackups, now)
	candidates := selectPruneCandidates(daily, weekly, monthly, opts.Retention)

	// Filter out backups within the safety window.
	var safeCandidates []BackupInfo
	for _, b := range candidates {
		if now.Sub(b.CreatedAt) < safetyWindow {
			continue
		}
		safeCandidates = append(safeCandidates, b)
	}

	// Build kept list (everything not in safeCandidates).
	pruneSet := make(map[string]bool, len(safeCandidates))
	for _, b := range safeCandidates {
		pruneSet[b.ID] = true
	}
	var kept []BackupInfo
	for _, b := range localBackups {
		if !pruneSet[b.ID] {
			kept = append(kept, b)
		}
	}

	result := &PruneResult{
		Deleted: safeCandidates,
		Kept:    kept,
		DryRun:  opts.DryRun,
	}

	if opts.DryRun {
		return result, nil
	}

	// Delete files.
	for _, b := range safeCandidates {
		// Delete local file.
		if b.Path != "" {
			if err := os.Remove(b.Path); err != nil && !os.IsNotExist(err) {
				result.Errors = append(result.Errors,
					fmt.Sprintf("local delete %s: %v", b.ID, err))
			}
		}
		// Delete remote copy if configured.
		if opts.Remote != nil {
			if key, ok := remoteByID[b.ID]; ok {
				if err := opts.Remote.DeleteRemote(ctx, key); err != nil {
					result.Errors = append(result.Errors,
						fmt.Sprintf("remote delete %s: %v", b.ID, err))
				}
			}
		}
	}

	return result, nil
}

// classifyBackups sorts backups into three age-based buckets:
//   - daily:   age < 7 days
//   - weekly:  7 days <= age < 30 days
//   - monthly: age >= 30 days
//
// Within each bucket, backups are sorted newest-first.
func classifyBackups(backups []BackupInfo, now time.Time) (daily, weekly, monthly []BackupInfo) {
	for _, b := range backups {
		age := now.Sub(b.CreatedAt)
		switch {
		case age < dailyBoundary:
			daily = append(daily, b)
		case age < weeklyBoundary:
			weekly = append(weekly, b)
		default:
			monthly = append(monthly, b)
		}
	}
	newestFirst := func(s []BackupInfo) {
		sort.Slice(s, func(i, j int) bool {
			return s[i].CreatedAt.After(s[j].CreatedAt)
		})
	}
	newestFirst(daily)
	newestFirst(weekly)
	newestFirst(monthly)
	return daily, weekly, monthly
}

// selectPruneCandidates returns the backups to delete from each bucket,
// keeping the N newest in each (where N comes from the RetentionConfig).
func selectPruneCandidates(daily, weekly, monthly []BackupInfo, ret config.RetentionConfig) []BackupInfo {
	var candidates []BackupInfo
	candidates = append(candidates, excessBackups(daily, ret.Daily)...)
	candidates = append(candidates, excessBackups(weekly, ret.Weekly)...)
	candidates = append(candidates, excessBackups(monthly, ret.Monthly)...)
	return candidates
}

// excessBackups returns the tail of the slice beyond the keep count.
// Assumes the slice is sorted newest-first. If keep <= 0, all are excess.
func excessBackups(backups []BackupInfo, keep int) []BackupInfo {
	if keep <= 0 {
		return backups
	}
	if len(backups) <= keep {
		return nil
	}
	return backups[keep:]
}
