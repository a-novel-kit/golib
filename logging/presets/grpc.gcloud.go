package loggingpresets

import (
	"io"
	"log/slog"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
)

var _ logging.RPCConfig = (*GRPCGcloud)(nil)

// GRPCGcloud implements [logging.RPCConfig] for Google Cloud, producing gRPC
// interceptors that write entries Cloud Logging parses and recover from
// handler panics. Component labels every entry with the emitting subsystem,
// and an entry logged under the RPC's span links to its trace.
type GRPCGcloud struct {
	Component string `json:"component" yaml:"component"`
	// ProjectId names the Google Cloud project that scopes trace resource names.
	ProjectId string `json:"projectID" yaml:"projectID"`
	// Out receives the entries. It defaults to standard error, where Cloud
	// Logging collects them. Set it before building an interceptor.
	Out io.Writer `json:"-" yaml:"-"`

	l *slog.Logger
}

func (logger *GRPCGcloud) UnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return grpclog.UnaryServerInterceptor(logInterceptor(logger.l))
}

func (logger *GRPCGcloud) StreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return grpclog.StreamServerInterceptor(logInterceptor(logger.l))
}

func (logger *GRPCGcloud) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

func (logger *GRPCGcloud) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

func (logger *GRPCGcloud) init() {
	if logger.l != nil {
		return
	}

	logger.l = slog.New(newGcloudHandler(logger.Out, logger.ProjectId)).
		With("service", "gRPC/server", "component", logger.Component)
}
