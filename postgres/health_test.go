package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"testing"
	"time"

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

func TestHealthStalePool(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		staleConns int
		deadline   bool
		expectErr  error
	}{
		{name: "Healthy", staleConns: 0},
		{name: "OneStaleConnection", staleConns: 1},
		{name: "MultipleStaleConnections", staleConns: 3},
		{name: "CallerDeadline", staleConns: 3, deadline: true, expectErr: context.DeadlineExceeded},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pool, err := testConfig(t).DB(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() {
				// An expired probe can leave intentionally closed sockets in the pool.
				closeErr := pool.Close()
				if !errors.Is(closeErr, net.ErrClosed) {
					require.NoError(t, closeErr)
				}
			})

			connections := make([]*sql.Conn, 0, testCase.staleConns)
			for range testCase.staleConns {
				conn, connErr := pool.DB.Conn(t.Context())
				require.NoError(t, connErr)
				t.Cleanup(func() {
					closeErr := conn.Close()
					if !errors.Is(closeErr, sql.ErrConnDone) {
						require.NoError(t, closeErr)
					}
				})

				connections = append(connections, conn)
			}

			for _, conn := range connections {
				// Close only the socket: the driver must discover the dead peer on
				// its next ping, as it does after a database host restarts.
				err = conn.Raw(func(connection any) error {
					driverConn, ok := connection.(*pgdriver.Conn)
					require.True(t, ok)

					return driverConn.Conn().Close()
				})
				require.NoError(t, err)
				require.NoError(t, conn.Close())
			}

			ctx := context.WithValue(t.Context(), postgres.ContextKey{}, pool)

			if testCase.deadline {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}

			err = postgres.Health(ctx)
			require.ErrorIs(t, err, testCase.expectErr)
		})
	}
}
