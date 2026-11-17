package postgres

import (
	"context"

	"github.com/uptrace/bun"
)

// Config supplies pooled bun database connections. It owns the driver options and the lifecycle of
// each connection. The context, migration and test helpers obtain their handles through it.
// postgrespresets.Default is the standard implementation.
type Config interface {
	// DB returns a connection to the primary database, opening it on first call
	// and reusing it thereafter.
	DB(ctx context.Context) (*bun.DB, error)
	// Close closes the connection DB opened, if any, and logs a failure. Defer it in a main, beside
	// the telemetry flush.
	Close()
}
