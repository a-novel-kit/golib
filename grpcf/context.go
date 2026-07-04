package grpcf

import (
	"context"

	"google.golang.org/grpc"
)

// BaseContextUnaryInterceptor returns a unary server interceptor that runs
// ctxInterceptor on each request's context before the handler sees it, giving
// every RPC a single place to seed request-scoped values such as a logger or a
// tracing span.
func BaseContextUnaryInterceptor(
	ctxInterceptor func(context.Context) context.Context,
) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (any, error) {
		return handler(ctxInterceptor(ctx), req)
	}
}

// wrappedStream overrides a ServerStream's Context so the transformed context
// reaches the handler; gRPC exposes no other hook to replace a stream's context.
type wrappedStream struct {
	grpc.ServerStream

	ctxInterceptor func(context.Context) context.Context
}

func (stream *wrappedStream) Context() context.Context {
	return stream.ctxInterceptor(stream.ServerStream.Context())
}

// BaseContextStreamInterceptor is the streaming counterpart to
// [BaseContextUnaryInterceptor]: it applies ctxInterceptor to the stream's
// context so every ServerStream.Context call inside the handler observes the
// transformed context.
func BaseContextStreamInterceptor(
	ctxInterceptor func(context.Context) context.Context,
) grpc.StreamServerInterceptor {
	return func(
		srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler,
	) error {
		return handler(srv, &wrappedStream{ss, ctxInterceptor})
	}
}
