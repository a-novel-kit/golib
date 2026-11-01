package loggingpresets

import (
	"context"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

var _ logging.RPCConfig = (*GRPCOTel)(nil)

// GRPCOTel implements [logging.RPCConfig] for a server whose RPC spans record each call, so it logs
// no call. Its recovery turns a handler panic into an internal error, reports the panic on the RPC
// span and logs it through the global OpenTelemetry logger provider.
type GRPCOTel struct{}

// UnaryInterceptor passes each unary call through: its RPC span is its record.
func (logger *GRPCOTel) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, req)
	}
}

// StreamInterceptor passes each streaming call through: its RPC span is its record.
func (logger *GRPCOTel) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, ss)
	}
}

func (logger *GRPCOTel) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(otel.Logger())))
}

func (logger *GRPCOTel) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(otel.Logger())))
}
