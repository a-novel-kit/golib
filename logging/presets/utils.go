package loggingpresets

import (
	"net/http"
)

// The ANSI SGR sequences the Local presets paint their lines with. A terminal reads them as colors; a
// file or a pipe keeps them as text.
const (
	ansiReset = "\x1b[0m"
	ansiFaint = "\x1b[2m"
	// ansiInfo, ansiWarn and ansiError are the 24-bit colors #25e6e3, #ffb600 and #ff3232.
	ansiInfo  = "\x1b[38;2;37;230;227m"
	ansiWarn  = "\x1b[38;2;255;182;0m"
	ansiError = "\x1b[38;2;255;50;50m"
)

// logLevel grades a log entry by severity. The Local presets paint each level in its own color.
type logLevel int

const (
	// levelInfo marks a normal, expected event.
	levelInfo logLevel = iota
	// levelWarn marks a recoverable problem worth attention.
	levelWarn
	// levelError marks a failure.
	levelError
)

// statusLevel grades an HTTP response status: a server error is an error, a client error a warning.
func statusLevel(status int) logLevel {
	switch {
	case status >= http.StatusInternalServerError:
		return levelError
	case status >= http.StatusBadRequest:
		return levelWarn
	default:
		return levelInfo
	}
}

// paint returns text in the color of level.
func (level logLevel) paint(text string) string {
	switch level {
	case levelWarn:
		return paint(ansiWarn, text)
	case levelError:
		return paint(ansiError, text)
	default:
		return paint(ansiInfo, text)
	}
}

// paint wraps text in the SGR sequence style, then resets the terminal.
func paint(style, text string) string {
	return style + text + ansiReset
}
