//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestBackupPrerequisites(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Verify the test environment supports backup operations by confirming
	// that data can be created and persisted.
	mt := factory.SimpleDocType("BackupDoc")
	env.RegisterMetaType(t, mt)

	for i := 1; i <= 3; i++ {
		env.NewTestDoc(t, "BackupDoc", factory.SimpleDocValues(i))
	}

	// Verify all documents exist.
	ctx := env.DocContext()
	_, total, err := env.DocManager().GetList(ctx, "BackupDoc", factory.EmptyListOptions())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 docs, got %d", total)
	}

	// Note: Full backup/restore cycle tests depend on the backup package (MS-11)
	// implementing pg_dump/pg_restore wrappers. This test verifies the test
	// environment is suitable for backup testing.
}
