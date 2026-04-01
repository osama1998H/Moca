package backup

import "time"

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
