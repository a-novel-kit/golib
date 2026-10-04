package loggingpresets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

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
	logger.log(ctx, slog.LevelInfo, msg, fields...)
}

func (logger *LogGcloud) Warn(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelWarn, msg, fields...)
}

func (logger *LogGcloud) Err(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelError, msg, fields...)
}

func (logger *LogGcloud) log(ctx context.Context, level slog.Level, msg string, fields ...any) {
	out := logger.Out
	if out == nil {
		out = os.Stderr
	}

	// The field names are the contract Cloud Logging reads to correlate an entry with its trace.
	// https://docs.cloud.google.com/logging/docs/agent/logging/configuration#special-fields
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		// Clip so the append copies, leaving the caller's backing array untouched.
		fields = append(slices.Clip(fields),
			slog.String("logging.googleapis.com/trace",
				fmt.Sprintf("projects/%s/traces/%s", logger.ProjectId, span.TraceID())),
			slog.String("logging.googleapis.com/spanId", span.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", span.IsSampled()),
		)
	}

	slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: gcloudAttr})).
		Log(ctx, level, msg, fields...)
}

// gcloudAttr renames slog's built-in keys to the ones Cloud Logging reads: the message becomes the
// entry's display text, and the level its severity.
func gcloudAttr(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attr
	}

	switch attr.Key {
	case slog.MessageKey:
		attr.Key = "message"
	case slog.LevelKey:
		level, _ := attr.Value.Any().(slog.Level)
		attr = slog.String("severity", gcloudSeverity(level))
	}

	return attr
}

// gcloudSeverity maps a slog level to the Cloud Logging severity at or below it.
func gcloudSeverity(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARNING"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
