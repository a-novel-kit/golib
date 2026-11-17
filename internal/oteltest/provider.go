// Package oteltest installs the tracer providers golib's tests read spans from.
package oteltest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// RecordSpans installs a global tracer provider that records every span started until the test
// ends, and returns its recorder. A test that calls it must not run in parallel with another that
// reads the global provider.
func RecordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	Install(t, sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))

	return recorder
}

// Install sets provider as the global tracer provider until the test ends. Then it restores the
// previous provider and shuts provider down.
func Install(t *testing.T, provider *sdktrace.TracerProvider) {
	t.Helper()

	previous := otel.GetTracerProvider()

	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
	})
}
