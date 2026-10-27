package loggingpresets

import (
	"context"
	"log/slog"
	"sync"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

var _ logging.Log = (*LogOTel)(nil)

// LogOTel implements the logging.Log interface through the global OpenTelemetry logger provider,
// so entries reach the backend of the installed [otel.Config]. An entry logged under a span
// carries its trace.
//
// Its entries take the instrumentation scope given to [otel.SetAppName]. Call SetAppName before the
// first entry.
type LogOTel struct {
	// once builds logger on first use, after the application named itself.
	once   sync.Once
	logger *slog.Logger
}

func (logger *LogOTel) Info(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelInfo, msg, fields...)
}

func (logger *LogOTel) Warn(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelWarn, msg, fields...)
}

func (logger *LogOTel) Err(ctx context.Context, msg string, fields ...any) {
	logger.log(ctx, slog.LevelError, msg, fields...)
}

func (logger *LogOTel) log(ctx context.Context, level slog.Level, msg string, fields ...any) {
	logger.once.Do(func() {
		logger.logger = otel.Logger()
	})

	logger.logger.Log(ctx, level, msg, fields...)
}
