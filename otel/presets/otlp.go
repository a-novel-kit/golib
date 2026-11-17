package otelpresets

import (
	"context"
	"fmt"
	"time"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*OTLP)(nil)

// keepEveryTrace samples every span, whatever a caller decided, except under a local parent that is
// not sampled. Only the liveness requests httpf.NewRouter leaves untraced run under such a parent.
var keepEveryTrace = sdktrace.ParentBased(
	sdktrace.AlwaysSample(),
	sdktrace.WithRemoteParentNotSampled(sdktrace.AlwaysSample()),
)

// OTLP is a Config that exports traces and logs over OTLP/HTTP. The standard OTEL_EXPORTER_OTLP_*
// variables choose the endpoint, its headers and the compression. OTEL_SERVICE_NAME names the
// service. The preset sets no exporter option of its own, so moving to another backend changes
// variables only.
type OTLP struct {
	// UntracedPaths does nothing: httpf.NewRouter leaves its liveness path untraced under every
	// preset.
	//
	// Deprecated: Drop the field; a later release removes it.
	UntracedPaths []string `json:"untracedPaths" yaml:"untracedPaths"`
	// FlushTimeout bounds how long Flush waits for buffered data to drain. Zero waits indefinitely.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	// Init builds tp and lp, so Flush shuts down the providers in use.
	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

// Init installs providers that export over OTLP/HTTP, and prints the OTLP banner. The propagator
// carries the trace context and the baggage of each call.
func (config *OTLP) Init() error {
	traceExporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	logExporter, err := otlploghttp.New(context.Background())
	if err != nil {
		return fmt.Errorf("create OTLP log exporter: %w", err)
	}

	printBanner(bannerExport, "📡 OpenTelemetry OTLP Mode: exporting traces and logs over OTLP/HTTP")

	config.tp = sdktrace.NewTracerProvider(sdktrace.WithSampler(keepEveryTrace), sdktrace.WithBatcher(traceExporter))
	config.lp = sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)))

	gootel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	)
	gootel.SetTracerProvider(config.tp)
	gootel.SetLoggerProvider(config.lp)

	return nil
}

// Flush shuts down the tracer and logger providers, which sends what they still buffer.
func (config *OTLP) Flush() {
	shutdownProviders(config.FlushTimeout, config.tp, config.lp)
}
