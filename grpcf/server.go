package grpcf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/downtime"
	golibproto "github.com/a-novel-kit/golib/grpcf/proto/gen"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// errShuttingDown ends the streams a server holds when its shutdown begins.
var errShuttingDown = errors.New("server shutting down")

// NewServer returns a server with the standard interceptors, outermost first: tracing, the end of
// streams at shutdown, the base context, call logs, panic recovery and the planned downtime.
//
// Pass as ctx the context [ListenAndServe] drains the server under. Once ctx is done, every stream
// ends with Unavailable, and its client resumes on another instance. A long-lived stream then cannot
// hold the drain to its deadline. Unary calls finish within the drain.
//
// baseContext derives the context of each call, so a call reaches process-wide values such as the
// database connection. A nil baseContext keeps the call's own context.
//
// The health and echo services that [RegisterEchoServers] adds still answer during a downtime.
func NewServer(
	ctx context.Context,
	tracing otel.Config,
	logger logging.RPCConfig,
	downtimeStart *time.Time,
	baseContext func(ctx context.Context) context.Context,
	extra ...grpc.ServerOption,
) *grpc.Server {
	if baseContext == nil {
		baseContext = func(ctx context.Context) context.Context { return ctx }
	}

	open := []string{
		"/" + healthpb.Health_ServiceDesc.ServiceName + "/",
		"/" + golibproto.EchoService_ServiceDesc.ServiceName + "/",
	}

	options := []grpc.ServerOption{
		tracing.RPCInterceptor(),
		grpc.ChainUnaryInterceptor(
			baseContextUnaryInterceptor(baseContext),
			logger.UnaryInterceptor(),
			logger.PanicUnaryInterceptor(),
			downtime.UnaryServerInterceptor(downtimeStart, open...),
		),
		grpc.ChainStreamInterceptor(
			endStreamsInterceptor(ctx),
			baseContextStreamInterceptor(baseContext),
			logger.StreamInterceptor(),
			logger.PanicStreamInterceptor(),
			downtime.StreamServerInterceptor(downtimeStart, open...),
		),
	}

	return grpc.NewServer(slices.Concat(options, extra)...)
}

// endStreamsInterceptor ends a stream once ctx is done, which marks the start of shutdown.
//
//nolint:contextcheck // A stream's context derives from the stream; ctx only signals its end.
func endStreamsInterceptor(ctx context.Context) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		streamCtx, cancel := context.WithCancelCause(ss.Context())
		defer cancel(nil)

		stop := context.AfterFunc(ctx, func() { cancel(errShuttingDown) })
		defer stop()

		err := handler(srv, &middleware.WrappedServerStream{ServerStream: ss, WrappedContext: streamCtx})
		if err != nil && errors.Is(context.Cause(streamCtx), errShuttingDown) {
			return status.Error(codes.Unavailable, "server shutting down: resume on another instance")
		}

		return err
	}
}

// ListenAndServe serves server on the configured port until ctx is canceled or the server stops.
// Cancellation lets active RPCs finish within the configured shutdown timeout, then stops remaining
// RPCs. A forced shutdown returns an error wrapping [context.DeadlineExceeded].
//
// [grpc.ErrServerStopped] is a normal result. A serving error that races with shutdown is joined
// with the shutdown error so callers can inspect both with [errors.Is].
//
// A command can connect process signals directly:
//
//	processCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	if err := grpcf.ListenAndServe(processCtx, cfg.Grpc, server); err != nil {
//		log.Printf("run gRPC server: %v", err)
//	}
func ListenAndServe(ctx context.Context, config ServerConfig, server *grpc.Server) error {
	address := ":" + strconv.Itoa(config.Port)
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
		err = serveError(err)
		if err != nil {
			server.Stop()
		}

		return err
	case <-ctx.Done():
	}

	shutdownErr := drain(ctx, server, config.Shutdown)

	return errors.Join(serveError(<-serveErrors), shutdownErr)
}

// drain stops server gracefully, then forcefully once shutdownTimeout passes. A forced stop returns
// an error wrapping [context.DeadlineExceeded].
func drain(ctx context.Context, server *grpc.Server, shutdownTimeout time.Duration) error {
	stopped := make(chan struct{})

	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	select {
	case <-stopped:
		return nil
	case <-shutdownCtx.Done():
	}

	// The graceful stop can finish as the timeout fires.
	select {
	case <-stopped:
		return nil
	default:
	}

	server.Stop()
	<-stopped

	return fmt.Errorf("shut down gRPC server: %w", shutdownCtx.Err())
}

// serveError wraps the error server.Serve returned, which [grpc.ErrServerStopped] ends normally.
func serveError(err error) error {
	if err == nil || errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}

	return fmt.Errorf("serve gRPC: %w", err)
}
