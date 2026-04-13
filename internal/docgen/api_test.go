package docgen

import (
	"strings"
	"testing"
)

func TestGenerateAPIReference(t *testing.T) {
	got := GenerateAPIReference()

	checks := []struct {
		desc    string
		present string
	}{
		{"Document CRUD section", "Document CRUD"},
		{"resource doctype endpoint", "/api/v1/resource/{doctype}"},
		{"Authentication section", "Authentication"},
		{"Rate Limiting section", "Rate Limiting"},
		{"Workflow section", "Workflow"},
	}

	for _, tc := range checks {
		if !strings.Contains(got, tc.present) {
			t.Errorf("%s: expected %q in output\nFull output:\n%s", tc.desc, tc.present, got)
		}
	}
}
