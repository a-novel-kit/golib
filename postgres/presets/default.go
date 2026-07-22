package postgrespresets

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
)

const (
	// CreateSchema is the format string for the statement that creates a schema.
	// Schema names cannot be bound as query parameters, so the name is
	// interpolated into the statement.
	CreateSchema = "CREATE SCHEMA IF NOT EXISTS %s;"

	// dropSchemaStmt is the counterpart for DropSchema. CASCADE takes the objects
	// created inside the schema with it, so a migrated test schema drops in one
	// statement.
	dropSchemaStmt = "DROP SCHEMA IF EXISTS %s CASCADE;"

	// MaxIdleConnsDefault is the number of idle connections a pool keeps when the
	// caller expresses no preference.
	//
	// database/sql keeps two, so past the second concurrent query every call opens
	// a fresh connection — a TCP round trip, a TLS handshake and a PostgreSQL
	// authentication exchange — and discards it on release, putting a silent
	// latency floor under every query. Idle connections cost a file descriptor
	// here and a backend on the server; ten stays well under a stock
	// max_connections of 100 once multiplied by a service's replica count.
	MaxIdleConnsDefault = 10
)

// Default is the standard postgres.Config implementation, backed by pgdriver.
// It opens connections lazily and caches them: one for the primary database and
// one per requested schema, all guarded by a single mutex.
type Default struct {
	options []pgdriver.Option

	// MaxIdleConns overrides how many idle connections the pools keep. Zero means
	// MaxIdleConnsDefault; a negative value keeps none, matching database/sql.
	MaxIdleConns int

	// MaxOpenConns bounds how many connections the pools may open. Zero means
	// unlimited, matching database/sql.
	//
	// No default: the right number depends on how many replicas share the database,
	// which a library cannot know.
	//
	// The limit applies as a pool opens. Setting it on the handle afterwards stops
	// working once anything has taken a connection, because the handle is cached;
	// past that point it applies to nothing and reports nothing.
	MaxOpenConns int

	// Cached connection to the primary database, opened on first use.
	db *bun.DB
	// Cached connection per schema name.
	schemas map[string]*bun.DB

	mu sync.RWMutex
}

// NewDefault returns a Default that connects using the given pgdriver options.
func NewDefault(options ...pgdriver.Option) *Default {
	return &Default{
		options: options,
		schemas: make(map[string]*bun.DB),
	}
}

// DB returns the main database connection.
func (config *Default) DB(ctx context.Context) (*bun.DB, error) {
	config.mu.Lock()
	defer config.mu.Unlock()

	if config.db == nil {
		sqldb := sql.OpenDB(pgdriver.NewConnector(config.options...))
		db := bun.NewDB(sqldb, pgdialect.New(), bun.WithDiscardUnknownColumns())
		db.SetMaxIdleConns(config.maxIdleConns())
		db.SetMaxOpenConns(config.MaxOpenConns)

		err := postgres.Ping(ctx, db)
		if err != nil {
			// The pool is not cached on this path, so nothing else will close it. Its
			// idle connections would otherwise outlive the failed call.
			_ = db.Close()

			return nil, fmt.Errorf("ping database: %w", err)
		}

		config.db = db
	}

	return config.db, nil
}

// DBSchema returns a database connection scoped to the named schema, caching one
// connection per schema name. When create is true and the schema has no cached
// connection yet, the schema is created before the connection is returned.
func (config *Default) DBSchema(ctx context.Context, schema string, create bool) (*bun.DB, error) {
	db, err := config.DB(ctx)
	if err != nil {
		return nil, fmt.Errorf("get main db: %w", err)
	}

	if schema == "" {
		return db, nil
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	if conn, exists := config.schemas[schema]; exists {
		return conn, nil
	}

	if create {
		_, err = db.NewRaw(fmt.Sprintf(CreateSchema, schema)).Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("create schema %s: %w", schema, err)
		}
	}

	options := append([]pgdriver.Option{}, config.options...)
	options = append(options, pgdriver.WithConnParams(map[string]any{"search_path": schema}))

	sqldb := sql.OpenDB(pgdriver.NewConnector(options...))
	db = bun.NewDB(sqldb, pgdialect.New(), bun.WithDiscardUnknownColumns())
	db.SetMaxIdleConns(config.maxIdleConns())

	err = postgres.Ping(ctx, db)
	if err != nil {
		// Not yet cached in config.schemas, so this call owns the only reference.
		_ = db.Close()

		return nil, fmt.Errorf("ping database schema %s: %w", schema, err)
	}

	config.schemas[schema] = db

	return db, nil
}

// DropSchema removes a schema created through DBSchema and releases the pool cached for
// it. It is a test-support operation — production schemas are not dropped — and pairs
// with the throwaway schema RunIsolatedTransactionalTest stands up.
//
// Without it, a randomly named schema and its pool are both kept forever: the schema
// accumulates in the database, and the pool holds idle connections against a schema
// nothing will query again.
func (config *Default) DropSchema(ctx context.Context, schema string) error {
	config.mu.Lock()

	if pool, exists := config.schemas[schema]; exists {
		// Close before dropping, so no session is left holding the schema in its
		// search_path.
		_ = pool.Close()

		delete(config.schemas, schema)
	}

	config.mu.Unlock()

	db, err := config.DB(ctx)
	if err != nil {
		return fmt.Errorf("get main db: %w", err)
	}

	_, err = db.NewRaw(fmt.Sprintf(dropSchemaStmt, schema)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("drop schema %s: %w", schema, err)
	}

	return nil
}

// Options returns a copy of the driver options the config was built with.
// postgres.RunDBTest reads them to derive sibling connections to other
// databases in the same cluster.
func (config *Default) Options() []pgdriver.Option {
	config.mu.RLock()
	defer config.mu.RUnlock()

	return append([]pgdriver.Option{}, config.options...)
}

// maxIdleConns resolves the configured override against the default. Callers hold
// the mutex.
func (config *Default) maxIdleConns() int {
	if config.MaxIdleConns == 0 {
		return MaxIdleConnsDefault
	}

	return config.MaxIdleConns
}
