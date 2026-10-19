package logging

import "context"

// Log is a leveled, context-aware logger. The context carries the request-scoped
// trace information an implementation attaches to each entry.
//
// The implementation renders fields, and the two presets differ because their destinations do.
// LogGcloud hands them to slog, where each becomes a structured attribute a log explorer can
// filter on. LogLocal renders msg as a format string with fields as its operands, which reads as a
// line in a terminal.
//
// Neither drops data. LogLocal appends a field with no verb to land on, and marks a verb with no
// field in place. A caller wanting the same line under both formats it into msg and passes no
// fields.
type Log interface {
	// Info records a routine event.
	Info(ctx context.Context, msg string, fields ...any)
	// Warn records a problem the request recovered from.
	Warn(ctx context.Context, msg string, fields ...any)
	// Err records a failure.
	Err(ctx context.Context, msg string, fields ...any)
}
