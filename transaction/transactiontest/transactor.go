// Package transactiontest provides an in-memory [transaction.Transactor] for
// unit tests.
//
// A test substitutes *transactiontest.Transactor wherever a
// [transaction.Transactor] is expected, so an operation under test runs its
// body without a database while the test can still assert that the body ran
// inside a transaction at all.
//
// It does not implement transactions. Nothing is rolled back, because there is
// nothing to roll back — an in-memory double cannot undo whatever the callback
// did to the fakes around it. What it verifies is that the caller opened a
// scope and propagated the callback's outcome; that a rollback actually
// discards writes is a property of the real implementation, and belongs in a
// test that has a database.
package transactiontest

import (
	"context"
	"sync"
)

// A Transactor is an in-memory [transaction.Transactor] that runs the callback
// inline and counts how many times it was entered. Safe for concurrent use.
type Transactor struct {
	err   error
	calls int
	mu    sync.Mutex
}

// NewTransactor returns a Transactor that runs every callback and returns
// whatever the callback returns.
func NewTransactor() *Transactor {
	return &Transactor{}
}

// NewFailingTransactor returns a Transactor that refuses to run the callback at
// all and returns err instead, standing in for a transaction that could not be
// opened. The callback not running is the point: an operation that reports
// success when its unit of work never started is the failure this reproduces.
func NewFailingTransactor(err error) *Transactor {
	return &Transactor{err: err}
}

// WithinTx satisfies [transaction.Transactor] by invoking fn with the context
// it was given, unless the Transactor was built to fail.
func (transactor *Transactor) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	transactor.mu.Lock()
	transactor.calls++
	err := transactor.err
	transactor.mu.Unlock()

	if err != nil {
		return err
	}

	return fn(ctx)
}

// Calls reports how many times WithinTx was entered, so a test can assert an
// operation opened exactly one scope rather than one per write.
func (transactor *Transactor) Calls() int {
	transactor.mu.Lock()
	defer transactor.mu.Unlock()

	return transactor.calls
}
