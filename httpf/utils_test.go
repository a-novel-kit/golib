package httpf_test

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// TestMain installs the W3C trace-context propagator, which otelhttp reads at request time. The
// global default injects nothing, so TestNewPoolClient finds the traceparent header only with this
// one installed.
func TestMain(m *testing.M) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	os.Exit(m.Run())
}
