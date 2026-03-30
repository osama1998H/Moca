package orm

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxFunc is a function that runs inside a database transaction.
// The ctx passed in carries the active transaction via TxFromContext.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// txKey is the unexported context key used to store an active transaction.
type txKey struct{}

// WithTransaction begins a transaction on pool, stores it in ctx, and calls fn.
// On success it commits; on error it rolls back. Panics inside fn are recovered,
// the transaction is rolled back, and the panic is re-raised.
//
// Nested transaction detection: callers can check TxFromContext before calling
// WithTransaction to detect and avoid double-wrapping.
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn TxFunc) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(txCtx)
			panic(p) // re-raise after rollback
		}
		if err != nil {
			_ = tx.Rollback(txCtx)
		}
	}()

	if err = fn(txCtx, tx); err != nil {
		return err
	}

	if err = tx.Commit(txCtx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// TxFromContext retrieves the active pgx.Tx from ctx, if any.
// Returns (tx, true) when a transaction is in progress, (nil, false) otherwise.
// Use this to detect nested transactions and avoid double-wrapping.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}
