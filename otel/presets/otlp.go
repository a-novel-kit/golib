package otelpresets

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"

	"charm.land/lipgloss/v2"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*OTLP)(nil)

// keepEveryTrace samples every span, whatever a caller decided, except under a local parent that is
// not sampled. Only the requests of [OTLP.UntracedPaths] run under such a parent.
var keepEveryTrace = sdktrace.ParentBased(
	sdktrace.AlwaysSample(),
	sdktrace.WithRemoteParentNotSampled(sdktrace.AlwaysSample()),
)

// OTLP is a Config that exports traces and logs over OTLP/HTTP. The standard OTEL_EXPORTER_OTLP_*
// variables choose the endpoint, its headers and the compression. OTEL_SERVICE_NAME names the
// service. The preset sets no exporter option of its own, so moving to another backend changes
// variables only.
type OTLP struct {
	// UntracedPaths lists the request paths that produce no span, such as a liveness probe's.
	UntracedPaths []string `json:"untracedPaths" yaml:"untracedPaths"`
	// FlushTimeout bounds how long Flush waits for buffered data to drain. Zero waits indefinitely.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	// The getters build tp and lp on their first call and return them on later calls, so Flush
	// shuts down the providers in use.
	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

// Init prints the OTLP startup banner.
func (config *OTLP) Init() error {
	banner := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	_, _ = fmt.Fprintln(os.Stdout, banner.Render("📡 OpenTelemetry OTLP Mode: exporting traces and logs over OTLP/HTTP"))

	return nil
}

func (config *OTLP) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	), nil
}

func (config *OTLP) GetTraceProvider() (trace.TracerProvider, error) {
	if config.tp != nil {
		return config.tp, nil
	}

	exporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	config.tp = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(keepEveryTrace),
		sdktrace.WithBatcher(exporter),
	)

	return config.tp, nil
}

func (config *OTLP) GetLogger() (log.LoggerProvider, error) {
	if config.lp != nil {
		return config.lp, nil
	}

	exporter, err := otlploghttp.New(context.Background())
	if err != nil {
		return nil, fmt.Errorf("create OTLP log exporter: %w", err)
	}

	config.lp = sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))

	return config.lp, nil
}

// Flush shuts down tracer and logger providers.
func (config *OTLP) Flush() {
	shutdownProviders(config.FlushTimeout, config.tp, config.lp)
}

// HTTPHandler traces each request except those to [OTLP.UntracedPaths]. Those requests run under a
// parent span that is not sampled, and the spans their handlers open are dropped with it.
func (config *OTLP) HTTPHandler() func(http.Handler) http.Handler {
	traced := otelhttp.NewMiddleware("")

	return func(next http.Handler) http.Handler {
		tracedNext := traced(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(config.UntracedPaths, r.URL.Path) {
				next.ServeHTTP(w, r.WithContext(untracedContext(r.Context())))

				return
			}

			tracedNext.ServeHTTP(w, r)
		})
	}
}

func (config *OTLP) RPCInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// untracedContext returns ctx under a local parent span that is not sampled.
func untracedContext(ctx context.Context) context.Context {
	var (
		traceID trace.TraceID
		spanID  trace.SpanID
	)

	// crypto/rand.Read never fails.
	_, _ = rand.Read(traceID[:])
	_, _ = rand.Read(spanID[:])

	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
}
