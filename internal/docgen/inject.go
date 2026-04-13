package docgen

import (
	"fmt"
	"os"
	"strings"
)

// InjectSection reads the file at path, finds the region between startMarker and
// endMarker, replaces it with content, and writes the result back to the same
// path. Both markers are preserved in the output. Returns an error if either
// marker is not found or if the file cannot be read or written.
func InjectSection(path, startMarker, endMarker, content string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("docgen: reading %q: %w", path, err)
	}
	text := string(raw)

	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return fmt.Errorf("docgen: start marker %q not found in %q", startMarker, path)
	}

	// Search for end marker after the start marker.
	afterStart := startIdx + len(startMarker)
	relEnd := strings.Index(text[afterStart:], endMarker)
	if relEnd == -1 {
		return fmt.Errorf("docgen: end marker %q not found after start marker in %q", endMarker, path)
	}
	endIdx := afterStart + relEnd

	// Reconstruct: prefix + startMarker + \n + content + \n + endMarker + suffix.
	var b strings.Builder
	b.WriteString(text[:afterStart])
	b.WriteByte('\n')
	b.WriteString(content)
	b.WriteByte('\n')
	b.WriteString(text[endIdx:])

	return os.WriteFile(path, []byte(b.String()), 0o644)
}
