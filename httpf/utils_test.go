package httpf_test

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// TestMain installs the W3C trace-context propagator, which otelhttp reads at request time and
// which defaults to injecting nothing. Without it the trace-context assertion below passes over an
// empty header.
func TestMain(m *testing.M) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	os.Exit(m.Run())
}
