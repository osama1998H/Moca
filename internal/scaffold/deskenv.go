package scaffold

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const deskEnvSiteKey = "VITE_MOCA_SITE"

// UpdateDeskEnvSite sets VITE_MOCA_SITE in <projectRoot>/desk/.env.
//
// Semantics:
//   - If <projectRoot>/desk does not exist, returns nil (headless project).
//   - If desk/.env does not exist, creates it with a single VITE_MOCA_SITE line.
//   - If VITE_MOCA_SITE is present and empty, replaces it with siteName.
//   - If VITE_MOCA_SITE is present and non-empty, replaces it only when force is true.
//   - If the key is absent, appends it.
//
// Writes are performed via a temp file + os.Rename in the same directory
// for atomic replacement; Vite's watcher handles the swap cleanly.
func UpdateDeskEnvSite(projectRoot, siteName string, force bool) error {
	deskDir := filepath.Join(projectRoot, "desk")
	if info, err := os.Stat(deskDir); err != nil || !info.IsDir() {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat desk dir: %w", err)
		}
		return nil
	}

	envPath := filepath.Join(deskDir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", envPath, err)
	}

	rendered, err := renderDeskEnv(existing, siteName, force)
	if err != nil {
		return err
	}
	if rendered == nil {
		return nil // nothing to change
	}

	return writeAtomic(envPath, rendered)
}

// renderDeskEnv returns the new file contents, or nil if no write is needed.
func renderDeskEnv(existing []byte, siteName string, force bool) ([]byte, error) {
	if len(existing) == 0 {
		return []byte(deskEnvSiteKey + "=" + siteName + "\n"), nil
	}

	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(existing))
	found := false
	changed := false

	for scanner.Scan() {
		line := scanner.Text()
		trim := strings.TrimLeft(line, " \t")
		if !found && strings.HasPrefix(trim, deskEnvSiteKey+"=") {
			found = true
			value := strings.TrimPrefix(trim, deskEnvSiteKey+"=")
			if value == "" || force {
				out.WriteString(deskEnvSiteKey + "=" + siteName + "\n")
				if value != siteName {
					changed = true
				}
				continue
			}
			// non-empty, force=false: keep line verbatim.
			out.WriteString(line + "\n")
			continue
		}
		out.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan .env: %w", err)
	}

	if !found {
		if !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
		out.WriteString(deskEnvSiteKey + "=" + siteName + "\n")
		changed = true
	}

	if !changed {
		return nil, nil
	}
	return out.Bytes(), nil
}

// writeAtomic writes data to path via a sibling temp file + rename.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".env.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op if Rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
