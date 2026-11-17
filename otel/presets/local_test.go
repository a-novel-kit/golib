package otelpresets_test

import (
	"log"
	"testing"

	"github.com/stretchr/testify/require"

	gootel "go.opentelemetry.io/otel"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/otel"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

// restoreGlobals puts back the no-op providers and the standard logger's flags once the test ends.
func restoreGlobals(t *testing.T) {
	t.Helper()

	flags := log.Flags()

	t.Cleanup(func() {
		gootel.SetTracerProvider(tracenoop.NewTracerProvider())
		gootel.SetLoggerProvider(lognoop.NewLoggerProvider())
		log.SetFlags(flags)
	})
}

//nolint:paralleltest // Init sets the global providers and the standard logger's flags.
func TestLocal(t *testing.T) {
	restoreGlobals(t)
	log.SetFlags(log.LstdFlags)

	config := &otelpresets.Local{}
	require.NoError(t, otel.Init(config))
	t.Cleanup(config.Flush)

	_, ok := gootel.GetTracerProvider().(*sdktrace.TracerProvider)
	require.True(t, ok, "Init installs the SDK tracer provider")
	require.Zero(t, log.Flags()&(log.Ldate|log.Ltime), "a terminal shows the time on its own")
}

//nolint:paralleltest // Init sets the global providers and the standard logger's flags.
func TestDisabled(t *testing.T) {
	restoreGlobals(t)
	log.SetFlags(log.LstdFlags)

	require.NoError(t, otel.Init(&otelpresets.Disabled{}))

	_, span := otel.Tracer().Start(t.Context(), "dropped")
	defer span.End()

	require.False(t, span.IsRecording())
	require.Zero(t, log.Flags()&(log.Ldate|log.Ltime))
}
