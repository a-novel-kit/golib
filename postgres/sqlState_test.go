package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/internal/pgtest"
	"github.com/a-novel-kit/golib/postgres"
)

func TestSQLState(t *testing.T) {
	t.Parallel()

	config := pgtest.Config(t)
	t.Cleanup(config.Close)

	db, err := config.DB(t.Context())
	require.NoError(t, err)

	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// A temporary table lives in this session alone, so parallel tests never share it.
	_, err = conn.ExecContext(t.Context(), "CREATE TEMPORARY TABLE probe (id int CONSTRAINT probe_pk PRIMARY KEY)")
	require.NoError(t, err)

	_, err = conn.ExecContext(t.Context(), "INSERT INTO probe VALUES (1)")
	require.NoError(t, err)

	_, duplicate := conn.ExecContext(t.Context(), "INSERT INTO probe VALUES (1)")
	_, division := conn.ExecContext(t.Context(), "SELECT 1 / 0")

	testCases := []struct {
		name string

		err error

		expectCode       string
		expectConstraint string
	}{
		{
			name:             "Success/Constraint",
			err:              fmt.Errorf("insert: %w", duplicate),
			expectCode:       "23505",
			expectConstraint: "probe_pk",
		},
		{name: "Success/NoConstraint", err: division, expectCode: "22012"},
		{name: "Success/NotPostgres", err: errors.New("connection refused")},
		{name: "Success/Nil"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			code, constraint := postgres.SQLState(testCase.err)
			require.Equal(t, testCase.expectCode, code)
			require.Equal(t, testCase.expectConstraint, constraint)
		})
	}
}
