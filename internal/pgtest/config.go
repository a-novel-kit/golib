// Package pgtest points golib's database tests at the throwaway PostgreSQL database POSTGRES_DSN
// names.
package pgtest

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

// Config returns a preset for the database POSTGRES_DSN names, the variable production uses, so a
// developer configures one thing. It has no default: the tests write, and a guessed target could be
// any database listening on the usual port.
func Config(t *testing.T) *postgrespresets.Default {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn, "POSTGRES_DSN must point at a throwaway database: golib's database tests write to it")

	return postgrespresets.NewDefault(pgdriver.WithDSN(dsn))
}

// Open opens a pool from options without pinging it, so a test decides when it first connects.
func Open(options ...pgdriver.Option) *bun.DB {
	return bun.NewDB(sql.OpenDB(pgdriver.NewConnector(options...)), pgdialect.New())
}
