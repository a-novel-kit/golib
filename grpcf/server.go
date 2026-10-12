package grpcf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

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
// Its streams end when ctx is done, the context [Serve] drains the server under: a long-lived stream
// would otherwise hold the drain to its deadline. Their clients receive Unavailable and resume on
// another instance, while unary calls finish within the drain. baseContext derives each call's
// context, so calls reach process-wide values such as the database connection; nil keeps the call's
// own. The health and echo services that [RegisterEchoServers] adds still answer during a downtime.
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

	return grpc.NewServer(append([]grpc.ServerOption{
		tracing.RpcInterceptor(),
		grpc.ChainUnaryInterceptor(
			BaseContextUnaryInterceptor(baseContext),
			logger.UnaryInterceptor(),
			logger.PanicUnaryInterceptor(),
			downtime.UnaryServerInterceptor(downtimeStart, open...),
		),
		grpc.ChainStreamInterceptor(
			endStreamsInterceptor(ctx),
			BaseContextStreamInterceptor(baseContext),
			logger.StreamInterceptor(),
			logger.PanicStreamInterceptor(),
			downtime.StreamServerInterceptor(downtimeStart, open...),
		),
	}, extra...)...)
}

// endStreamsInterceptor ends a stream once shutdown, the end of ctx, begins.
//
//nolint:contextcheck // A stream's context derives from the stream; ctx only signals its end.
func endStreamsInterceptor(ctx context.Context) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		streamCtx, cancel := context.WithCancelCause(ss.Context())
		defer cancel(nil)

		stop := context.AfterFunc(ctx, func() { cancel(errShuttingDown) })
		defer stop()

		err := handler(srv, &wrappedStream{ss, func(context.Context) context.Context { return streamCtx }})
		if err != nil && errors.Is(context.Cause(streamCtx), errShuttingDown) {
			return status.Error(codes.Unavailable, "server shutting down: resume on another instance")
		}

		return err
	}
}

// ListenAndServe serves server on the configured port until ctx is canceled, then drains active
// calls within the configured shutdown timeout. See [Serve].
func ListenAndServe(ctx context.Context, config ServerConfig, server *grpc.Server) error {
	return Serve(ctx, server, ":"+strconv.Itoa(config.Port), config.Shutdown)
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
