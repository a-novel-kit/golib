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
	// DBSchema returns a connection scoped to the named schema, creating the
	// schema first when create is true. An empty schema name yields the primary
	// connection.
	DBSchema(ctx context.Context, schema string, create bool) (*bun.DB, error)
}
