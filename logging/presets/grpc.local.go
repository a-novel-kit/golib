package loggingpresets

import (
	"log/slog"

	"github.com/fatih/color"
	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
)

var _ logging.RPCConfig = (*GRPCLocal)(nil)

type GRPCLocal struct {
	Component string `json:"component" yaml:"component"`

	l *slog.Logger
}

func (logger *GRPCLocal) UnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return grpclog.UnaryServerInterceptor(logInterceptor(logger.l), grpclog.WithFieldsFromContext(logTraceId))
}

func (logger *GRPCLocal) StreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return grpclog.StreamServerInterceptor(logInterceptor(logger.l), grpclog.WithFieldsFromContext(logTraceId))
}

func (logger *GRPCLocal) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandler(panicInterceptor(logger.l)))
}

func (logger *GRPCLocal) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandler(panicInterceptor(logger.l)))
}

func (logger *GRPCLocal) init() {
	if logger.l != nil {
		return
	}

	log := slog.New(slog.NewTextHandler(color.Output, &slog.HandlerOptions{}))
	logger.l = log.With("service", "gRPC/server", "component", logger.Component)
}

// GrpcLocal is the legacy spelling of GRPCLocal.
//
// Deprecated: use GRPCLocal. The renamed alias matches the project's
// acronym-casing convention (`gRPC`/`GRPC`, not `Grpc`); behaviour is
// unchanged.
type GrpcLocal = GRPCLocal
