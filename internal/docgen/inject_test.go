package docgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInjectSection_ReplacesContentBetweenMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki.md")

	original := "# My Wiki\n\nSome intro text.\n\n<!-- AUTO-GENERATED:START -->\nold generated content\n<!-- AUTO-GENERATED:END -->\n\nSome footer text.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	newContent := "| Column A | Column B |\n|----------|----------|\n| row1a    | row1b    |"
	if err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", newContent); err != nil {
		t.Fatalf("InjectSection returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file after inject: %v", err)
	}
	result := string(got)

	// Hand-written content before marker preserved.
	if want := "# My Wiki\n\nSome intro text.\n\n<!-- AUTO-GENERATED:START -->"; !contains(result, want) {
		t.Errorf("expected prefix content, got:\n%s", result)
	}

	// New content present.
	if !contains(result, newContent) {
		t.Errorf("expected new content in result, got:\n%s", result)
	}

	// Old generated content replaced.
	if contains(result, "old generated content") {
		t.Errorf("old content should have been replaced, got:\n%s", result)
	}

	// Footer preserved.
	if !contains(result, "<!-- AUTO-GENERATED:END -->\n\nSome footer text.\n") {
		t.Errorf("expected footer content preserved, got:\n%s", result)
	}
}

func TestInjectSection_ErrorWhenStartMarkerNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki.md")

	if err := os.WriteFile(path, []byte("# No markers here\n"), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", "content")
	if err == nil {
		t.Fatal("expected error when start marker not found, got nil")
	}
}

func TestInjectSection_ErrorWhenEndMarkerNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki.md")

	if err := os.WriteFile(path, []byte("# Wiki\n<!-- AUTO-GENERATED:START -->\nsome content\n"), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	err := InjectSection(path, "<!-- AUTO-GENERATED:START -->", "<!-- AUTO-GENERATED:END -->", "content")
	if err == nil {
		t.Fatal("expected error when end marker not found, got nil")
	}
}

// contains is a helper so we don't need strings import.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
