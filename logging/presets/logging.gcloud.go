package loggingpresets

import (
	"context"
	"log/slog"
	"os"
)

type LogGcloud struct {
	ProjectId string `json:"projectID" yaml:"projectID"`
	l         *slog.Logger
}

func (logger *LogGcloud) Info(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, LogLevelInfo, msg, fields...)
}

func (logger *LogGcloud) Warn(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, LogLevelWarn, msg, fields...)
}

func (logger *LogGcloud) Err(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, LogLevelError, msg, fields...)
}

func (logger *LogGcloud) log(ctx context.Context, level LogLevel, msg string, fields ...any) {
	if logger.l == nil {
		logger.l = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	}

	var (
		gcloudLevel string
		logFn       func(ctx context.Context, msg string, args ...any)
	)

	switch level {
	case LogLevelInfo:
		gcloudLevel = "INFO"
		logFn = logger.l.InfoContext
	case LogLevelWarn:
		gcloudLevel = "WARNING"
		logFn = logger.l.WarnContext
	case LogLevelError:
		gcloudLevel = "ERROR"
		logFn = logger.l.ErrorContext
	}

	fields = append([]any{slog.String("severity", gcloudLevel)}, fields...)
	logFn(ctx, msg, fields...)
}
