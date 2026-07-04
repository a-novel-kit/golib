package otelpresets

import (
	"fmt"
	"net/http"
	"os"

	"charm.land/lipgloss/v2"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Disabled)(nil)

// Disabled is a Config whose providers are no-ops, so no traces or logs are
// exported.
type Disabled struct{}

// Init prints a startup banner announcing that OpenTelemetry tracing and
// logging are disabled.
func (config *Disabled) Init() error {
	banner := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	_, _ = fmt.Fprintln(os.Stdout, banner.Render("🚀 OpenTelemetry Disabled Mode: no tracing"))

	return nil
}

func (config *Disabled) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.TraceContext{}, nil
}

func (config *Disabled) GetTraceProvider() (trace.TracerProvider, error) {
	return tracenoop.NewTracerProvider(), nil
}

func (config *Disabled) GetLogger() (log.LoggerProvider, error) {
	return lognoop.NewLoggerProvider(), nil
}

func (config *Disabled) Flush() {
}

func (config *Disabled) HttpHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}

func (config *Disabled) RpcInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
