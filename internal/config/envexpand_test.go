package config

import (
	"os"
	"testing"
)

func TestExpandEnvVars_NoPatterns(t *testing.T) {
	src := []byte("key: value\nother: 123")
	result, err := ExpandEnvVars(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(src) {
		t.Errorf("result = %q, want %q", string(result), string(src))
	}
}

func TestExpandEnvVars_SingleVar(t *testing.T) {
	t.Setenv("MOCA_TEST_DB_HOST", "localhost")

	src := []byte("host: ${MOCA_TEST_DB_HOST}")
	result, err := ExpandEnvVars(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "host: localhost" {
		t.Errorf("result = %q", string(result))
	}
}

func TestExpandEnvVars_MultipleVars(t *testing.T) {
	t.Setenv("MOCA_TEST_HOST", "127.0.0.1")
	t.Setenv("MOCA_TEST_PORT", "5432")

	src := []byte("url: ${MOCA_TEST_HOST}:${MOCA_TEST_PORT}")
	result, err := ExpandEnvVars(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "url: 127.0.0.1:5432" {
		t.Errorf("result = %q", string(result))
	}
}

func TestExpandEnvVars_MissingVar(t *testing.T) {
	// Ensure the variable is unset.
	os.Unsetenv("MOCA_TEST_MISSING_VAR_XYZ")

	src := []byte("host: ${MOCA_TEST_MISSING_VAR_XYZ}")
	_, err := ExpandEnvVars(src)
	if err == nil {
		t.Fatal("expected error for missing variable")
	}

	envErr, ok := err.(*EnvExpandError)
	if !ok {
		t.Fatalf("expected *EnvExpandError, got %T", err)
	}
	if len(envErr.Missing) != 1 || envErr.Missing[0] != "MOCA_TEST_MISSING_VAR_XYZ" {
		t.Errorf("Missing = %v", envErr.Missing)
	}
}

func TestExpandEnvVars_MultipleMissingVars(t *testing.T) {
	os.Unsetenv("MOCA_TEST_MISS_A")
	os.Unsetenv("MOCA_TEST_MISS_B")

	src := []byte("a: ${MOCA_TEST_MISS_A}\nb: ${MOCA_TEST_MISS_B}")
	_, err := ExpandEnvVars(src)
	if err == nil {
		t.Fatal("expected error for missing variables")
	}

	envErr, ok := err.(*EnvExpandError)
	if !ok {
		t.Fatalf("expected *EnvExpandError, got %T", err)
	}
	if len(envErr.Missing) != 2 {
		t.Errorf("Missing count = %d, want 2", len(envErr.Missing))
	}
	// Should be sorted.
	if envErr.Missing[0] != "MOCA_TEST_MISS_A" || envErr.Missing[1] != "MOCA_TEST_MISS_B" {
		t.Errorf("Missing = %v (should be sorted)", envErr.Missing)
	}
}

func TestExpandEnvVars_DuplicateMissingVar(t *testing.T) {
	os.Unsetenv("MOCA_TEST_DUP")

	src := []byte("a: ${MOCA_TEST_DUP}\nb: ${MOCA_TEST_DUP}")
	_, err := ExpandEnvVars(src)
	if err == nil {
		t.Fatal("expected error")
	}

	envErr, ok := err.(*EnvExpandError)
	if !ok {
		t.Fatalf("expected *EnvExpandError, got %T", err)
	}
	// Should be deduplicated.
	if len(envErr.Missing) != 1 {
		t.Errorf("Missing count = %d, want 1 (deduplicated)", len(envErr.Missing))
	}
}

func TestExpandEnvVars_EmptyValue(t *testing.T) {
	t.Setenv("MOCA_TEST_EMPTY", "")

	src := []byte("host: ${MOCA_TEST_EMPTY}")
	result, err := ExpandEnvVars(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "host: " {
		t.Errorf("result = %q", string(result))
	}
}

func TestExpandEnvVars_EmptyInput(t *testing.T) {
	result, err := ExpandEnvVars([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("result = %q, want empty", string(result))
	}
}

func TestExpandEnvVars_InvalidPatterns(t *testing.T) {
	// These should not be treated as variables.
	src := []byte("host: $VAR\nother: ${}\nanother: ${123}")
	result, err := ExpandEnvVars(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should pass through unchanged since patterns don't match the regex.
	if string(result) != string(src) {
		t.Errorf("result = %q, want unchanged", string(result))
	}
}


func TestEnvVarPattern_ValidNames(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"${MY_VAR}", true},
		{"${_leading}", true},
		{"${var123}", true},
		{"${A}", true},
		{"${123}", false},  // starts with digit
		{"${}", false},     // empty
		{"$VAR", false},    // no braces
		{"${my-var}", false}, // hyphen not allowed
	}
	for _, tt := range tests {
		got := envVarPattern.MatchString(tt.input)
		if got != tt.match {
			t.Errorf("envVarPattern.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
		}
	}
}
