package otelpresets

import (
	"fmt"
	"os"
	"time"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Local)(nil)

// Local is a Config that writes traces and logs to standard output, for local development.
type Local struct {
	// FlushTimeout bounds how long Flush waits for buffered data to drain. Zero waits indefinitely.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	// Init builds tp and lp, so Flush shuts down the providers in use.
	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

// Init installs providers that print every span and log record to standard output, and prints the
// local development banner. It drops the date and time from the standard logger's lines, which a
// terminal shows on its own.
func (config *Local) Init() error {
	traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint(), stdouttrace.WithWriter(os.Stdout))
	if err != nil {
		return fmt.Errorf("create stdout trace exporter: %w", err)
	}

	logExporter, err := stdoutlog.New(stdoutlog.WithPrettyPrint(), stdoutlog.WithWriter(os.Stdout))
	if err != nil {
		return fmt.Errorf("create stdout log exporter: %w", err)
	}

	printBanner(bannerStart, "🚀 OpenTelemetry Local Mode: All traces and logs to stdout")
	trimLogFlags()

	config.tp = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithBatcher(traceExporter),
	)
	config.lp = sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)))

	gootel.SetTextMapPropagator(propagation.TraceContext{})
	gootel.SetTracerProvider(config.tp)
	gootel.SetLoggerProvider(config.lp)

	return nil
}

// Flush shuts down the tracer and logger providers, which sends what they still buffer.
func (config *Local) Flush() {
	shutdownProviders(config.FlushTimeout, config.tp, config.lp)
}
