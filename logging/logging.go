// Package logging defines the logging contracts a service wires into its HTTP
// and gRPC servers. It holds only interfaces; the presets subpackage supplies
// the concrete implementations, in a human-readable local variant and a
// structured Google Cloud variant.
package logging

import "context"

// Log is a leveled, context-aware logger. The context carries the request-scoped
// trace information an implementation attaches to each entry, and fields hold any
// extra data to record alongside the message.
type Log interface {
	// Info records a routine event.
	Info(ctx context.Context, msg string, fields ...any)
	// Warn records a problem the request recovered from.
	Warn(ctx context.Context, msg string, fields ...any)
	// Err records a failure.
	Err(ctx context.Context, msg string, fields ...any)
}
