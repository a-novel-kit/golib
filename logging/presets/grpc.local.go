package loggingpresets

import (
	"log/slog"
	"os"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
)

var _ logging.RpcConfig = (*GrpcLocal)(nil)

type GrpcLocal struct {
	Component string `json:"component" yaml:"component"`

	l *slog.Logger
}

func (logger *GrpcLocal) UnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return grpclog.UnaryServerInterceptor(logInterceptor(logger.l), grpclog.WithFieldsFromContext(logTraceId))
}

func (logger *GrpcLocal) StreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return grpclog.StreamServerInterceptor(logInterceptor(logger.l), grpclog.WithFieldsFromContext(logTraceId))
}

func (logger *GrpcLocal) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandler(panicInterceptor(logger.l)))
}

func (logger *GrpcLocal) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandler(panicInterceptor(logger.l)))
}

func (logger *GrpcLocal) init() {
	if logger.l != nil {
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
	logger.l = log.With("service", "gRPC/server", "component", logger.Component)
}
