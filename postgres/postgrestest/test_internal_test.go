package postgrestest

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/a-novel-kit/golib/internal/pgtest"
)

// duplicateDatabaseError returns the error PostgreSQL answers CREATE DATABASE with, on a name that
// already exists.
func duplicateDatabaseError(t *testing.T) error {
	t.Helper()

	config := pgtest.Config(t)
	t.Cleanup(config.Close)

	db, err := config.DB(t.Context())
	require.NoError(t, err)

	name := "gotest_duplicate_" + strings.ToLower(rand.Text())

	_, err = db.NewRaw("CREATE DATABASE ?", bun.Ident(name)).Exec(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.NewRaw("DROP DATABASE IF EXISTS ? WITH (FORCE)", bun.Ident(name)).Exec(context.Background())
	})

	_, err = db.NewRaw("CREATE DATABASE ?", bun.Ident(name)).Exec(t.Context())
	require.Error(t, err)

	return err
}

func TestDBTestRetryCreate(t *testing.T) {
	t.Parallel()

	errTimeout := errors.New("read tcp: i/o timeout")
	errDuplicate := duplicateDatabaseError(t)

	testCases := []struct {
		name string

		// errs are what each attempt returns in turn; an attempt past them succeeds.
		errs []error

		// expectErr is part of the failure's message: pgdriver.Error holds a map, so errors.Is never
		// matches it.
		expectErr   string
		expectCalls int
	}{
		{
			name: "Success/FirstAttempt",

			expectCalls: 1,
		},
		{
			name: "Success/RetryAfterFailure",

			errs: []error{errTimeout},

			expectCalls: 2,
		},
		{
			// The first attempt timed out on the client after the server created the database, so
			// every later attempt finds it.
			name: "Success/RetryFindsTheDatabase",

			errs: []error{errTimeout, errDuplicate, errDuplicate, errDuplicate, errDuplicate},

			expectCalls: 2,
		},
		{
			// Another database took the name: no retry can make it ours.
			name: "Error/FirstAttemptFindsADatabase",

			errs: []error{errDuplicate},

			expectErr:   "SQLSTATE=42P04",
			expectCalls: 1,
		},
		{
			name: "Error/Exhausted",

			errs: []error{errTimeout, errTimeout, errTimeout, errTimeout, errTimeout},

			expectErr:   "i/o timeout",
			expectCalls: dbTestCreateAttempts,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// The bubble's fake clock runs the backoff between attempts at once.
			synctest.Test(t, func(t *testing.T) {
				var calls int

				err := dbTestRetryCreate(t.Context(), func(context.Context) error {
					calls++
					if calls <= len(testCase.errs) {
						return testCase.errs[calls-1]
					}

					return nil
				})

				if testCase.expectErr == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, testCase.expectErr)
				}

				require.Equal(t, testCase.expectCalls, calls)
			})
		})
	}
}
