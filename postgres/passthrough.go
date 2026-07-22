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

func (tx *PassthroughTx) Commit() error {
	// no-op.
	return nil
}

func (tx *PassthroughTx) Rollback() error {
	// no-op.
	return nil
}

func (tx *PassthroughTx) Begin() (bun.Tx, error) {
	// no-op.
	return tx.Tx, nil
}

func (tx *PassthroughTx) BeginTx(_ context.Context, _ *sql.TxOptions) (bun.Tx, error) {
	// no-op.
	return tx.Tx, nil
}

func (tx *PassthroughTx) RunInTx(
	ctx context.Context, _ *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error,
) error {
	return fn(ctx, tx.Tx)
}
