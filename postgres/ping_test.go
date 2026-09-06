//nolint:testpackage
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pingerFunc func(ctx context.Context) error

func (fn pingerFunc) PingContext(ctx context.Context) error {
	return fn(ctx)
}

func TestPing(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")

	testCases := []struct {
		name string

		context  func(t *testing.T) context.Context
		pinger   pingerFunc
		timeout  time.Duration
		interval time.Duration

		expectError error
		expectCalls int
	}{
		{
			name: "Success",
			context: func(t *testing.T) context.Context {
				t.Helper()

				return t.Context()
			},
			pinger: func(context.Context) error {
				return nil
			},
			timeout:     time.Second,
			interval:    time.Millisecond,
			expectCalls: 1,
		},
		{
			name: "Error/CallerDeadline",
			context: func(t *testing.T) context.Context {
				t.Helper()

				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
				t.Cleanup(cancel)

				return ctx
			},
			pinger: func(ctx context.Context) error {
				<-ctx.Done()

				return errFoo
			},
			timeout:     time.Second,
			interval:    time.Millisecond,
			expectError: context.DeadlineExceeded,
			expectCalls: 1,
		},
		{
			name: "Error/OverallDeadline",
			context: func(t *testing.T) context.Context {
				t.Helper()

				return t.Context()
			},
			pinger: func(ctx context.Context) error {
				<-ctx.Done()

				return errFoo
			},
			timeout:     time.Millisecond,
			interval:    time.Millisecond,
			expectError: context.DeadlineExceeded,
			expectCalls: 1,
		},
		{
			name: "Error/RetryWaitDeadline",
			context: func(t *testing.T) context.Context {
				t.Helper()

				return t.Context()
			},
			pinger: func(context.Context) error {
				return errFoo
			},
			timeout:     time.Millisecond,
			interval:    time.Second,
			expectError: context.DeadlineExceeded,
			expectCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			err := ping(testCase.context(t), pingerFunc(func(ctx context.Context) error {
				calls++

				return testCase.pinger(ctx)
			}), testCase.timeout, testCase.interval)

			if testCase.expectError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, testCase.expectError)
				require.ErrorIs(t, err, errFoo)
			}

			require.Equal(t, testCase.expectCalls, calls)
		})
	}
}
