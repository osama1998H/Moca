package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/osama1998H/moca/pkg/sitepath"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// Create produces a schema-scoped PostgreSQL dump for the given site.
// The backup is stored as a timestamped file in sites/{site}/backups/.
func Create(ctx context.Context, opts CreateOptions) (*BackupInfo, error) {
	if opts.Site == "" {
		return nil, fmt.Errorf("site name is required")
	}
	if opts.ProjectRoot == "" {
		return nil, fmt.Errorf("project root is required")
	}

	schemaName := tenancy.SchemaNameForSite(opts.Site)
	now := time.Now()
	backupID := fmt.Sprintf("bk_%s_%s", sanitizeSiteName(opts.Site), now.Format("20060102_150405"))

	backupDir, err := sitepath.Path(opts.ProjectRoot, opts.Site, "backups")
	if err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	if mkdirErr := os.MkdirAll(backupDir, 0o755); mkdirErr != nil {
		return nil, fmt.Errorf("create backup directory: %w", mkdirErr)
	}

	ext := ".sql"
	if opts.Compress {
		ext = ".sql.gz"
	}
	if opts.Encrypt {
		ext += ".enc"
	}
	filename := backupID + ext
	filePath := filepath.Join(backupDir, filename)

	info, runErr := runPGDump(ctx, opts, schemaName, filePath)
	if runErr != nil {
		_ = os.Remove(filePath)
		return nil, runErr
	}

	// Compute checksum and size.
	checksum, size, err := fileChecksumAndSize(filePath)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	info.ID = backupID
	info.Site = opts.Site
	info.Type = "full"
	info.Path = filePath
	info.Size = size
	info.CreatedAt = now
	info.Compressed = opts.Compress
	info.Encrypted = opts.Encrypt
	info.Verified = true
	info.Checksum = checksum

	return info, nil
}

// runPGDump executes pg_dump and writes output to filePath.
func runPGDump(ctx context.Context, opts CreateOptions, schemaName, filePath string) (*BackupInfo, error) {
	args := []string{
		"--schema=" + schemaName,
		"--no-owner",
		"--no-privileges",
		"--format=plain",
	}
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = pgEnv(opts.DBConfig)

	outFile, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("create backup file: %w", err)
	}

	// Build writer chain: pg_dump → [gzip] → [encrypt] → file.
	// Close order is innermost to outermost.
	var baseWriter io.Writer = outFile
	var encWriter io.WriteCloser

	if opts.Encrypt {
		encKey, keyErr := ParseHexKey(opts.EncryptionKey)
		if keyErr != nil {
			_ = outFile.Close()
			return nil, fmt.Errorf("backup encryption key: %w", keyErr)
		}
		ew, encErr := EncryptStream(outFile, encKey)
		if encErr != nil {
			_ = outFile.Close()
			return nil, fmt.Errorf("init backup encryption: %w", encErr)
		}
		encWriter = ew
		baseWriter = ew
	}

	var writer io.WriteCloser
	var gzWriter *gzip.Writer
	if opts.Compress {
		gzWriter = gzip.NewWriter(baseWriter)
		writer = gzWriter
	} else {
		writer = nopWriteCloser{baseWriter}
	}

	cmd.Stdout = writer
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()

	// Close writers innermost to outermost: gzip → encrypt → file.
	if gzWriter != nil {
		if closeErr := gzWriter.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("close gzip writer: %w", closeErr)
		}
	}
	if encWriter != nil {
		if closeErr := encWriter.Close(); closeErr != nil && runErr == nil {
			runErr = fmt.Errorf("close encryption writer: %w", closeErr)
		}
	}
	if closeErr := outFile.Close(); closeErr != nil && runErr == nil {
		runErr = fmt.Errorf("close backup file: %w", closeErr)
	}

	if runErr != nil {
		return nil, fmt.Errorf("pg_dump failed: %w", runErr)
	}

	return &BackupInfo{}, nil
}

// pgEnv builds environment variables for pg_dump/psql from DBConnConfig.
func pgEnv(cfg DBConnConfig) []string {
	env := os.Environ()
	env = append(env,
		"PGHOST="+cfg.Host,
		"PGPORT="+strconv.Itoa(cfg.Port),
		"PGUSER="+cfg.User,
		"PGPASSWORD="+cfg.Password,
		"PGDATABASE="+cfg.Database,
	)
	return env
}

// fileChecksumAndSize computes the SHA-256 checksum and size of a file.
func fileChecksumAndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// sanitizeSiteName produces a safe string for use in backup IDs.
func sanitizeSiteName(name string) string {
	var out []byte
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // lowercase
		case c == '.', c == '-', c == ' ':
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "site"
	}
	return string(out)
}

// nopWriteCloser wraps an io.Writer as a no-op WriteCloser.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
