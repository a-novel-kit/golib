package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/internal/pgtest"
	"github.com/a-novel-kit/golib/postgres"
)

func TestNewContext(t *testing.T) {
	t.Parallel()

	config := pgtest.Config(t)

	ctx, err := postgres.NewContext(t.Context(), config)
	require.NoError(t, err)

	pool, err := config.DB(t.Context())
	require.NoError(t, err)

	db, err := postgres.GetContext(ctx)
	require.NoError(t, err)
	require.Same(t, pool, db)

	// A detached context, such as a background task's, reaches the same pool once transferred.
	db, err = postgres.GetContext(postgres.TransferContext(ctx, t.Context()))
	require.NoError(t, err)
	require.Same(t, pool, db)

	_, err = postgres.GetContext(t.Context())
	require.ErrorIs(t, err, postgres.ErrNoIDBInContext)
}
