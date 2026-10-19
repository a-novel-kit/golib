// Package loggingpresets provides ready-made implementations of the logging
// package interfaces for the two environments a service runs in: Google Cloud
// and local development. The Gcloud presets emit structured JSON that Cloud
// Logging parses and correlates with traces; the Local presets emit
// human-readable, color-coded output for a developer's terminal. Each
// environment offers a base logger plus HTTP and gRPC middleware built on it.
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

// LogLevel is the severity of a log entry. Each preset maps it to that
// environment's native representation — a Google Cloud severity string, or a
// terminal color.
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
