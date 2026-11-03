package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
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

			expectErr: postgres.ErrNoDBInContext,
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

	dsn := os.Getenv("POSTGRES_DSN")
	require.NotEmpty(t, dsn, "POSTGRES_DSN must point at a throwaway database")

	admin := openProbeDB(pgdriver.WithDSN(dsn))

	t.Cleanup(func() { require.NoError(t, admin.Close()) })

	staleCases := []struct {
		name       string
		staleConns int
		// terminated ends the session of each stale connection on the server, as a restart does. A
		// stale connection otherwise loses its socket on the client side.
		terminated bool
		deadline   bool
		expectErr  error
	}{
		{name: "Healthy", staleConns: 0},
		{name: "OneStaleConnection", staleConns: 1},
		{name: "MultipleStaleConnections", staleConns: 3},
		{name: "TerminatedSessions", staleConns: 3, terminated: true},
		{name: "CallerDeadline", staleConns: 3, deadline: true, expectErr: context.DeadlineExceeded},
	}

	for _, testCase := range staleCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// The presets check a pooled connection before handing it out. This pool checks none, so
			// every stale connection reaches the probe.
			pool := openProbeDB(pgdriver.WithDSN(dsn))
			// An expired probe can leave intentionally closed sockets in the pool.
			t.Cleanup(func() { requireClosed(t, pool.Close, net.ErrClosed) })

			connections := make([]*sql.Conn, 0, testCase.staleConns)
			for range testCase.staleConns {
				conn, connErr := pool.DB.Conn(t.Context())
				require.NoError(t, connErr)
				t.Cleanup(func() { requireClosed(t, conn.Close, sql.ErrConnDone) })

				connections = append(connections, conn)
			}

			for _, conn := range connections {
				if testCase.terminated {
					terminateSession(t, admin, conn)
				} else {
					// The driver discovers the dead peer on its next ping, as it does after a database
					// host restarts.
					require.NoError(t, conn.Raw(closeSocket))
				}

				require.NoError(t, conn.Close())
			}

			ctx := context.WithValue(t.Context(), postgres.ContextKey{}, pool)

			if testCase.deadline {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}

			err := postgres.Health(ctx)
			require.ErrorIs(t, err, testCase.expectErr)
		})
	}
}

// terminateSession ends the server session behind conn from admin, as a restart or a failover ends
// every session. PostgreSQL sends the session its admin_shutdown error before closing the socket.
func terminateSession(t *testing.T, admin *bun.DB, conn *sql.Conn) {
	t.Helper()

	var pid int

	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))

	// The timeout waits for the session to end, so its error is on the wire before the probe runs.
	var terminated bool

	require.NoError(t, admin.NewRaw("SELECT pg_terminate_backend(?, 5000)", pid).Scan(t.Context(), &terminated))
	require.True(t, terminated)
}

// requireClosed requires closeFunc to succeed, or to fail with tolerated.
func requireClosed(t *testing.T, closeFunc func() error, tolerated error) {
	t.Helper()

	err := closeFunc()
	if !errors.Is(err, tolerated) {
		require.NoError(t, err)
	}
}

// closeSocket closes the network connection under a pooled driver connection, and leaves the pool
// to discover the dead peer on its next ping.
func closeSocket(connection any) error {
	driverConn, ok := connection.(*pgdriver.Conn)
	if !ok {
		return fmt.Errorf("unexpected driver connection %T", connection)
	}

	return driverConn.Conn().Close()
}
