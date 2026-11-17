package otelpresets

import (
	gootel "go.opentelemetry.io/otel"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Disabled)(nil)

// Disabled is a Config whose providers are no-ops, so no traces or logs are exported.
type Disabled struct{}

// Init installs the no-op providers and prints a banner announcing that tracing is off. It drops
// the date and time from the standard logger's lines, which a terminal shows on its own.
func (config *Disabled) Init() error {
	printBanner(bannerStart, "🚀 OpenTelemetry Disabled Mode: no tracing")
	trimLogFlags()

	gootel.SetTextMapPropagator(propagation.TraceContext{})
	gootel.SetTracerProvider(tracenoop.NewTracerProvider())
	gootel.SetLoggerProvider(lognoop.NewLoggerProvider())

	return nil
}

// Flush does nothing: the no-op providers buffer nothing.
func (config *Disabled) Flush() {}
