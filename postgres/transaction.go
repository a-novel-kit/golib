package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/uptrace/bun"
)

// WithTx derives a context carrying tx, so GetContext resolves that transaction.
//
// [WithinTx] applies this for you and is what callers normally want. WithTx is
// exported for the cases that own the transaction lifecycle themselves — a test
// harness wrapping a whole suite, or a caller bridging a transaction it opened
// through some other API.
func WithTx(ctx context.Context, tx bun.IDB) context.Context {
	return context.WithValue(ctx, ContextKey{}, tx)
}

// InTx reports whether ctx carries an open transaction. It reports false when
// ctx carries the connection pool, and false when it carries no database at all.
//
// Work that must not hold a pooled connection — any call to an external service
// — guards itself with this.
//
// It reports true under [RunTransactionalTest], whose PassthroughTx is not a
// *bun.DB. A test covering an outbound call must therefore use [RunDBTest],
// which puts a real pool on the context.
func InTx(ctx context.Context) bool {
	db, err := GetContext(ctx)
	if err != nil {
		return false
	}

	_, isPool := db.(*bun.DB)

	return !isPool
}

// WithinTx runs callback inside a transaction opened on the connection carried
// by ctx, and installs that transaction on the context callback receives, so
// every call made with it takes part in the transaction.
//
// The callback takes no transaction argument: the context it receives is the
// only database handle reachable inside it, so no call can escape unnoticed.
//
// A nested call joins the transaction already in progress, so one unit of work
// has one outcome and a rollback anywhere discards all of it. A nested call
// never reaches opts: an operation depending on a specific isolation level must
// be the outermost transaction.
func WithinTx(ctx context.Context, opts *sql.TxOptions, callback func(ctx context.Context) error) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get database handle from context: %w", err)
	}

	pool, isPool := db.(*bun.DB)
	if !isPool {
		return callback(ctx)
	}

	err = pool.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		return callback(WithTx(ctx, tx))
	})
	if err != nil {
		return fmt.Errorf("run in transaction: %w", err)
	}

	return nil
}

// A Transactor is the PostgreSQL implementation of
// [github.com/a-novel-kit/golib/transaction.Transactor].
type Transactor struct {
	opts *sql.TxOptions
}

// NewTransactor returns a Transactor opening its transactions with opts. A nil
// opts leaves the database defaults in place, which is read-committed
// isolation.
func NewTransactor(opts *sql.TxOptions) *Transactor {
	return &Transactor{opts: opts}
}

// WithinTx satisfies [github.com/a-novel-kit/golib/transaction.Transactor].
func (transactor *Transactor) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return WithinTx(ctx, transactor.opts, fn)
}
