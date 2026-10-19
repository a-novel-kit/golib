package loggingpresets

import (
	"context"
	"fmt"
	"io"

	"charm.land/lipgloss/v2"
)

// LogLocal implements the logging.Log interface for local development,
// rendering each entry in a severity color to Out. It carries no trace
// context, since the output is meant to be read directly in a terminal.
type LogLocal struct {
	Out io.Writer
}

func (logger *LogLocal) Info(_ context.Context, msg string, fields ...any) {
	logger.log(LogLevelInfo, msg, fields...)
}

func (logger *LogLocal) Warn(_ context.Context, msg string, fields ...any) {
	logger.log(LogLevelWarn, msg, fields...)
}

func (logger *LogLocal) Err(_ context.Context, msg string, fields ...any) {
	logger.log(LogLevelError, msg, fields...)
}

func (logger *LogLocal) log(level LogLevel, msg string, fields ...any) {
	// Without fields, msg goes out as written: Sprintf rewrites the lone % of an error text such
	// as "50% done", which httpf.HandleError logs with no fields.
	rendered := msg
	if len(fields) > 0 {
		rendered = fmt.Sprintf(msg, fields...)
	}

	_, _ = lipgloss.Fprint(logger.Out, level.style().Render(rendered)+"\n")
}
