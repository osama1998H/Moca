package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	deploymentsDirName = "deployments"
	historyFileName    = "history.yaml"
	checksumsFileName  = "checksums.yaml"
)

// deploymentsDir returns the .moca/deployments/ path.
func deploymentsDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".moca", deploymentsDirName)
}

// historyPath returns the path to .moca/deployments/history.yaml.
func historyPath(projectRoot string) string {
	return filepath.Join(deploymentsDir(projectRoot), historyFileName)
}

// snapshotDir returns .moca/deployments/{id}/.
func snapshotDir(projectRoot, deploymentID string) string {
	return filepath.Join(deploymentsDir(projectRoot), deploymentID)
}

// LoadHistory reads .moca/deployments/history.yaml.
// Returns an empty history when the file does not exist.
func LoadHistory(projectRoot string) (*DeploymentHistory, error) {
	data, err := os.ReadFile(historyPath(projectRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DeploymentHistory{}, nil
		}
		return nil, fmt.Errorf("deploy: load history: %w", err)
	}

	var h DeploymentHistory
	if err := yaml.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("deploy: parse history: %w", err)
	}
	return &h, nil
}

// SaveHistory writes the history to .moca/deployments/history.yaml.
func SaveHistory(projectRoot string, h *DeploymentHistory) error {
	dir := deploymentsDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("deploy: create deployments dir: %w", err)
	}

	data, err := yaml.Marshal(h)
	if err != nil {
		return fmt.Errorf("deploy: marshal history: %w", err)
	}

	header := []byte("# Moca deployment history — do not edit manually.\n")
	return os.WriteFile(historyPath(projectRoot), append(header, data...), 0o644)
}

// RecordDeployment appends a record to the history and saves it.
func RecordDeployment(projectRoot string, record DeploymentRecord) error {
	h, err := LoadHistory(projectRoot)
	if err != nil {
		return err
	}
	h.Records = append(h.Records, record)
	return SaveHistory(projectRoot, h)
}

// LatestDeployment returns the most recent record, or nil if none exist.
func LatestDeployment(projectRoot string) (*DeploymentRecord, error) {
	h, err := LoadHistory(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(h.Records) == 0 {
		return nil, nil
	}
	return &h.Records[len(h.Records)-1], nil
}

// FindDeployment returns the record matching the given ID.
func FindDeployment(projectRoot, deploymentID string) (*DeploymentRecord, error) {
	h, err := LoadHistory(projectRoot)
	if err != nil {
		return nil, err
	}
	for i := range h.Records {
		if h.Records[i].ID == deploymentID {
			return &h.Records[i], nil
		}
	}
	return nil, fmt.Errorf("deploy: deployment %s not found", deploymentID)
}

// FindByStep returns the record N steps before the latest.
// Step 1 means the deployment before the latest.
func FindByStep(projectRoot string, step int) (*DeploymentRecord, error) {
	h, err := LoadHistory(projectRoot)
	if err != nil {
		return nil, err
	}
	idx := len(h.Records) - 1 - step
	if idx < 0 {
		return nil, fmt.Errorf("deploy: only %d deployment(s) in history, cannot step back %d", len(h.Records), step)
	}
	return &h.Records[idx], nil
}

// CreateSnapshot copies moca.yaml, moca.lock, and computes binary checksums
// into .moca/deployments/{id}/.
func CreateSnapshot(projectRoot, deploymentID string) error {
	dir := snapshotDir(projectRoot, deploymentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("deploy: create snapshot dir: %w", err)
	}

	// Copy moca.yaml.
	if err := copyFileIfExists(
		filepath.Join(projectRoot, "moca.yaml"),
		filepath.Join(dir, "moca.yaml"),
	); err != nil {
		return fmt.Errorf("deploy: snapshot moca.yaml: %w", err)
	}

	// Copy moca.lock.
	if err := copyFileIfExists(
		filepath.Join(projectRoot, "moca.lock"),
		filepath.Join(dir, "moca.lock"),
	); err != nil {
		return fmt.Errorf("deploy: snapshot moca.lock: %w", err)
	}

	// Compute binary checksums.
	checksums, err := computeBinChecksums(filepath.Join(projectRoot, "bin"))
	if err != nil {
		// bin/ may not exist yet (e.g., first setup before build step).
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deploy: snapshot checksums: %w", err)
		}
		checksums = map[string]string{}
	}

	data, err := yaml.Marshal(checksums)
	if err != nil {
		return fmt.Errorf("deploy: marshal checksums: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, checksumsFileName), data, 0o644)
}

// RestoreSnapshot copies moca.yaml and moca.lock from the snapshot back to the
// project root.
func RestoreSnapshot(projectRoot, deploymentID string) error {
	dir := snapshotDir(projectRoot, deploymentID)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("deploy: snapshot %s not found: %w", deploymentID, err)
	}

	if err := copyFileIfExists(
		filepath.Join(dir, "moca.yaml"),
		filepath.Join(projectRoot, "moca.yaml"),
	); err != nil {
		return fmt.Errorf("deploy: restore moca.yaml: %w", err)
	}

	if err := copyFileIfExists(
		filepath.Join(dir, "moca.lock"),
		filepath.Join(projectRoot, "moca.lock"),
	); err != nil {
		return fmt.Errorf("deploy: restore moca.lock: %w", err)
	}

	return nil
}

// copyFileIfExists copies src to dst. Returns nil if src does not exist.
func copyFileIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// computeBinChecksums returns SHA-256 checksums for all files in dir.
func computeBinChecksums(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	checksums := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		sum, err := fileChecksum(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		checksums[e.Name()] = sum
	}

	return checksums, nil
}

// fileChecksum computes the SHA-256 hex digest of a file.
func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SnapshotExists returns true if a snapshot directory exists for the given ID.
func SnapshotExists(projectRoot, deploymentID string) bool {
	info, err := os.Stat(snapshotDir(projectRoot, deploymentID))
	return err == nil && info.IsDir()
}

// ListDeployments returns all records sorted newest-first, limited to n entries.
// If n <= 0, all records are returned.
func ListDeployments(projectRoot string, n int) ([]DeploymentRecord, error) {
	h, err := LoadHistory(projectRoot)
	if err != nil {
		return nil, err
	}

	records := h.Records
	// Sort newest first.
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt.After(records[j].StartedAt)
	})

	if n > 0 && len(records) > n {
		records = records[:n]
	}
	return records, nil
}
