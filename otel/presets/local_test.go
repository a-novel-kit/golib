package otelpresets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

func TestLocal(t *testing.T) {
	t.Parallel()

	t.Run("Success/TraceProviderBuiltOnce", func(t *testing.T) {
		t.Parallel()

		config := &otelpresets.Local{}
		t.Cleanup(config.Flush)

		first, err := config.GetTraceProvider()
		require.NoError(t, err)

		second, err := config.GetTraceProvider()
		require.NoError(t, err)
		require.Same(t, first, second)
	})

	t.Run("Success/LoggerBuiltOnce", func(t *testing.T) {
		t.Parallel()

		config := &otelpresets.Local{}
		t.Cleanup(config.Flush)

		first, err := config.GetLogger()
		require.NoError(t, err)

		second, err := config.GetLogger()
		require.NoError(t, err)
		require.Same(t, first, second)
	})
}
