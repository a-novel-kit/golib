package loggingpresets

import "github.com/charmbracelet/lipgloss"

var (
	LipColorError = lipgloss.Color("#e6203e")
	LipColorWarn  = lipgloss.Color("#ffb600")
	LipColorInfo  = lipgloss.Color("#25e6e3")
)

type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelWarn
	LogLevelError
)
