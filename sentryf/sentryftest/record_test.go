package sentryftest_test

import (
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/sentryf"
	"github.com/a-novel-kit/golib/sentryf/sentryftest"
)

//nolint:paralleltest // Installs the process-wide Sentry reporter.
func TestRecord(t *testing.T) {
	t.Run("Success/KeepsEvents", func(t *testing.T) {
		transport := sentryftest.Record(t)

		sentryf.Capture(t.Context(), "rotate-keys", errors.New("rotate"))

		events := transport.Events()
		require.Len(t, events, 1)
		require.Equal(t, sentryftest.Release, events[0].Release)
		require.Equal(t, sentryftest.Environment, events[0].Environment)
	})

	// The sub-test above ended, so its reporter is gone.
	require.False(t, sentry.CurrentHub().Client().IsEnabled())
}
