package logging

import "context"

type Log interface {
	Info(ctx context.Context, msg string, fields ...any)
	Warn(ctx context.Context, msg string, fields ...any)
	Err(ctx context.Context, msg string, fields ...any)
}
