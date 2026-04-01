package backup

import (
	"os"
	"strings"
	"testing"
)

func TestSanitizeSiteName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"acme.localhost", "acme_localhost"},
		{"my-erp", "my_erp"},
		{"simple", "simple"},
		{"UPPER.Case", "upper_case"},
		{"with spaces", "with_spaces"},
		{"special@chars!", "specialchars"},
		{"", "site"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeSiteName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeSiteName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPgEnv(t *testing.T) {
	cfg := DBConnConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "moca",
		Password: "secret",
		Database: "moca_system",
	}

	env := pgEnv(cfg)

	// Check that our env vars are present.
	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	checks := map[string]string{
		"PGHOST":     "localhost",
		"PGPORT":     "5432",
		"PGUSER":     "moca",
		"PGPASSWORD": "secret",
		"PGDATABASE": "moca_system",
	}

	for key, want := range checks {
		got, ok := envMap[key]
		if !ok {
			t.Errorf("missing env var %s", key)
		} else if got != want {
			t.Errorf("env %s = %q, want %q", key, got, want)
		}
	}

	// Should also include existing env vars (inherited from os.Environ).
	if len(env) <= 5 {
		t.Error("expected inherited environment variables")
	}
}

func TestFileChecksumAndSize(t *testing.T) {
	// Create a temp file with known content.
	f, err := os.CreateTemp(t.TempDir(), "backup-*.sql")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("CREATE TABLE test (id int);")
	_, err = f.Write(content)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	checksum, size, err := fileChecksumAndSize(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}
	if checksum == "" {
		t.Error("expected non-empty checksum")
	}
	// SHA-256 hex is 64 chars.
	if len(checksum) != 64 {
		t.Errorf("checksum length = %d, want 64", len(checksum))
	}
}
