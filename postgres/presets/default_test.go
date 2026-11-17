package postgrespresets_test

import (
	"context"
	"crypto/rand"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/internal/pgtest"
	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

// concurrentQueries is how many statements the tests run at once. It sits above
// database/sql's own idle default of two and below the preset's, so the number
// of connections left idle afterwards tells the two apart.
const concurrentQueries = 5

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

	// A restart or a failover ends every session, idle ones included. The pool replaces each
	// connection the server closed, so every statement after it succeeds.
	t.Run("Success/ReplacesConnectionsTheServerClosed", func(t *testing.T) {
		t.Parallel()

		// The application name tells the sessions of this pool apart from those of parallel tests.
		name := "golib-test-" + rand.Text()
		config := postgrespresets.NewDefault(append(pgtest.Config(t).Options(), pgdriver.WithApplicationName(name))...)

		ctx := t.Context()
		holdOpen(ctx, t, config)

		admin, err := pgtest.Config(t).DB(ctx)
		require.NoError(t, err)

		// The timeout waits for each session to end, so its error is on the wire before the next
		// statements run.
		var terminated []bool

		err = admin.NewRaw(
			"SELECT pg_terminate_backend(pid, 5000) FROM pg_stat_activity WHERE application_name = ?", name,
		).Scan(ctx, &terminated)
		require.NoError(t, err)
		require.Equal(t, slices.Repeat([]bool{true}, concurrentQueries), terminated)

		holdOpen(ctx, t, config)
	})

	// The pool keeps a live connection, however long it sat idle.
	t.Run("Success/ReusesLiveIdleConnections", func(t *testing.T) {
		t.Parallel()

		// The driver leaves a read deadline behind, which passes while the connection sits idle.
		readTimeout := pgdriver.WithReadTimeout(100 * time.Millisecond)
		config := postgrespresets.NewDefault(append(pgtest.Config(t).Options(), readTimeout)...)

		ctx := t.Context()

		db, err := config.DB(ctx)
		require.NoError(t, err)

		var before, after int

		require.NoError(t, db.NewRaw("SELECT pg_backend_pid()").Scan(ctx, &before))
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, db.NewRaw("SELECT pg_backend_pid()").Scan(ctx, &after))

		require.Equal(t, before, after)
	})

	// A pool keeps up to MaxIdleConns released connections, so the next callers reuse them without a
	// fresh handshake. Zero keeps MaxIdleConnsDefault, above the queries holdOpen runs, and a negative
	// value keeps none, matching database/sql.
	idleCases := []struct {
		name string

		maxIdleConns int

		expectIdle int
	}{
		{name: "Success/KeepsIdleConnections", maxIdleConns: 0, expectIdle: concurrentQueries},
		{name: "Success/HonorsTheIdleOverride", maxIdleConns: 1, expectIdle: 1},
		{name: "Success/NegativeIdleOverrideKeepsNone", maxIdleConns: -1, expectIdle: 0},
	}

	for _, testCase := range idleCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := pgtest.Config(t)
			config.MaxIdleConns = testCase.maxIdleConns

			ctx := t.Context()
			holdOpen(ctx, t, config)

			db, err := config.DB(ctx)
			require.NoError(t, err)

			require.Equal(t, testCase.expectIdle, db.Stats().Idle)
		})
	}

	// The open limit has no default. A service states its own where it builds the config, and
	// the pool honors it on every connection it opens.
	t.Run("Success/BoundsOpenConnections", func(t *testing.T) {
		t.Parallel()

		config := pgtest.Config(t)
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

		config := pgtest.Config(t)

		db, err := config.DB(t.Context())
		require.NoError(t, err)

		require.Zero(t, db.Stats().MaxOpenConnections, "0 is database/sql's encoding of unlimited")
	})
}
