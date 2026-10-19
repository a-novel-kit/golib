package loggingpresets

import (
	"net/http"

	"charm.land/lipgloss/v2"
)

// Terminal colors the Local presets use to distinguish log severities.
var (
	LipColorError = lipgloss.Color("#ff3232")
	LipColorWarn  = lipgloss.Color("#ffb600")
	LipColorInfo  = lipgloss.Color("#25e6e3")
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
		return lipgloss.NewStyle().Foreground(LipColorWarn)
	case LogLevelError:
		return lipgloss.NewStyle().Foreground(LipColorError)
	default:
		return lipgloss.NewStyle().Foreground(LipColorInfo)
	}
}
