package logging

import "context"

// Log is a leveled, context-aware logger. The context carries the request-scoped
// trace information an implementation attaches to each entry.
//
// Each implementation renders fields to suit its destination. LogGcloud hands them to slog, where
// each becomes a structured attribute a log explorer can filter on. LogLocal formats them into msg
// as fmt.Sprintf does, and prints one line for a terminal.
//
// Neither drops data. LogLocal appends a field that has no verb to land on, and marks a verb that
// has no field in place. To log the same line under both, format it into msg and pass no fields.
type Log interface {
	// Info records a routine event.
	Info(ctx context.Context, msg string, fields ...any)
	// Warn records a problem the request recovered from.
	Warn(ctx context.Context, msg string, fields ...any)
	// Err records a failure.
	Err(ctx context.Context, msg string, fields ...any)
}
