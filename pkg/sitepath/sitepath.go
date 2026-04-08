package sitepath

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validSiteNameRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,61}[A-Za-z0-9])?$`)

// ValidateName constrains site names to safe path-segment characters so they
// can be used across filesystem-backed site operations without escaping the
// project sites directory.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("site name is required")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("site name %q is invalid: leading or trailing whitespace is not allowed", name)
	}
	if !validSiteNameRe.MatchString(name) {
		return fmt.Errorf("site name %q is invalid: use letters, digits, dots, underscores, and hyphens", name)
	}
	return nil
}

// Path resolves a path under projectRoot/sites/<siteName> and rejects names
// that would escape the sites directory.
func Path(projectRoot, siteName string, elems ...string) (string, error) {
	if projectRoot == "" {
		return "", fmt.Errorf("project root is required")
	}
	if err := ValidateName(siteName); err != nil {
		return "", err
	}

	sitesRoot := filepath.Join(projectRoot, "sites")
	parts := make([]string, 0, len(elems)+2)
	parts = append(parts, sitesRoot, siteName)
	parts = append(parts, elems...)

	candidate := filepath.Join(parts...)
	absRoot, err := filepath.Abs(sitesRoot)
	if err != nil {
		return "", fmt.Errorf("resolve sites root: %w", err)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve site path: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return "", fmt.Errorf("resolve site path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("site name %q resolves outside %q", siteName, absRoot)
	}
	return absCandidate, nil
}
