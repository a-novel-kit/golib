package grpcf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/a-novel-kit/golib/downtime"
	golibproto "github.com/a-novel-kit/golib/grpcf/proto/gen"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// ServerOptions holds what a server needs beyond its [ServerConfig].
type ServerOptions struct {
	// Otel traces every call.
	Otel otel.Config
	// Logger logs every call and recovers its panics.
	Logger logging.RPCConfig
	// DowntimeStart is when a planned downtime starts; nil when none is planned.
	DowntimeStart *time.Time
	// BaseContext derives each call's context, so calls reach process-wide values such as the
	// database connection. Nil keeps the call's own context.
	BaseContext func(ctx context.Context) context.Context
}

// NewServer returns a server with the standard interceptors, outermost first: tracing, the base
// context, call logs, panic recovery and the planned downtime. The health and echo services that
// [RegisterEchoServers] adds still answer during a downtime.
func NewServer(options ServerOptions, extra ...grpc.ServerOption) *grpc.Server {
	baseContext := options.BaseContext
	if baseContext == nil {
		baseContext = func(ctx context.Context) context.Context { return ctx }
	}

	open := []string{
		"/" + healthpb.Health_ServiceDesc.ServiceName + "/",
		"/" + golibproto.EchoService_ServiceDesc.ServiceName + "/",
	}

	return grpc.NewServer(append([]grpc.ServerOption{
		options.Otel.RpcInterceptor(),
		grpc.ChainUnaryInterceptor(
			BaseContextUnaryInterceptor(baseContext),
			options.Logger.UnaryInterceptor(),
			options.Logger.PanicUnaryInterceptor(),
			downtime.UnaryServerInterceptor(options.DowntimeStart, open...),
		),
		grpc.ChainStreamInterceptor(
			BaseContextStreamInterceptor(baseContext),
			options.Logger.StreamInterceptor(),
			options.Logger.PanicStreamInterceptor(),
			downtime.StreamServerInterceptor(options.DowntimeStart, open...),
		),
	}, extra...)...)
}

// Serve listens on address until ctx is canceled or the server stops. Cancellation lets active
// RPCs finish within shutdownTimeout, then stops remaining RPCs. A forced shutdown returns an
// error wrapping [context.DeadlineExceeded].
//
// [grpc.ErrServerStopped] is a normal result. A serving error that races with shutdown is joined
// with the shutdown error so callers can inspect both with [errors.Is].
//
// A command can connect process signals directly:
//
//	processCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	if err := grpcf.Serve(processCtx, server, ":8080", 30*time.Second); err != nil {
//		log.Printf("run gRPC server: %v", err)
//	}
func Serve(ctx context.Context, server *grpc.Server, address string, shutdownTimeout time.Duration) error {
	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen for gRPC on %s: %w", address, err)
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	select {
	case err = <-serveErrors:
		if err == nil || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}

		server.Stop()

		return fmt.Errorf("serve gRPC: %w", err)
	case <-ctx.Done():
	}

	gracefulStopDone := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(gracefulStopDone)
	}()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	var shutdownErr error

	select {
	case <-gracefulStopDone:
	case <-shutdownCtx.Done():
		select {
		case <-gracefulStopDone:
		default:
			shutdownErr = fmt.Errorf("shut down gRPC server: %w", shutdownCtx.Err())

			server.Stop()
			<-gracefulStopDone
		}
	}

	serveErr := <-serveErrors
	if errors.Is(serveErr, grpc.ErrServerStopped) {
		serveErr = nil
	}

	if serveErr != nil {
		serveErr = fmt.Errorf("serve gRPC: %w", serveErr)
	}

	return errors.Join(serveErr, shutdownErr)
}
