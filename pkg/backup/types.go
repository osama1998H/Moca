package backup

import (
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// BackupInfo holds metadata about a single backup file.
type BackupInfo struct {
	CreatedAt  time.Time `json:"created_at"`
	ID         string    `json:"id"`
	Site       string    `json:"site"`
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	Checksum   string    `json:"checksum,omitempty"`
	Size       int64     `json:"size"`
	Compressed bool      `json:"compressed"`
	Verified   bool      `json:"verified"`
}

// CreateOptions configures a backup creation.
type CreateOptions struct {
	Site        string
	ProjectRoot string
	DBConfig    DBConnConfig
	Compress    bool
}

// RestoreOptions configures a backup restore.
type RestoreOptions struct {
	Site       string
	BackupPath string
	DBConfig   DBConnConfig
	Force      bool
}

// VerifyResult holds the outcome of a backup verification.
type VerifyResult struct {
	BackupID    string `json:"backup_id"`
	Path        string `json:"path"`
	Checksum    string `json:"checksum"`
	Error       string `json:"error,omitempty"`
	ObjectCount int    `json:"object_count,omitempty"`
	Valid       bool   `json:"valid"`
}

// DBConnConfig holds the subset of database config needed for pg_dump/psql.
type DBConnConfig struct {
	Host     string
	User     string
	Password string
	Database string
	Port     int
}

// RemoteBackupInfo extends BackupInfo with remote storage metadata.
type RemoteBackupInfo struct {
	RemoteKey string `json:"remote_key,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
	BackupInfo
}

// ScheduleInfo holds the current state of the backup cron schedule.
type ScheduleInfo struct {
	CronExpr    string `json:"cron_expr,omitempty"`
	ProjectName string `json:"project_name"`
	ProjectRoot string `json:"project_root"`
	Enabled     bool   `json:"enabled"`
	Installed   bool   `json:"installed"`
}

// PruneOptions configures a prune operation.
type PruneOptions struct {
	Remote      *RemoteStorage
	Now         time.Time // injectable for testing; zero means time.Now()
	Site        string
	ProjectRoot string
	Retention   config.RetentionConfig
	DryRun      bool
}

// PruneResult summarizes a prune operation.
type PruneResult struct {
	Deleted []BackupInfo `json:"deleted"`
	Kept    []BackupInfo `json:"kept"`
	Errors  []string     `json:"errors,omitempty"`
	DryRun  bool         `json:"dry_run"`
}
