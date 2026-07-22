// Package transaction declares the scope a unit of work runs in, independently
// of what stores it.
//
// It exists as its own package, rather than alongside a driver, so that a
// caller can name what it needs without gaining access to a database. Its
// entire dependency list is context: nothing here can reach a connection, a
// pool, or a driver symbol, which is what makes it safe to import from a layer
// that is supposed to stay free of persistence detail.
//
// Implementations live with their driver — see
// [github.com/a-novel-kit/golib/postgres.Transactor] for the PostgreSQL one.
package transaction

import "context"

// A Transactor runs a unit of work atomically: every persistence call made with
// the context it hands the callback either commits together or is rolled back
// together.
//
// The callback receives no transaction handle. That is deliberate, and it is
// the reason this interface exists rather than callers driving a driver
// directly: a handle callers are expected to thread through every nested call
// is a handle they can forget, and forgetting it fails silently — the calls run
// outside the transaction, the block commits, and the tests pass. With nothing
// to thread, the mistake cannot be written.
//
// Two behaviours are part of the contract, and an implementation that does not
// honour them does not satisfy this interface.
//
// A nested WithinTx joins the transaction already in progress rather than
// opening a nested one, so a rollback anywhere discards the whole outermost
// unit of work. An inner operation that treats a failure as locally
// recoverable is therefore discarding its caller's work too. Nesting is legal,
// but it should be a deliberate choice rather than an accident of composition.
//
// Nothing that talks to an external service may run inside the callback. An
// open transaction holds a pooled connection for as long as it lasts, and
// pinning one for the length of a call to a third party exhausts the pool and
// blocks reclamation of dead rows. Persist what the external call needs, close
// the transaction, make the call, then open a new transaction to record the
// result.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
