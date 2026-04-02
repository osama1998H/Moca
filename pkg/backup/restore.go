package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// Restore drops the site's schema and recreates it from a backup file.
// The backup file may be gzip-compressed (.sql.gz) or plain SQL (.sql).
func Restore(ctx context.Context, opts RestoreOptions) error {
	if opts.Site == "" {
		return fmt.Errorf("site name is required")
	}
	if opts.BackupPath == "" {
		return fmt.Errorf("backup path is required")
	}

	if _, statErr := os.Stat(opts.BackupPath); statErr != nil {
		return fmt.Errorf("backup file not found: %w", statErr)
	}

	schemaName := tenancy.SchemaNameForSite(opts.Site)

	// Drop existing schema.
	dropSQL := fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schemaName)
	if _, err := runPSQL(ctx, opts.DBConfig, "-c", dropSQL); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}

	// Recreate schema.
	createSQL := fmt.Sprintf("CREATE SCHEMA %q", schemaName)
	if _, err := runPSQL(ctx, opts.DBConfig, "-c", createSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	// Pipe backup file through psql.
	return restoreFromFile(ctx, opts)
}

// restoreFromFile pipes the backup file content through psql.
func restoreFromFile(ctx context.Context, opts RestoreOptions) error {
	f, err := os.Open(opts.BackupPath)
	if err != nil {
		return fmt.Errorf("open backup file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f
	if strings.HasSuffix(opts.BackupPath, ".gz") {
		gz, gzErr := gzip.NewReader(f)
		if gzErr != nil {
			return fmt.Errorf("decompress backup: %w", gzErr)
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	cmd := exec.CommandContext(ctx, "psql",
		"--set", "ON_ERROR_STOP=1",
		"--quiet",
	)
	cmd.Env = pgEnv(opts.DBConfig)
	cmd.Stdin = reader

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("restore failed: %w: %s", runErr, stderr.String())
	}

	return nil
}

// runPSQL executes a psql command and returns its combined output.
func runPSQL(ctx context.Context, cfg DBConnConfig, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = pgEnv(cfg)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
