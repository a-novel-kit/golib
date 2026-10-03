package loggingpresets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// LogGcloud implements the logging.Log interface for Google Cloud, writing
// entries as structured JSON with a severity field that Cloud Logging
// understands. An entry logged under a span carries the fields Cloud Logging
// reads to attach it to that span's trace.
type LogGcloud struct {
	// ProjectId names the Google Cloud project that scopes trace resource names.
	ProjectId string `json:"projectID" yaml:"projectID"`
	// Out receives the entries. It defaults to standard error, where Cloud
	// Logging collects them.
	Out io.Writer `json:"-" yaml:"-"`
}

func (logger *LogGcloud) Info(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelInfo, "INFO", msg, fields...)
}

func (logger *LogGcloud) Warn(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelWarn, "WARNING", msg, fields...)
}

func (logger *LogGcloud) Err(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelError, "ERROR", msg, fields...)
}

func (logger *LogGcloud) log(ctx context.Context, level slog.Level, severity, msg string, fields ...any) {
	out := logger.Out
	if out == nil {
		out = os.Stderr
	}

	fields = append([]any{slog.String("severity", severity)}, fields...)

	// The field names are the contract Cloud Logging reads to correlate an entry with its trace.
	// https://docs.cloud.google.com/logging/docs/structured-logging
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		fields = append(fields,
			slog.String("logging.googleapis.com/trace",
				fmt.Sprintf("projects/%s/traces/%s", logger.ProjectId, span.TraceID())),
			slog.String("logging.googleapis.com/spanId", span.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", span.IsSampled()),
		)
	}

	slog.New(slog.NewJSONHandler(out, nil)).Log(ctx, level, msg, fields...)
}
