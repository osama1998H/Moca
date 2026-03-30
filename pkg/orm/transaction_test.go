//go:build integration

package orm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/moca-framework/moca/pkg/orm"
)

// systemPool returns a raw pool pointed at the system schema for transaction tests.
// It reuses the adminPool established in TestMain (postgres_test.go).
func systemPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// adminPool is the raw admin pool created in TestMain (postgres_test.go).
	// Transaction tests use the moca_system.tx_test table created in setupFixtures.
	return adminPool
}

// ── Test 1: Commit on success ─────────────────────────────────────────────────

// TestTransactionCommit verifies that WithTransaction commits the work when fn
// returns nil.
func TestTransactionCommit(t *testing.T) {
	ctx := context.Background()
	pool := systemPool(t)

	unique := fmt.Sprintf("commit_test_%d", uniqueID())

	err := orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO moca_system.tx_test (value) VALUES ($1)", unique,
		)
		return err
	})
	if err != nil {
		t.Fatalf("WithTransaction: unexpected error: %v", err)
	}

	// Verify row is visible outside the transaction (committed).
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM moca_system.tx_test WHERE value = $1", unique,
	).Scan(&count); err != nil {
		t.Fatalf("post-commit query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 committed row, got %d", count)
	}
}

// ── Test 2: Rollback on error ─────────────────────────────────────────────────

// TestTransactionRollbackOnError verifies that WithTransaction rolls back when
// fn returns a non-nil error.
func TestTransactionRollbackOnError(t *testing.T) {
	ctx := context.Background()
	pool := systemPool(t)

	unique := fmt.Sprintf("rollback_err_%d", uniqueID())
	sentinel := errors.New("intentional error")

	err := orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"INSERT INTO moca_system.tx_test (value) VALUES ($1)", unique,
		); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTransaction: expected sentinel error, got: %v", err)
	}

	// Verify row was NOT committed.
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM moca_system.tx_test WHERE value = $1", unique,
	).Scan(&count); err != nil {
		t.Fatalf("post-rollback query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after rollback, got %d", count)
	}
}

// ── Test 3: Rollback on panic ─────────────────────────────────────────────────

// TestTransactionRollbackOnPanic verifies that WithTransaction rolls back the
// transaction when fn panics, and that the panic is re-raised to the caller.
func TestTransactionRollbackOnPanic(t *testing.T) {
	ctx := context.Background()
	pool := systemPool(t)

	unique := fmt.Sprintf("rollback_panic_%d", uniqueID())

	var panicValue interface{}
	func() {
		defer func() {
			panicValue = recover()
		}()
		_ = orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				"INSERT INTO moca_system.tx_test (value) VALUES ($1)", unique,
			); err != nil {
				return err
			}
			panic("simulated panic")
		})
	}()

	if panicValue == nil {
		t.Fatal("expected panic to be re-raised, but it was not")
	}
	if panicValue != "simulated panic" {
		t.Errorf("expected panic value %q, got %v", "simulated panic", panicValue)
	}

	// Verify the insert was rolled back.
	var count int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM moca_system.tx_test WHERE value = $1", unique,
	).Scan(&count); err != nil {
		t.Fatalf("post-panic-rollback query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after panic rollback, got %d", count)
	}
}

// ── Test 4: TxFromContext nested detection ────────────────────────────────────

// TestTxFromContext verifies that WithTransaction stores the active tx in the
// context so nested code can detect and avoid double-wrapping.
func TestTxFromContext(t *testing.T) {
	ctx := context.Background()
	pool := systemPool(t)

	// Outside a transaction: TxFromContext must return false.
	if _, ok := orm.TxFromContext(ctx); ok {
		t.Error("TxFromContext: expected false outside transaction, got true")
	}

	// Inside WithTransaction: TxFromContext must return the active tx.
	err := orm.WithTransaction(ctx, pool, func(innerCtx context.Context, tx pgx.Tx) error {
		gotTx, ok := orm.TxFromContext(innerCtx)
		if !ok {
			t.Error("TxFromContext: expected true inside transaction, got false")
			return nil
		}
		if gotTx == nil {
			t.Error("TxFromContext: returned nil tx inside transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransaction: %v", err)
	}
}

// uniqueID returns a monotonically increasing integer for unique row values.
var idCounter int64

func uniqueID() int64 {
	idCounter++
	return idCounter
}
