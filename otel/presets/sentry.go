package otelpresets

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	sentryotel "github.com/getsentry/sentry-go/otel"
	sentryotlp "github.com/getsentry/sentry-go/otel/otlp"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Sentry)(nil)

// Sentry is a Config that reports traces to Sentry over OTLP and forwards captured
// errors through the Sentry SDK.
type Sentry struct {
	// DSN is the Sentry project's ingest URL, which selects the project events are
	// sent to.
	DSN string `json:"dsn" yaml:"dsn"`
	// ServerName names the host in every event.
	ServerName string `json:"serverName" yaml:"serverName"`
	// Release names the deployed version in every event.
	Release string `json:"release" yaml:"release"`
	// Environment names the deployment environment in every event.
	Environment string `json:"environment" yaml:"environment"`
	// FlushTimeout bounds how long Flush waits for buffered data to drain. Zero waits
	// indefinitely for spans and logs, and gives the events of the Sentry SDK no time to send.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`
	// Debug makes the Sentry SDK log its own activity to standard error.
	Debug bool `json:"debug" yaml:"debug"`

	// The getters build tp and lp on their first call and return them on later calls, so Flush
	// shuts down the providers in use.
	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

func (config *Sentry) Init() error {
	options := sentry.ClientOptions{
		Dsn:              config.DSN,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Debug:            config.Debug,
		DebugWriter:      os.Stderr,
		ServerName:       config.ServerName,
		Release:          config.Release,
		Environment:      config.Environment,
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			return append(integrations, sentryotel.NewOtelIntegration())
		},
		BeforeSend: sentryEventIP,
	}

	return sentry.Init(options)
}

func (config *Sentry) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	), nil
}

func (config *Sentry) GetTraceProvider() (trace.TracerProvider, error) {
	if config.tp != nil {
		return config.tp, nil
	}

	// Spans reach Sentry through an OTLP exporter. The batch processor buffers them
	// and is drained by Flush -> tp.Shutdown.
	exporter, err := sentryotlp.NewTraceExporter(context.Background(), config.DSN)
	if err != nil {
		return nil, err
	}

	config.tp = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))

	return config.tp, nil
}

func (config *Sentry) GetLogger() (log.LoggerProvider, error) {
	if config.lp != nil {
		return config.lp, nil
	}

	// TODO: switch to Sentry native logger for production use.
	logExporter, err := stdoutlog.New()
	if err != nil {
		return nil, err
	}

	config.lp = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	return config.lp, nil
}

func (config *Sentry) Flush() {
	shutdownProviders(config.FlushTimeout, config.tp, config.lp)
	sentry.Flush(config.FlushTimeout)
}

func (config *Sentry) HTTPHandler() func(http.Handler) http.Handler {
	sentryHandler := sentryhttp.New(sentryhttp.Options{})

	return sentryHandler.Handle
}

func (config *Sentry) RPCInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// sentryEventIP sets the user IP of an event captured during a request to that request's peer
// address.
func sentryEventIP(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	if hint == nil || hint.Context == nil {
		return event
	}

	if req, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
		event.User.IPAddress = req.RemoteAddr
	}

	return event
}
