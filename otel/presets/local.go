package otelpresets

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"charm.land/lipgloss/v2"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Local)(nil)

// Local is a Config that writes traces and logs to standard output, for local
// development.
type Local struct {
	// FlushTimeout bounds how long Flush waits for buffered data to drain. Zero
	// waits indefinitely.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

// Init prints the local development banner.
func (config *Local) Init() error {
	banner := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	_, _ = fmt.Fprintln(os.Stdout, banner.Render("🚀 OpenTelemetry Local Mode: All traces and logs to stdout"))

	return nil
}

func (config *Local) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.TraceContext{}, nil
}

func (config *Local) GetTraceProvider() (trace.TracerProvider, error) {
	traceExporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
		stdouttrace.WithWriter(os.Stdout),
	)
	if err != nil {
		return nil, err
	}

	config.tp = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(traceExporter),
	)

	return config.tp, nil
}

func (config *Local) GetLogger() (log.LoggerProvider, error) {
	logExporter, err := stdoutlog.New(
		stdoutlog.WithPrettyPrint(),
		stdoutlog.WithWriter(os.Stdout),
	)
	if err != nil {
		return nil, err
	}

	config.lp = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	return config.lp, nil
}

func (config *Local) Flush() {
	shutdownProviders(config.FlushTimeout, config.tp, config.lp)
}

func (config *Local) HTTPHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}

func (config *Local) RPCInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
