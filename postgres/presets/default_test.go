package postgrespresets_test

import (
	"context"
	"os"
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
		wg.Add(1)

		go func() {
			defer wg.Done()

			// The statements have to overlap. pg_sleep holds each connection long enough
			// for the pool to open a second.
			_, queryErr := db.NewRaw("SELECT pg_sleep(0.2);").Exec(ctx)
			// require calls FailNow, which only works on the goroutine running the
			// test.
			assert.NoError(t, queryErr)
		}()
	}

	wg.Wait()
}

// TestDefaultKeepsIdleConnections pins the pool's idle default. All five connections
// survive being released. The next caller reuses them without a fresh handshake.
func TestDefaultKeepsIdleConnections(t *testing.T) {
	t.Parallel()

	config := testConfig(t)

	ctx := t.Context()
	holdOpen(ctx, t, config)

	db, err := config.DB(ctx)
	require.NoError(t, err)

	require.Equal(t, concurrentQueries, db.Stats().Idle,
		"every released connection should have been kept, not closed and reopened later")
}

// TestDefaultHonoursTheOverride covers the escape hatch: a deployment that sets
// its own number gets it, and the default applies only when none was expressed.
func TestDefaultHonoursTheOverride(t *testing.T) {
	t.Parallel()

	config := testConfig(t)
	config.MaxIdleConns = 1

	ctx := t.Context()
	holdOpen(ctx, t, config)

	db, err := config.DB(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, db.Stats().Idle)
}

// TestDefaultNegativeOverrideKeepsNone pins the one value that is not "unset": a
// negative override keeps nothing, matching database/sql.
func TestDefaultNegativeOverrideKeepsNone(t *testing.T) {
	t.Parallel()

	config := testConfig(t)
	config.MaxIdleConns = -1

	ctx := t.Context()
	holdOpen(ctx, t, config)

	db, err := config.DB(ctx)
	require.NoError(t, err)

	require.Zero(t, db.Stats().Idle)
}
