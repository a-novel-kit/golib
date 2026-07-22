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
	// Schema names cannot be passed as query parameters, so the name is
	// interpolated into the statement rather than bound.
	CreateSchema = "CREATE SCHEMA IF NOT EXISTS %s;"

	// MaxIdleConnsDefault is the number of idle connections a pool keeps when the
	// caller expresses no preference.
	//
	// It exists because database/sql keeps two, and two is nobody's intended
	// answer: past the second concurrent query every call opens a fresh
	// connection — a TCP round trip, a TLS handshake and a PostgreSQL
	// authentication exchange — then discards it on release. Nothing reports that.
	// It is simply a latency floor under every query, in every service, that no
	// error or log or test attributes to anything.
	//
	// Idle connections are cheap: a file descriptor here and a backend on the
	// server. The handshake they save is not. Ten is deliberately modest — it must
	// stay well under a stock max_connections of 100 once multiplied by however
	// many replicas a service runs.
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

	// MaxOpenConns bounds how many connections the pools may open at once. Zero
	// means unlimited, which is database/sql's own default and what this package
	// has always done.
	//
	// There is deliberately no default here: the right number depends on how many
	// replicas a deployment runs against one database, which a library cannot know.
	// The field exists so a service can state its answer where it builds the
	// config, rather than reaching into the pool after the fact — the handle is
	// cached, so that has to happen before anything else uses it, and an ordering
	// requirement nothing enforces is one somebody eventually gets wrong.
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
		db.SetMaxOpenConns(config.MaxOpenConns)

		err := postgres.Ping(ctx, db)
		if err != nil {
			return nil, fmt.Errorf("ping database: %w", err)
		}

		config.db = db
	}

	return config.db, nil
}

// DBSchema returns a database connection for the specified schema. It smartly caches and reuses connections for
// any given schema name.
//
// If the `create` parameter is true, and no connection exists for the specified schema, it will create the schema
// in the database before returning the connection.
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
		return nil, fmt.Errorf("ping database schema %s: %w", schema, err)
	}

	config.schemas[schema] = db

	return db, nil
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
