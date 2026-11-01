package grpcf

import (
	"context"
	"crypto/tls"
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
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/downtime"
	golibproto "github.com/a-novel-kit/golib/grpcf/proto/gen"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
	"github.com/a-novel-kit/golib/servertls"
)

// maxConnectionAge bounds the life of each connection a server accepts. Its client then connects
// again and resolves the server's name anew. gRPC-Go resolves a name at most once every 30 seconds,
// so a shorter age finds no new instance sooner.
const maxConnectionAge = 30 * time.Second

// errShuttingDown ends the streams a server holds when its shutdown begins.
var errShuttingDown = errors.New("server shutting down")

// NewServer returns a server with the standard interceptors, outermost first: tracing, the end of
// streams at shutdown, the base context, call logs, panic recovery, the planned downtime and the
// report of server faults on the call's span, which [Fault] describes.
//
// Pass as ctx the context [ListenAndServe] drains the server under. Once ctx is done, every stream
// ends with Unavailable, and its client resumes on another instance. A long-lived stream then cannot
// hold the drain to its deadline. Unary calls finish within the drain.
//
// baseContext derives the context of each call, so a call reaches process-wide values such as the
// database connection. A nil baseContext keeps the call's own context.
//
// Each connection closes gracefully after 30 seconds, give or take a tenth: calls in progress
// finish, and the client connects again. A client that balances over every instance of a service
// then reaches instances added since. An extra [grpc.KeepaliveParams] option replaces this age.
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
			captureUnaryInterceptor,
		),
		grpc.ChainStreamInterceptor(
			endStreamsInterceptor(ctx),
			baseContextStreamInterceptor(baseContext),
			logger.StreamInterceptor(),
			logger.PanicStreamInterceptor(),
			downtime.StreamServerInterceptor(downtimeStart, open...),
			captureStreamInterceptor,
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionAge: maxConnectionAge}),
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
//
// The port serves TLS once config.TLS names a key pair, which [servertls.Config] loads and reloads
// at renewal. A config.PlaintextPort also serves plaintext beside it, on the same server.
func ListenAndServe(ctx context.Context, config ServerConfig, server *grpc.Server) error {
	listeners, err := listen(ctx, config)
	if err != nil {
		return err
	}

	serveErrors := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() {
			serveErrors <- server.Serve(listener)
		}()
	}

	select {
	case err = <-serveErrors:
		// A stopped server ends every listener, and stopping it again does nothing.
		server.Stop()

		return errors.Join(serveError(err), waitServe(serveErrors, len(listeners)-1))
	case <-ctx.Done():
	}

	shutdownErr := drain(ctx, server, config.Shutdown)

	return errors.Join(waitServe(serveErrors, len(listeners)), shutdownErr)
}

// listen opens the listeners config names: its port, over TLS once config.TLS names a key pair,
// and its plaintext port when set.
func listen(ctx context.Context, config ServerConfig) ([]net.Listener, error) {
	var tlsConfig *tls.Config

	if config.TLS != (servertls.Files{}) {
		loaded, err := servertls.Config(config.TLS)
		if err != nil {
			return nil, fmt.Errorf("serve gRPC over TLS: %w", err)
		}

		// gRPC clients require HTTP/2, which TLS negotiates through ALPN.
		loaded.NextProtos = []string{"h2"}
		tlsConfig = loaded
	}

	listener, err := listenPort(ctx, config.Port)
	if err != nil {
		return nil, err
	}

	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}

	if config.PlaintextPort == 0 {
		return []net.Listener{listener}, nil
	}

	plaintext, err := listenPort(ctx, config.PlaintextPort)
	if err != nil {
		// The port's listener never served, so its close error adds nothing.
		_ = listener.Close()

		return nil, err
	}

	return []net.Listener{listener, plaintext}, nil
}

// listenPort opens a TCP listener on port.
func listenPort(ctx context.Context, port int) (net.Listener, error) {
	address := ":" + strconv.Itoa(port)
	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for gRPC on %s: %w", address, err)
	}

	return listener, nil
}

// waitServe reads count results of server.Serve, and joins their errors.
func waitServe(serveErrors <-chan error, count int) error {
	var err error
	for range count {
		err = errors.Join(err, serveError(<-serveErrors))
	}

	return err
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
