package postgres

import (
	"context"
	"database/sql"

	"github.com/uptrace/bun"
)

// PassthroughTx is an extension of bun.Tx that prevents sub contexts from creating new sub-transactions.
//
// Because postgresql does not support nested transactions, bun will create savepoints instead. Unlike
// top-level transactions, savepoints don't support parallelism (as they are part of the same query).
//
// For testing, where the whole application is wrapped in a transaction, this can cause issues where
// multiple parallel calls to a method will attempt concurrent write on the same transaction.
//
// To get around it, this package provides an alternative bun.IDB implementation, that does not create
// new transactions.
type PassthroughTx struct {
	bun.Tx
}

// NewPassthroughTx wraps tx so that nested transaction calls resolve to tx
// itself rather than opening savepoints.
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
