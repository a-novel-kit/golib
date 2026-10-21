//nolint:testpackage
package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pingerFunc scripts the answers of a database handle to ping.
type pingerFunc func(ctx context.Context) error

func (fn pingerFunc) PingContext(ctx context.Context) error {
	return fn(ctx)
}

// errPingFailed is the failure the scripted pingers answer with.
var errPingFailed = errors.New("ping failed")

// pingAnswers answers every ping.
func pingAnswers(context.Context) error { return nil }

// pingFails fails every ping at once.
func pingFails(context.Context) error { return errPingFailed }

// pingHangs fails a ping only once its context ends.
func pingHangs(ctx context.Context) error {
	<-ctx.Done()

	return errPingFailed
}

func TestPing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// callerTimeout, when set, bounds the context the caller passes.
		callerTimeout time.Duration
		pinger        pingerFunc
		timeout       time.Duration
		interval      time.Duration

		expectError error
		expectCalls int
	}{
		{
			name:        "Success",
			pinger:      pingAnswers,
			timeout:     time.Second,
			interval:    time.Millisecond,
			expectCalls: 1,
		},
		{
			name:          "Error/CallerDeadline",
			callerTimeout: time.Millisecond,
			pinger:        pingHangs,
			timeout:       time.Second,
			interval:      time.Millisecond,
			expectError:   context.DeadlineExceeded,
			expectCalls:   1,
		},
		{
			name:        "Error/OverallDeadline",
			pinger:      pingHangs,
			timeout:     time.Millisecond,
			interval:    time.Millisecond,
			expectError: context.DeadlineExceeded,
			expectCalls: 1,
		},
		{
			name:        "Error/RetryWaitDeadline",
			pinger:      pingFails,
			timeout:     time.Millisecond,
			interval:    time.Second,
			expectError: context.DeadlineExceeded,
			expectCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			if testCase.callerTimeout > 0 {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, testCase.callerTimeout)
				defer cancel()
			}

			calls := 0
			counted := pingerFunc(func(ctx context.Context) error {
				calls++

				return testCase.pinger(ctx)
			})

			err := ping(ctx, counted, testCase.timeout, testCase.interval, nil)

			if testCase.expectError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, testCase.expectError)
				require.ErrorIs(t, err, errPingFailed)
			}

			require.Equal(t, testCase.expectCalls, calls)
		})
	}
}

func TestPingRetries(t *testing.T) {
	t.Parallel()

	errPermanent := errors.New("permanent failure")
	badConnection := func(err error) bool { return errors.Is(err, driver.ErrBadConn) }

	testCases := []struct {
		name string

		errors    []error
		retryable func(error) bool
		cancel    bool

		expectError error
		expectCalls int
	}{
		{
			name:        "Success/DiscardedConnection",
			errors:      []error{driver.ErrBadConn, nil},
			retryable:   badConnection,
			expectCalls: 2,
		},
		{
			name:        "Success/WrappedDiscardedConnection",
			errors:      []error{fmt.Errorf("wrapped: %w", driver.ErrBadConn), nil},
			retryable:   badConnection,
			expectCalls: 2,
		},
		{
			name:        "Success/StartupRetriesEveryError",
			errors:      []error{errPermanent, nil},
			expectCalls: 2,
		},
		{
			name:        "Error/Permanent",
			errors:      []error{errPermanent, nil},
			retryable:   badConnection,
			expectError: errPermanent,
			expectCalls: 1,
		},
		{
			name:        "Error/PermanentAfterDiscard",
			errors:      []error{driver.ErrBadConn, errPermanent, nil},
			retryable:   badConnection,
			expectError: errPermanent,
			expectCalls: 2,
		},
		{
			name:        "Error/CanceledKeepsDiscardedError",
			errors:      []error{driver.ErrBadConn, nil},
			retryable:   badConnection,
			cancel:      true,
			expectError: context.Canceled,
			expectCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			calls := 0
			scripted := pingerFunc(func(context.Context) error {
				if testCase.cancel {
					cancel()
				}

				err := testCase.errors[calls]
				calls++

				return err
			})

			err := ping(ctx, scripted, time.Second, time.Millisecond, testCase.retryable)

			require.ErrorIs(t, err, testCase.expectError)
			require.Equal(t, testCase.expectCalls, calls)

			if testCase.cancel {
				require.ErrorIs(t, err, driver.ErrBadConn)
			}
		})
	}
}
