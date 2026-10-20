package loggingpresets

import (
	"net/http"

	"charm.land/lipgloss/v2"
)

// LogLevel grades a log entry by severity. The Local presets render each level in its own color.
type LogLevel int

const (
	// LogLevelInfo marks a normal, expected event.
	LogLevelInfo LogLevel = iota
	// LogLevelWarn marks a recoverable problem worth attention.
	LogLevelWarn
	// LogLevelError marks a failure.
	LogLevelError
)

// statusLevel grades an HTTP response status: a server error is an error, a client error a warning.
func statusLevel(status int) LogLevel {
	switch {
	case status >= http.StatusInternalServerError:
		return LogLevelError
	case status >= http.StatusBadRequest:
		return LogLevelWarn
	default:
		return LogLevelInfo
	}
}

// style returns the terminal style the Local presets render an entry of level in.
func (level LogLevel) style() lipgloss.Style {
	switch level {
	case LogLevelWarn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ffb600"))
	case LogLevelError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff3232"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#25e6e3"))
	}
}
