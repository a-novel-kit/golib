package loggingpresets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

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
	// Logging collects them. Set it before the first entry.
	Out io.Writer `json:"-" yaml:"-"`

	// once builds logger on first use; its handler serializes writes to Out.
	once   sync.Once
	logger *slog.Logger
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
	logger.once.Do(func() {
		logger.logger = slog.New(newGcloudHandler(logger.Out, logger.ProjectId))
	})

	logger.logger.Log(ctx, level, msg, fields...)
}

// gcloudHandler writes entries as the JSON Cloud Logging parses. An entry logged under a span
// carries the fields Cloud Logging reads to attach it to that span's trace.
type gcloudHandler struct {
	slog.Handler

	projectID string
}

// newGcloudHandler returns a gcloudHandler writing to out, or to standard error when out is nil.
func newGcloudHandler(out io.Writer, projectID string) *gcloudHandler {
	if out == nil {
		out = os.Stderr
	}

	return &gcloudHandler{
		Handler:   slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: gcloudAttr}),
		projectID: projectID,
	}
}

func (handler *gcloudHandler) Handle(ctx context.Context, record slog.Record) error {
	// The field names are the contract Cloud Logging reads to correlate an entry with its trace.
	// https://docs.cloud.google.com/logging/docs/agent/logging/configuration#special-fields
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record.AddAttrs(
			slog.String("logging.googleapis.com/trace",
				fmt.Sprintf("projects/%s/traces/%s", handler.projectID, span.TraceID())),
			slog.String("logging.googleapis.com/spanId", span.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", span.IsSampled()),
		)
	}

	return handler.Handler.Handle(ctx, record)
}

func (handler *gcloudHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &gcloudHandler{Handler: handler.Handler.WithAttrs(attrs), projectID: handler.projectID}
}

func (handler *gcloudHandler) WithGroup(name string) slog.Handler {
	return &gcloudHandler{Handler: handler.Handler.WithGroup(name), projectID: handler.projectID}
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
