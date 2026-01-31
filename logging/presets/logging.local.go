package loggingpresets

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/charmbracelet/lipgloss"
)

type LogLocal struct {
	Out      io.Writer
	Renderer *lipgloss.Renderer
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
	if logger.Renderer == nil {
		logger.Renderer = lipgloss.DefaultRenderer()
	}

	if logger.Out == nil {
		log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

		logger.Out = os.Stdout
	}

	lstyle := logger.Renderer.NewStyle()

	switch level {
	case LogLevelInfo:
		lstyle = lstyle.Foreground(LipColorInfo)
	case LogLevelWarn:
		lstyle = lstyle.Foreground(LipColorWarn)
	case LogLevelError:
		lstyle = lstyle.Foreground(LipColorError)
	}

	_, _ = fmt.Fprint(logger.Out, lstyle.Render(fmt.Sprintf(msg, fields...))+"\n")
}
