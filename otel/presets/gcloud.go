package otelpresets

import (
	"context"
	"crypto/tls"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"time"

	"charm.land/lipgloss/v2"
	"google.golang.org/grpc"
	grpccredentials "google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"

	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

const (
	gcloudProjectIDAttribute = "gcp.project_id"
	gcloudTelemetryHost      = "telemetry.googleapis.com"
	gcloudTelemetryEndpoint  = gcloudTelemetryHost + ":443"
)

var _ otel.Config = (*Gcloud)(nil)

// Gcloud is a Config that exports traces and logs to Google Cloud over
// authenticated OTLP. Cloud Trace and Cloud Logging read the trace context and
// severity from the OTLP records.
type Gcloud struct {
	// ProjectID is the Google Cloud project telemetry is sent to. When empty, the
	// project is detected from Google Application Default Credentials or the
	// runtime environment.
	ProjectID string `json:"projectID" yaml:"projectID"`
	// FlushTimeout bounds how long Flush waits for buffered data to drain; zero
	// waits indefinitely.
	FlushTimeout time.Duration `json:"flushTimeout" yaml:"flushTimeout"`

	tp *sdktrace.TracerProvider
	lp *sdklog.LoggerProvider
}

func (config *Gcloud) Init() error {
	banner := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	_, _ = fmt.Fprintln(os.Stdout, banner.Render(fmt.Sprintf(
		"☁️ OpenTelemetry GCP Mode: exporting traces and logs over OTLP (project=%s)", config.ProjectID,
	)))

	return nil
}

func (config *Gcloud) GetPropagators() (propagation.TextMapPropagator, error) {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	), nil
}

func (config *Gcloud) GetTraceProvider() (trace.TracerProvider, error) {
	ctx := context.Background()

	telemetryResource, telemetryCredentials, err := config.telemetryTarget(ctx)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(gcloudTelemetryEndpoint),
		otlptracegrpc.WithTLSCredentials(gcloudTLS()),
		otlptracegrpc.WithDialOption(grpc.WithPerRPCCredentials(telemetryCredentials)),
	)
	if err != nil {
		return nil, fmt.Errorf("create GCP trace exporter: %w", err)
	}

	config.tp = sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(telemetryResource),
	)

	return config.tp, nil
}

// GetLogger returns a logger provider that exports logs over OTLP, where Cloud Logging stores each
// record with its severity and links it to its trace.
func (config *Gcloud) GetLogger() (log.LoggerProvider, error) {
	ctx := context.Background()

	telemetryResource, telemetryCredentials, err := config.telemetryTarget(ctx)
	if err != nil {
		return nil, err
	}

	exporter, err := otlploggrpc.New(
		ctx,
		otlploggrpc.WithEndpoint(gcloudTelemetryEndpoint),
		otlploggrpc.WithTLSCredentials(gcloudTLS()),
		otlploggrpc.WithDialOption(grpc.WithPerRPCCredentials(telemetryCredentials)),
	)
	if err != nil {
		return nil, fmt.Errorf("create GCP log exporter: %w", err)
	}

	config.lp = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(telemetryResource),
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

func (config *Gcloud) HttpHandler() func(http.Handler) http.Handler {
	return otelhttp.NewMiddleware("")
}

func (config *Gcloud) RpcInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// telemetryTarget returns the resource every record is exported under, and the credentials that
// authenticate the export.
func (config *Gcloud) telemetryTarget(
	ctx context.Context,
) (*resource.Resource, grpccredentials.PerRPCCredentials, error) {
	telemetryCredentials, err := oauth.NewApplicationDefault(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load Google application default credentials: %w", err)
	}

	gcpResourceOptions := []resource.Option{
		resource.WithDetectors(gcp.NewDetector()),
	}
	if config.ProjectID != "" {
		gcpResourceOptions = append(
			gcpResourceOptions,
			resource.WithAttributes(attribute.String(gcloudProjectIDAttribute, config.ProjectID)),
		)
	}

	gcpResource, err := resource.New(ctx, gcpResourceOptions...)
	if err != nil {
		return nil, nil, fmt.Errorf("detect GCP telemetry resource: %w", err)
	}

	telemetryResource, err := resource.Merge(resource.DefaultWithContext(ctx), gcpResource)
	if err != nil {
		return nil, nil, fmt.Errorf("merge GCP telemetry resource: %w", err)
	}

	return telemetryResource, telemetryCredentials, nil
}

// gcloudTLS secures the connection to the Telemetry API.
func gcloudTLS() grpccredentials.TransportCredentials {
	return grpccredentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: gcloudTelemetryHost,
	})
}
