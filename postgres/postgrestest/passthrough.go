package postgrestest

import (
	"context"
	"database/sql"

	"github.com/uptrace/bun"
)

// passthroughTx extends bun.Tx so that a nested transaction call resolves to the
// same transaction.
//
// PostgreSQL has no nested transactions, so bun opens a savepoint instead. A savepoint belongs to
// the query stream of its parent, so parallel callers cannot share it. A test suite that wraps the
// whole application in one transaction has such callers: it calls methods from several goroutines.
type passthroughTx struct {
	bun.Tx
}

// Commit does nothing. The owner of the wrapped transaction ends it.
func (tx *passthroughTx) Commit() error {
	return nil
}

// Rollback does nothing. The owner of the wrapped transaction ends it.
func (tx *passthroughTx) Rollback() error {
	return nil
}

// Begin returns the wrapped transaction itself.
func (tx *passthroughTx) Begin() (bun.Tx, error) {
	return tx.Tx, nil
}

// BeginTx returns the wrapped transaction itself, whatever the options.
func (tx *passthroughTx) BeginTx(_ context.Context, _ *sql.TxOptions) (bun.Tx, error) {
	return tx.Tx, nil
}

// RunInTx runs fn on the wrapped transaction itself.
func (tx *passthroughTx) RunInTx(
	ctx context.Context, _ *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error,
) error {
	return fn(ctx, tx.Tx)
}
