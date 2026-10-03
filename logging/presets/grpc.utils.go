package loggingpresets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/trace"

	libotel "github.com/a-novel-kit/golib/otel"
)

// errRecoveredPanic wraps the panic value recorded on the RPC span.
var errRecoveredPanic = errors.New("recovered panic")

func logInterceptor(l *slog.Logger) grpclog.Logger {
	return grpclog.LoggerFunc(func(ctx context.Context, lvl grpclog.Level, msg string, fields ...any) {
		l.Log(ctx, slog.Level(lvl), msg, fields...)
	})
}

func logTraceId(ctx context.Context) grpclog.Fields {
	if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
		return grpclog.Fields{"traceID", span.TraceID().String()}
	}

	return nil
}

// panicInterceptor answers a recovered panic with a fixed Internal status. The panic value can hold
// anything the failing code held, such as a connection string, so it goes only to the RPC span and
// the log.
func panicInterceptor(l *slog.Logger) recovery.RecoveryHandlerFuncContext {
	return func(ctx context.Context, p any) error {
		_ = libotel.ReportError(trace.SpanFromContext(ctx), fmt.Errorf("%w: %v", errRecoveredPanic, p))
		l.ErrorContext(ctx, "recovered from panic", "panic", p, "stack", string(debug.Stack()))

		return status.Error(codes.Internal, "internal error")
	}
}
