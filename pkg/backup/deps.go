package backup

import (
	"fmt"
	"os/exec"
	"strings"
)

// RequiredBinaries lists the external PostgreSQL tools needed by backup operations.
var RequiredBinaries = []string{"pg_dump", "psql"}

// CheckDependencies verifies that pg_dump and psql are available on PATH.
// Returns a descriptive error with installation instructions if either is missing.
func CheckDependencies() error {
	var missing []string
	for _, bin := range RequiredBinaries {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("required PostgreSQL tools not found: %s\n\n"+
		"Install PostgreSQL client tools:\n"+
		"  macOS:   brew install libpq && brew link --force libpq\n"+
		"  Ubuntu:  sudo apt install postgresql-client\n"+
		"  Alpine:  apk add postgresql-client",
		strings.Join(missing, ", "))
}
