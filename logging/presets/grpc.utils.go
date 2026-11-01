package loggingpresets

import (
	"context"
	"log/slog"
	"runtime/debug"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/trace"

	libotel "github.com/a-novel-kit/golib/otel"
)

// logFinishedCalls logs each call once, when it finishes, with its start time and duration.
var logFinishedCalls = grpclog.WithLogOnEvents(grpclog.FinishCall)

// logInterceptor adapts l to the logger the call-logging interceptors write through.
func logInterceptor(l *slog.Logger) grpclog.Logger {
	return grpclog.LoggerFunc(func(ctx context.Context, lvl grpclog.Level, msg string, fields ...any) {
		l.Log(ctx, slog.Level(lvl), msg, fields...)
	})
}

// logTraceID adds the trace ID of a sampled call to its log fields, so a terminal line points at
// its trace.
func logTraceID(ctx context.Context) grpclog.Fields {
	if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
		return grpclog.Fields{"traceID", span.TraceID().String()}
	}

	return nil
}

// panicInterceptor answers a recovered panic with a fixed Internal status. The panic value can hold
// anything the failing code held, such as a connection string, so it goes only to the RPC span and
// the log. The span's error.type names the panic's type, and the log holds its stack.
func panicInterceptor(l *slog.Logger) recovery.RecoveryHandlerFuncContext {
	return func(ctx context.Context, p any) error {
		_ = libotel.ReportPanic(trace.SpanFromContext(ctx), p)
		l.ErrorContext(ctx, "recovered from panic", "panic", p, "stack", string(debug.Stack()))

		return status.Error(codes.Internal, "internal error")
	}
}
