package loggingpresets

import (
	"io"
	"log/slog"
	"sync"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"google.golang.org/grpc"

	"github.com/a-novel-kit/golib/logging"
)

var _ logging.RPCConfig = (*GRPCGcloud)(nil)

// GRPCGcloud implements [logging.RPCConfig] for Google Cloud, producing gRPC
// interceptors that write entries Cloud Logging parses and recover from
// handler panics. Each call writes one entry, when it finishes. An entry logged
// under the RPC's span links to its trace.
//
// Deprecated: use [GRPCOTel], which leaves each call's record to its RPC span.
type GRPCGcloud struct {
	// Component labels every entry with the emitting subsystem.
	Component string `json:"component" yaml:"component"`
	// ProjectID names the Google Cloud project that scopes trace resource names.
	ProjectID string `json:"projectID" yaml:"projectID"`
	// Out receives the entries. It defaults to standard error, where Cloud
	// Logging collects them. Set it before building an interceptor.
	Out io.Writer `json:"-" yaml:"-"`

	once sync.Once
	l    *slog.Logger
}

func (logger *GRPCGcloud) UnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return grpclog.UnaryServerInterceptor(logInterceptor(logger.l), logFinishedCalls)
}

func (logger *GRPCGcloud) StreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return grpclog.StreamServerInterceptor(logInterceptor(logger.l), logFinishedCalls)
}

func (logger *GRPCGcloud) PanicUnaryInterceptor() grpc.UnaryServerInterceptor {
	logger.init()

	return recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

func (logger *GRPCGcloud) PanicStreamInterceptor() grpc.StreamServerInterceptor {
	logger.init()

	return recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(panicInterceptor(logger.l)))
}

// init builds the logger every interceptor of logger shares, on the first call.
func (logger *GRPCGcloud) init() {
	logger.once.Do(func() {
		logger.l = slog.New(newGcloudHandler(logger.Out, logger.ProjectID)).
			With("service", "gRPC/server", "component", logger.Component)
	})
}
