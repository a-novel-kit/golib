package loggingpresets_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gootel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

// recordingExporter keeps a copy of every log record it exports.
type recordingExporter struct {
	records []sdklog.Record
}

func (exporter *recordingExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}

	return nil
}

func (exporter *recordingExporter) Shutdown(context.Context) error   { return nil }
func (exporter *recordingExporter) ForceFlush(context.Context) error { return nil }

//nolint:paralleltest // Swaps the process-wide logger provider.
func TestLogOTel(t *testing.T) {
	testCases := []struct {
		name string

		write func(logger *loggingpresets.LogOTel, ctx context.Context)

		expectSeverity log.Severity
		expectBody     string
	}{
		{
			name:           "Success/Info",
			write:          func(logger *loggingpresets.LogOTel, ctx context.Context) { logger.Info(ctx, "loaded") },
			expectSeverity: log.SeverityInfo,
			expectBody:     "loaded",
		},
		{
			name:           "Success/Warn",
			write:          func(logger *loggingpresets.LogOTel, ctx context.Context) { logger.Warn(ctx, "retried") },
			expectSeverity: log.SeverityWarn,
			expectBody:     "retried",
		},
		{
			name:           "Success/Err",
			write:          func(logger *loggingpresets.LogOTel, ctx context.Context) { logger.Err(ctx, "failed") },
			expectSeverity: log.SeverityError,
			expectBody:     "failed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			exporter := &recordingExporter{}
			gootel.SetLoggerProvider(sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter))))
			t.Cleanup(func() { gootel.SetLoggerProvider(lognoop.NewLoggerProvider()) })

			tracer := sdktrace.NewTracerProvider()

			t.Cleanup(func() { require.NoError(t, tracer.Shutdown(context.WithoutCancel(t.Context()))) })

			ctx, span := tracer.Tracer("presets-test").Start(t.Context(), "request")
			defer span.End()

			testCase.write(&loggingpresets.LogOTel{}, ctx)

			// The entry reaches the global provider and links the span it was logged under.
			require.Len(t, exporter.records, 1)
			require.Equal(t, testCase.expectSeverity, exporter.records[0].Severity())
			require.Equal(t, testCase.expectBody, exporter.records[0].Body().AsString())
			require.Equal(t, span.SpanContext().TraceID(), exporter.records[0].TraceID())
		})
	}
}
