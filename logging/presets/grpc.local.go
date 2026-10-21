package loggingpresets

import (
	"log/slog"
	"os"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
)

var _ logging.RPCConfig = (*GRPCLocal)(nil)

// GRPCLocal implements [logging.RPCConfig] for local development, producing
// gRPC interceptors that emit human-readable text to stdout and recover from
// handler panics. Each call writes one line, when it finishes.
type GRPCLocal struct {
	// Component labels every entry with the emitting subsystem.
	Component string `json:"component" yaml:"component"`

	l *slog.Logger
}

func (logger *GRPCLocal) UnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return grpclog.UnaryServerInterceptor(
		logInterceptor(logger.l), logFinishedCalls, grpclog.WithFieldsFromContext(logTraceID),
	)
}

func (logger *GRPCLocal) StreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return grpclog.StreamServerInterceptor(
		logInterceptor(logger.l), logFinishedCalls, grpclog.WithFieldsFromContext(logTraceID),
	)
}

func (logger *GRPCLocal) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

func (logger *GRPCLocal) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

// init builds the logger every interceptor of logger shares, on the first call.
func (logger *GRPCLocal) init() {
	if logger.l != nil {
		return
	}

	logger.l = slog.New(slog.NewTextHandler(os.Stdout, nil)).
		With("service", "gRPC/server", "component", logger.Component)
}
