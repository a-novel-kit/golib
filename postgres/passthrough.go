package postgres

import (
	"context"
	"database/sql"

	"github.com/uptrace/bun"
)

// PassthroughTx extends bun.Tx so that a nested transaction call resolves to the
// same transaction.
//
// PostgreSQL has no nested transactions, so bun opens a savepoint instead. A
// savepoint belongs to its parent's query stream and cannot serve parallel
// callers, which breaks a test suite that wraps the whole application in one
// transaction and then calls a method from several goroutines.
type PassthroughTx struct {
	bun.Tx
}

// NewPassthroughTx wraps tx so that nested transaction calls resolve to tx
// itself.
func NewPassthroughTx(tx bun.Tx) *PassthroughTx {
	return &PassthroughTx{Tx: tx}
}

// Commit does nothing. The owner of the wrapped transaction ends it.
func (tx *PassthroughTx) Commit() error {
	return nil
}

// Rollback does nothing. The owner of the wrapped transaction ends it.
func (tx *PassthroughTx) Rollback() error {
	return nil
}

// Begin returns the wrapped transaction itself.
func (tx *PassthroughTx) Begin() (bun.Tx, error) {
	return tx.Tx, nil
}

// BeginTx returns the wrapped transaction itself, whatever the options.
func (tx *PassthroughTx) BeginTx(_ context.Context, _ *sql.TxOptions) (bun.Tx, error) {
	return tx.Tx, nil
}

// RunInTx runs fn on the wrapped transaction itself.
func (tx *PassthroughTx) RunInTx(
	ctx context.Context, _ *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error,
) error {
	return fn(ctx, tx.Tx)
}
