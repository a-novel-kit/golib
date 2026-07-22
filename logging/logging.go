// Package logging defines the logging contracts a service wires into its HTTP
// and gRPC servers. It holds only interfaces; the presets subpackage supplies
// the concrete implementations, in a human-readable local variant and a
// structured Google Cloud variant.
package logging

import "context"

// Log is a leveled, context-aware logger. The context carries the request-scoped
// trace information an implementation attaches to each entry.
//
// How fields are rendered belongs to the implementation, and the two presets differ
// because their destinations do. LogGcloud hands them to slog, where each becomes a
// structured attribute a log explorer can filter on. LogLocal renders msg as a format
// string with fields as its operands, which is what reads as a line in a terminal.
//
// A call that passes fields therefore renders differently under each. Neither drops
// data — a field with no verb to land on is appended, a verb with no field is marked
// in place — but a caller wanting one exact line should format it into msg and pass no
// fields. With no fields the two agree.
type Log interface {
	// Info records a routine event.
	Info(ctx context.Context, msg string, fields ...any)
	// Warn records a problem the request recovered from.
	Warn(ctx context.Context, msg string, fields ...any)
	// Err records a failure.
	Err(ctx context.Context, msg string, fields ...any)
}
