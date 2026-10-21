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
