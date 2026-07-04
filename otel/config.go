package otel

import (
	"fmt"
	"net/http"

	"google.golang.org/grpc"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// A Config wires one OpenTelemetry backend into the process. Each implementation
// targets a single destination for traces and logs; the presets subpackage ships
// the ready-made ones (standard output, Google Cloud, Sentry, and a disabled
// no-op). Pass a Config to Init to install its providers as the global state.
type Config interface {
	// Init sets up the backend's SDK clients and prints its startup banner.
	Init() error
	// GetPropagators returns the propagator that extracts and injects trace
	// context across service boundaries.
	GetPropagators() (propagation.TextMapPropagator, error)
	// GetTraceProvider returns the tracer provider to install globally.
	GetTraceProvider() (trace.TracerProvider, error)
	// GetLogger returns the logger provider to install globally.
	GetLogger() (log.LoggerProvider, error)
	// Flush drains buffered spans and logs; run it before the process exits.
	Flush()
	// HttpHandler returns middleware that instruments incoming HTTP requests.
	HttpHandler() func(http.Handler) http.Handler
	// RpcInterceptor returns the server option that instruments incoming gRPC calls.
	RpcInterceptor() grpc.ServerOption
}

// Init runs the backend's own setup, then installs its propagator, tracer, and
// logger providers as the process-global OpenTelemetry state. A nil config leaves
// telemetry disabled and is a no-op.
func Init(config Config) error {
	if config == nil {
		return nil
	}

	err := config.Init()
	if err != nil {
		return fmt.Errorf("initialize otel: %w", err)
	}

	tracePropagator, err := config.GetPropagators()
	if err != nil {
		return fmt.Errorf("get trace propagators: %w", err)
	}

	traceProvider, err := config.GetTraceProvider()
	if err != nil {
		return fmt.Errorf("get trace provider: %w", err)
	}

	logger, err := config.GetLogger()
	if err != nil {
		return fmt.Errorf("get logger: %w", err)
	}

	otel.SetTextMapPropagator(tracePropagator)
	otel.SetTracerProvider(traceProvider)
	global.SetLoggerProvider(logger)

	return nil
}
