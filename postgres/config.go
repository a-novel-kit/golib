package postgres

import (
	"context"

	"github.com/uptrace/bun"
)

// Config supplies pooled bun database connections to the rest of the package.
// A Config owns the driver options and the connection lifecycle; the context,
// migration, and test helpers all obtain their handles through it.
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
