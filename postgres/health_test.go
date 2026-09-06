package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/postgres"
)

func TestHealth(t *testing.T) {
	t.Parallel()

	pool := bun.NewDB(sql.OpenDB(pgdriver.NewConnector()), pgdialect.New())

	t.Cleanup(func() { require.NoError(t, pool.Close()) })

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	testCases := []struct {
		name string

		newContext func() context.Context

		expectErr error
	}{
		{
			name: "Error/MissingDatabase",

			newContext: t.Context,

			expectErr: postgres.ErrNoIDBInContext,
		},
		{
			name: "Error/TransactionDatabase",

			newContext: func() context.Context {
				return context.WithValue(t.Context(), postgres.ContextKey{}, bun.Tx{})
			},

			expectErr: postgres.ErrNoDbInContext,
		},
		{
			name: "Error/Cancelled",

			newContext: func() context.Context {
				return context.WithValue(cancelled, postgres.ContextKey{}, pool)
			},

			expectErr: context.Canceled,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := postgres.Health(testCase.newContext())
			require.ErrorIs(t, err, testCase.expectErr)
		})
	}
}
