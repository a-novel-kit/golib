package postgres_test

import (
	"context"
	"embed"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

// Migrations is the probe schema the transaction tests write to.
//
//go:embed testdata/migrations/*.sql
var migrations embed.FS

// errRollback is the failure a callback returns to trigger a rollback. It
// stands for any error a real unit of work might raise; only its identity
// matters.
var errRollback = errors.New("rollback")

// testConfig points the tests at a throwaway database, named by the same
// environment variable production uses so a developer configures one thing. The
// variable has no default, since these tests write and a guessed target could be
// any database listening on the usual port.
func testConfig(t *testing.T) postgres.Config {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn,
		"POSTGRES_DSN must point at a throwaway database — this package wraps a real one and cannot be tested without it")

	return postgrespresets.NewDefault(pgdriver.WithDSN(dsn))
}

// writeProbe inserts one row through GetContext, the way every consumer's
// data-access code resolves its handle, so the tests exercise the path a real
// caller takes.
func writeProbe(ctx context.Context, t *testing.T, id string) {
	t.Helper()

	db, err := postgres.GetContext(ctx)
	require.NoError(t, err)

	_, err = db.NewRaw("INSERT INTO transaction_probe (id) VALUES (?0)", id).Exec(ctx)
	require.NoError(t, err)
}

func probeCount(ctx context.Context, t *testing.T, id string) int {
	t.Helper()

	db, err := postgres.GetContext(ctx)
	require.NoError(t, err)

	var count int

	err = db.NewRaw("SELECT count(*) FROM transaction_probe WHERE id = ?0", id).Scan(ctx, &count)
	require.NoError(t, err)

	return count
}

// TestWithinTxRollback pins that the callback's writes go through the transaction.
// The rollback takes the insert with it.
func TestWithinTxRollback(t *testing.T) {
	t.Parallel()

	id := "rollback"

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		err := postgres.WithinTx(ctx, nil, func(ctx context.Context) error {
			writeProbe(ctx, t, id)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		require.Equal(t, 0, probeCount(ctx, t, id))
	})
}

func TestWithinTxCommit(t *testing.T) {
	t.Parallel()

	id := "commit"

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		err := postgres.WithinTx(ctx, nil, func(ctx context.Context) error {
			writeProbe(ctx, t, id)

			return nil
		})
		require.NoError(t, err)

		require.Equal(t, 1, probeCount(ctx, t, id))
	})
}

// TestWithinTxNested covers the joining rule: the inner call takes part in the
// transaction already in progress, so an inner failure discards the outer unit
// of work too.
func TestWithinTxNested(t *testing.T) {
	t.Parallel()

	outer := "nested-outer"
	inner := "nested-inner"

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		err := postgres.WithinTx(ctx, nil, func(ctx context.Context) error {
			writeProbe(ctx, t, outer)

			return postgres.WithinTx(ctx, nil, func(ctx context.Context) error {
				writeProbe(ctx, t, inner)

				return errRollback
			})
		})
		require.ErrorIs(t, err, errRollback)

		require.Equal(t, 0, probeCount(ctx, t, outer))
		require.Equal(t, 0, probeCount(ctx, t, inner))
	})
}

func TestTransactorDelegates(t *testing.T) {
	t.Parallel()

	id := "transactor"

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		err := postgres.NewTransactor(nil).WithinTx(ctx, func(ctx context.Context) error {
			writeProbe(ctx, t, id)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		require.Equal(t, 0, probeCount(ctx, t, id))
	})
}

func TestInTx(t *testing.T) {
	t.Parallel()

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		require.False(t, postgres.InTx(ctx), "the pool is on the context outside a transaction")

		err := postgres.WithinTx(ctx, nil, func(ctx context.Context) error {
			require.True(t, postgres.InTx(ctx), "the transaction is on the context inside WithinTx")

			return nil
		})
		require.NoError(t, err)
	})
}

// TestInTxNoContext covers a context nothing seeded: InTx answers "is a
// transaction open", which a missing database handle is not.
func TestInTxNoContext(t *testing.T) {
	t.Parallel()

	require.False(t, postgres.InTx(t.Context()))
}

func TestWithinTxNoContext(t *testing.T) {
	t.Parallel()

	err := postgres.WithinTx(t.Context(), nil, func(context.Context) error {
		t.Error("the callback must not run when the context carries no database")

		return nil
	})
	require.ErrorIs(t, err, postgres.ErrNoIDBInContext)
}
