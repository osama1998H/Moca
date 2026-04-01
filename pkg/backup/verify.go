package backup

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Verify validates the integrity of a backup file.
// Basic verification checks that the file exists and the gzip envelope is valid.
// Deep verification additionally decompresses the entire file and counts SQL objects.
func Verify(_ context.Context, backupPath string, deep bool) (*VerifyResult, error) {
	absPath, err := filepath.Abs(backupPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	result := &VerifyResult{
		BackupID: backupIDFromPath(absPath),
		Path:     absPath,
	}

	// Check file exists.
	if _, statErr := os.Stat(absPath); statErr != nil {
		result.Error = fmt.Sprintf("file not found: %s", absPath)
		return result, nil
	}

	// Compute checksum.
	checksum, _, checksumErr := fileChecksumAndSize(absPath)
	if checksumErr != nil {
		result.Error = fmt.Sprintf("checksum computation failed: %v", checksumErr)
		return result, nil
	}
	result.Checksum = checksum

	isGzipped := strings.HasSuffix(absPath, ".gz")

	if isGzipped {
		if gzErr := verifyGzipIntegrity(absPath); gzErr != nil {
			result.Error = fmt.Sprintf("gzip integrity check failed: %v", gzErr)
			return result, nil
		}
	}

	if deep {
		count, countErr := countSQLObjects(absPath, isGzipped)
		if countErr != nil {
			result.Error = fmt.Sprintf("deep verification failed: %v", countErr)
			return result, nil
		}
		result.ObjectCount = count
	}

	result.Valid = true
	return result, nil
}

// verifyGzipIntegrity reads through the entire gzip stream to validate checksums.
func verifyGzipIntegrity(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("invalid gzip header: %w", err)
	}
	defer func() { _ = gz.Close() }()

	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("corrupt gzip data: %w", err)
	}
	return nil
}

// countSQLObjects scans the SQL content and counts DDL/DML statements.
func countSQLObjects(path string, isGzipped bool) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f
	if isGzipped {
		gz, gzErr := gzip.NewReader(f)
		if gzErr != nil {
			return 0, gzErr
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	count := 0
	scanner := bufio.NewScanner(reader)
	// Increase buffer for long SQL lines.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isSQLObjectLine(line) {
			count++
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return 0, scanErr
	}
	return count, nil
}

// isSQLObjectLine checks if a line starts with a DDL/DML keyword.
func isSQLObjectLine(line string) bool {
	upper := strings.ToUpper(line)
	for _, prefix := range []string{
		"CREATE TABLE",
		"CREATE INDEX",
		"CREATE SEQUENCE",
		"CREATE TYPE",
		"ALTER TABLE",
		"INSERT INTO",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// backupIDFromPath extracts the backup ID from a file path.
func backupIDFromPath(path string) string {
	base := filepath.Base(path)
	// Remove extensions (.sql.gz or .sql).
	id := strings.TrimSuffix(base, ".gz")
	id = strings.TrimSuffix(id, ".sql")
	return id
}
