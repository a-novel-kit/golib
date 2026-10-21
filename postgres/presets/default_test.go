package postgrespresets_test

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"

	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

// concurrentQueries is how many statements the tests run at once. It sits above
// database/sql's own idle default of two and below the preset's, so the number
// of connections left idle afterwards tells the two apart.
const concurrentQueries = 5

// testConfig points the tests at the throwaway database POSTGRES_DSN names.
func testConfig(t *testing.T) *postgrespresets.Default {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn,
		"POSTGRES_DSN must point at a throwaway database — this package opens real connections")

	return postgrespresets.NewDefault(pgdriver.WithDSN(dsn))
}

// holdOpen runs concurrentQueries statements that overlap in time, forcing the pool
// to open that many connections, and returns once every one has been released back
// to it.
func holdOpen(ctx context.Context, t *testing.T, config *postgrespresets.Default) {
	t.Helper()

	db, err := config.DB(ctx)
	require.NoError(t, err)

	var wg sync.WaitGroup

	for range concurrentQueries {
		wg.Go(func() {
			// The statements have to overlap. pg_sleep holds each connection long enough
			// for the pool to open a second.
			_, queryErr := db.NewRaw("SELECT pg_sleep(0.2);").Exec(ctx)
			// require calls FailNow, which only works on the goroutine running the
			// test.
			assert.NoError(t, queryErr)
		})
	}

	wg.Wait()
}

func TestDefault(t *testing.T) {
	t.Parallel()

	// All five connections survive being released, so the next caller reuses them without a
	// fresh handshake.
	t.Run("Success/KeepsIdleConnections", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)

		ctx := t.Context()
		holdOpen(ctx, t, config)

		db, err := config.DB(ctx)
		require.NoError(t, err)

		require.Equal(t, concurrentQueries, db.Stats().Idle,
			"every released connection should have been kept, not closed and reopened later")
	})

	// A deployment that sets its own number gets it. The default applies only when none is set.
	t.Run("Success/HonorsTheIdleOverride", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)
		config.MaxIdleConns = 1

		ctx := t.Context()
		holdOpen(ctx, t, config)

		db, err := config.DB(ctx)
		require.NoError(t, err)

		require.Equal(t, 1, db.Stats().Idle)
	})

	// A negative override keeps nothing, matching database/sql.
	t.Run("Success/NegativeIdleOverrideKeepsNone", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)
		config.MaxIdleConns = -1

		ctx := t.Context()
		holdOpen(ctx, t, config)

		db, err := config.DB(ctx)
		require.NoError(t, err)

		require.Zero(t, db.Stats().Idle)
	})

	// The open limit has no default. A service states its own where it builds the config, and
	// the pool honors it on every connection it opens.
	t.Run("Success/BoundsOpenConnections", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)
		config.MaxOpenConns = 2

		ctx := t.Context()

		db, err := config.DB(ctx)
		require.NoError(t, err)

		require.Equal(t, 2, db.Stats().MaxOpenConnections)

		// The cap has to hold under contention, not just report itself: more callers
		// than connections must queue rather than open more.
		holdOpen(ctx, t, config)
		require.LessOrEqual(t, db.Stats().OpenConnections, 2)
	})

	// A library cannot know how many replicas share a database, so an unset limit keeps the
	// unlimited behavior of database/sql.
	t.Run("Success/OpenConnectionsUnlimitedByDefault", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)

		db, err := config.DB(t.Context())
		require.NoError(t, err)

		require.Zero(t, db.Stats().MaxOpenConnections, "0 is database/sql's encoding of unlimited")
	})

	t.Run("Success/BoundsSchemaOpenConnections", func(t *testing.T) {
		t.Parallel()

		config := testConfig(t)
		config.MaxOpenConns = 2

		schema := "max_open_connections_" + strings.ToLower(rand.Text())

		t.Cleanup(func() {
			require.NoError(t, config.DropSchema(context.Background(), schema))
		})

		db, err := config.DBSchema(t.Context(), schema, true)
		require.NoError(t, err)

		require.Equal(t, 2, db.Stats().MaxOpenConnections)
	})
}
