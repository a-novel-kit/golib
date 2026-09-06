package grpcf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
)

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
