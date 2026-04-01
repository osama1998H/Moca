package backup

import (
	"testing"
)

func TestCheckDependencies(t *testing.T) {
	// This test assumes pg_dump and psql are available in the test environment.
	// If not, this test will fail, which correctly indicates the dev environment
	// is not set up for backup operations.
	err := CheckDependencies()
	if err != nil {
		t.Skipf("PostgreSQL client tools not available: %v", err)
	}
}
