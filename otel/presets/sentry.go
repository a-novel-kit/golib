package otelpresets

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	sentryotel "github.com/getsentry/sentry-go/otel"
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

type Sentry struct {
	DSN          string        `json:"dsn"          yaml:"dsn"`
	ServerName   string        `json:"serverName"   yaml:"serverName"`
	Release      string        `json:"release"      yaml:"release"`
	Environment  string        `json:"environment"  yaml:"environment"`
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`
	Debug        bool          `json:"debug"        yaml:"debug"`

	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

func (config *Sentry) Init() error {
	return sentry.Init(sentry.ClientOptions{
		Dsn:              config.DSN,
		EnableTracing:    true,
		EnableLogs:       true,
		TracesSampleRate: 1.0,
		Debug:            config.Debug,
		DebugWriter:      os.Stderr,
		ServerName:       config.ServerName,
		Release:          config.Release,
		Environment:      config.Environment,
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			return append(integrations, sentryotel.NewOtelIntegration())
		},
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if hint == nil || hint.Context == nil {
				return event
			}

			if req, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
				// Add IP Address to user information.
				event.User.IPAddress = req.RemoteAddr
			}

			return event
		},
	})
}

func (config *Sentry) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	), nil
}

func (config *Sentry) GetTraceProvider() (trace.TracerProvider, error) {
	//nolint:staticcheck // sentryotlp.NewTraceExporter not yet available; migrate when sentry-go/otel/otlp is released
	config.tp = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sentryotel.NewSentrySpanProcessor()))

	return config.tp, nil
}

func (config *Sentry) GetLogger() (log.LoggerProvider, error) {
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
	ctx := context.Background()

	if config.FlushTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, config.FlushTimeout)
		defer cancel()
	}

	if config.tp != nil {
		_ = config.tp.Shutdown(ctx)
	}

	if config.lp != nil {
		_ = config.lp.Shutdown(ctx)
	}

	sentry.Flush(config.FlushTimeout)
}

func (config *Sentry) HttpHandler() func(http.Handler) http.Handler {
	sentryHandler := sentryhttp.New(sentryhttp.Options{})

	return sentryHandler.Handle
}

func (config *Sentry) RpcInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
