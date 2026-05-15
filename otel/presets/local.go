package otelpresets

import (
	"context"
	"fmt"
	stdlog "log"
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

// Local configures OTEL to log traces & logs to stdout.
type Local struct {
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

// Init just prints a banner for local dev mode.
func (config *Local) Init() error {
	banner := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	fmt.Println(banner.Render("🚀 OpenTelemetry Local Mode: All traces and logs to stdout"))

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
	ctx := context.Background()

	if config.FlushTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, config.FlushTimeout)
		defer cancel()
	}

	if config.tp != nil {
		err := config.tp.Shutdown(ctx)
		if err != nil {
			stdlog.Printf("failed to shutdown tracer provider: %v\n", err)
		}
	}

	if config.lp != nil {
		err := config.lp.Shutdown(ctx)
		if err != nil {
			stdlog.Printf("failed to shutdown logger provider: %v\n", err)
		}
	}
}

func (config *Local) HttpHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}

func (config *Local) RpcInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
