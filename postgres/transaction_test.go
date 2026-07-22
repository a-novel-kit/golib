package postgres_test

import (
	"context"
	"embed"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
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
// environment variable production uses so a developer configures one thing.
// There is no default: a wrong guess would silently target whatever happens to
// listen on the usual port, and these tests write.
func testConfig(t *testing.T) postgres.Config {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn,
		"POSTGRES_DSN must point at a throwaway database — this package wraps a real one and cannot be tested without it")

	return postgrespresets.NewDefault(pgdriver.WithDSN(dsn))
}

// writeProbe inserts one row through GetContext — the same way every consumer's
// data-access code resolves its handle. That is what makes these tests
// meaningful: they exercise the path that silently escaped the transaction,
// not a handle threaded in by the test itself.
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

// TestWithinTxRollback is the regression test for the defect this package
// carried: it fails against an implementation that leaves the pool on the
// context, because the insert would then commit on its own and outlive the
// rollback.
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

// TestRunInTxDoesNotCoverTheContext pins the deprecated function's behaviour.
// It asserts the row SURVIVES a rolled-back transaction, which is not a mistake:
// it documents, in the repository rather than in a review thread, exactly what
// WithinTx was introduced to fix.
//
// It is expected to be deleted with RunInTx, not repaired.
func TestRunInTxDoesNotCoverTheContext(t *testing.T) {
	t.Parallel()

	id := "run-in-tx-escape"

	postgres.RunDBTest(t, testConfig(t), migrations, func(ctx context.Context, t *testing.T) {
		t.Helper()

		err := postgres.RunInTx(ctx, nil, func(ctx context.Context, _ bun.IDB) error {
			writeProbe(ctx, t, id)

			return errRollback
		})
		require.ErrorIs(t, err, errRollback)

		require.Equal(t, 1, probeCount(ctx, t, id), "the write escaped the transaction and committed on its own")
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

// TestWithinTxNested covers the joining rule: an inner failure discards the
// outer unit of work, because the inner call takes part in the transaction
// already in progress rather than opening a savepoint it could roll back alone.
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

// TestInTxNoContext covers a context nothing seeded. InTx answers "is a
// transaction open", and no database handle at all is not one.
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
