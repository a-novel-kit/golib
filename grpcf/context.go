package grpcf

import (
	"context"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"google.golang.org/grpc"
)

// baseContextUnaryInterceptor runs ctxInterceptor on the context of each unary call before the
// handler sees it. Every call then gets one place to seed request-scoped values, such as a logger.
func baseContextUnaryInterceptor(
	ctxInterceptor func(context.Context) context.Context,
) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		return handler(ctxInterceptor(ctx), req)
	}
}

// baseContextStreamInterceptor is the streaming counterpart of [baseContextUnaryInterceptor]. It
// derives the stream's context once, and every Context call inside the handler returns it.
func baseContextStreamInterceptor(
	ctxInterceptor func(context.Context) context.Context,
) grpc.StreamServerInterceptor {
	return func(
		srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler,
	) error {
		stream := &middleware.WrappedServerStream{ServerStream: ss, WrappedContext: ctxInterceptor(ss.Context())}

		return handler(srv, stream)
	}
}

// valuesOf is the context of a call that also reaches the values of base, such as the process
// context the server serves under. The call keeps its own deadline and cancellation.
type valuesOf struct {
	context.Context //nolint:containedctx // The type is a context, merging the values of two.

	base context.Context //nolint:containedctx // The type is a context, merging the values of two.
}

// Value returns the value the call carries for key, or else the one base carries.
func (ctx valuesOf) Value(key any) any {
	value := ctx.Context.Value(key)
	if value != nil {
		return value
	}

	return ctx.base.Value(key)
}

// withValuesOf returns callCtx, reaching the values of base for a key callCtx lacks.
func withValuesOf(callCtx, base context.Context) context.Context {
	return valuesOf{Context: callCtx, base: base}
}
