package postgrespresets

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"slices"
	"sync"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
)

const (
	// MaxIdleConnsDefault is the number of idle connections a pool keeps when the
	// caller expresses no preference.
	//
	// database/sql keeps two, so past the second concurrent query every call opens
	// a fresh connection and discards it on release. Each opening costs a TCP round
	// trip, a TLS handshake and an authentication exchange. An idle connection costs
	// a file descriptor here and a backend on the server. Twenty matches the open
	// bound of [ConnectionDefault], so a burst keeps the connections it is about to
	// reuse.
	MaxIdleConnsDefault = 20
)

// Default is the standard postgres.Config implementation, backed by pgdriver.
// It opens its pool on first use and caches it. It is safe for concurrent use.
//
// PostgreSQL ends every session when it restarts or fails over. The pool drops each connection the
// server closed while it sat idle, so the next query runs on a live one.
type Default struct {
	options []pgdriver.Option

	// MaxIdleConns overrides how many idle connections the pool keeps. Zero means
	// MaxIdleConnsDefault; a negative value keeps none, matching database/sql.
	MaxIdleConns int

	// MaxOpenConns bounds how many connections the pool may open. Zero means
	// unlimited, matching database/sql.
	//
	// No default: the right number depends on how many replicas share the database,
	// which a library cannot know.
	//
	// The limit applies when the pool opens. The pool is cached, so changing the field
	// after the first connection has no effect.
	MaxOpenConns int

	// Cached connection to the primary database, opened on first use.
	db *bun.DB

	mu sync.RWMutex
}

// NewDefault returns a Default that connects using the given pgdriver options.
func NewDefault(options ...pgdriver.Option) *Default {
	return &Default{options: options}
}

// DB returns the main database connection.
func (config *Default) DB(ctx context.Context) (*bun.DB, error) {
	config.mu.Lock()
	defer config.mu.Unlock()

	if config.db == nil {
		db, err := config.openDB(ctx, config.options)
		if err != nil {
			return nil, fmt.Errorf("ping database: %w", err)
		}

		config.db = db
	}

	return config.db, nil
}

// Close closes the pool, once opened, and logs a failure: a process that is exiting has nothing
// else to do with it. A later [Default.DB] opens a new pool.
func (config *Default) Close() {
	config.mu.Lock()
	defer config.mu.Unlock()

	if config.db == nil {
		return
	}

	err := config.db.Close()
	if err != nil {
		log.Println("close postgres: " + err.Error())
	}

	config.db = nil
}

// Options returns a copy of the driver options the config was built with.
// `postgrestest.RunDBTest` reads them to derive sibling connections to other
// databases in the same cluster.
func (config *Default) Options() []pgdriver.Option {
	config.mu.RLock()
	defer config.mu.RUnlock()

	return slices.Clone(config.options)
}

// maxIdleConns resolves the configured override against the default. Callers hold
// the mutex.
func (config *Default) maxIdleConns() int {
	if config.MaxIdleConns == 0 {
		return MaxIdleConnsDefault
	}

	return config.MaxIdleConns
}

// openDB opens a pool from options, sized by the config, and pings it. A
// [pgdriver.WithResetSessionFunc] among options replaces checkIdleConn.
func (config *Default) openDB(ctx context.Context, options []pgdriver.Option) (*bun.DB, error) {
	options = append([]pgdriver.Option{pgdriver.WithResetSessionFunc(checkIdleConn)}, options...)

	sqldb := sql.OpenDB(pgdriver.NewConnector(options...))
	db := bun.NewDB(sqldb, pgdialect.New(), bun.WithDiscardUnknownColumns())
	db.SetMaxIdleConns(config.maxIdleConns())
	db.SetMaxOpenConns(config.MaxOpenConns)

	err := postgres.Ping(ctx, db)
	if err != nil {
		_ = db.Close()

		return nil, err
	}

	return db, nil
}
