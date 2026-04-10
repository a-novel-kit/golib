package otelpresets

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"time"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	gcppropagator "github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"github.com/fatih/color"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

var _ otel.Config = (*Gcloud)(nil)

type Gcloud struct {
	ProjectID    string        `json:"projectID"    yaml:"projectID"`
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

func (config *Gcloud) Init() error {
	blue := color.New(color.FgBlue).Add(color.Bold)
	_, _ = blue.Printf("☁️ OpenTelemetry GCP Mode: exporting traces to Cloud Trace (project=%s)\n", config.ProjectID)

	return nil
}

func (config *Gcloud) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.NewCompositeTextMapPropagator(
		// Putting the CloudTraceOneWayPropagator first means the TraceContext propagator
		// takes precedence if both the traceparent and the XCTC headers exist.
		gcppropagator.CloudTraceOneWayPropagator{},
		propagation.TraceContext{},
		propagation.Baggage{},
	), nil
}

func (config *Gcloud) GetTraceProvider() (trace.TracerProvider, error) {
	var opts []texporter.Option
	if config.ProjectID != "" {
		opts = append(opts, texporter.WithProjectID(config.ProjectID))
	}

	exporter, err := texporter.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP trace exporter: %w", err)
	}

	config.tp = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
	)

	return config.tp, nil
}

// GetLogger sets up a structured JSON logger that GCP can parse and link to traces.
func (config *Gcloud) GetLogger() (log.LoggerProvider, error) {
	logExporter, err := stdoutlog.New(
		stdoutlog.WithWriter(os.Stderr),
	)
	if err != nil {
		return nil, err
	}

	config.lp = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	return config.lp, nil
}

// Flush shuts down tracer and logger providers.
func (config *Gcloud) Flush() {
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

// HttpHandler is kept for interface compatibility but doesn't wrap anything in GCP mode.
func (config *Gcloud) HttpHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}

func (config *Gcloud) RpcInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
