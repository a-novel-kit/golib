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
	lstyle := lipgloss.NewStyle()

	switch level {
	case LogLevelInfo:
		lstyle = lstyle.Foreground(LipColorInfo)
	case LogLevelWarn:
		lstyle = lstyle.Foreground(LipColorWarn)
	case LogLevelError:
		lstyle = lstyle.Foreground(LipColorError)
	}

	// With no operands there is nothing to format, and msg goes out as written. Running it
	// through Sprintf would rewrite any % it contains — an error text reading "50% done"
	// becomes "50%!(NOVERB)", losing the detail at the point the log exists to carry it.
	// httpf.HandleError reaches here with the error text and no fields.
	rendered := msg
	if len(fields) > 0 {
		rendered = fmt.Sprintf(msg, fields...)
	}

	_, _ = lipgloss.Fprint(logger.Out, lstyle.Render(rendered)+"\n")
}
