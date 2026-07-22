// Package transaction declares the scope a unit of work runs in, independently
// of what stores it.
//
// Its entire dependency list is context: nothing here reaches a connection, a
// pool, or a driver symbol, so a layer that must stay free of persistence detail
// can import it and still name the scope it needs.
//
// Implementations live with their driver — see
// [github.com/a-novel-kit/golib/postgres.Transactor] for the PostgreSQL one.
package transaction

import "context"

// A Transactor runs a unit of work atomically: every persistence call made with
// the context it hands the callback either commits together or is rolled back
// together.
//
// The callback receives no transaction handle: the context it is given is the
// only way to reach the transaction, so no call can escape it unnoticed.
//
// Two behaviors are part of the contract, and an implementation must honor both.
//
// A nested WithinTx joins the transaction already in progress, so a rollback
// anywhere discards the whole outermost unit of work. An inner operation that
// treats a failure as locally recoverable is therefore discarding its caller's
// work too, which makes nesting a deliberate choice.
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
