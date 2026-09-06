package grpcf_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/a-novel-kit/golib/grpcf"
)

const grpcServerTestTimeout = 5 * time.Second

func TestServe(t *testing.T) {
	t.Parallel()

	t.Run("Success/ProcessCancellation", func(t *testing.T) {
		t.Parallel()

		address := freeGRPCAddress(t)
		ctx, cancel := context.WithCancel(t.Context())

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- grpcf.Serve(ctx, grpc.NewServer(), address, time.Second) }()

		waitForGRPCServer(t, address)
		cancel()

		select {
		case err := <-serveErrors:
			require.NoError(t, err)
		case <-time.After(grpcServerTestTimeout):
			require.FailNow(t, "gRPC server did not stop after process cancellation")
		}
	})

	t.Run("Error/BindFailure", func(t *testing.T) {
		t.Parallel()

		listenerConfig := &net.ListenConfig{}
		listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, listener.Close()) })

		err = grpcf.Serve(t.Context(), grpc.NewServer(), listener.Addr().String(), time.Second)
		require.Error(t, err)
		require.Contains(t, err.Error(), "listen for gRPC")
	})

	t.Run("Error/ForcedShutdown", func(t *testing.T) {
		t.Parallel()

		address := freeGRPCAddress(t)
		ctx, cancel := context.WithCancel(t.Context())
		server := grpc.NewServer()
		healthpb.RegisterHealthServer(server, health.NewServer())

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- grpcf.Serve(ctx, server, address, 50*time.Millisecond) }()

		connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		watchCtx, stopWatch := context.WithTimeout(t.Context(), grpcServerTestTimeout)
		t.Cleanup(stopWatch)

		watch, err := healthpb.NewHealthClient(connection).Watch(
			watchCtx,
			&healthpb.HealthCheckRequest{},
			grpc.WaitForReady(true),
		)
		require.NoError(t, err)

		_, err = watch.Recv()
		require.NoError(t, err)

		cancel()

		select {
		case err = <-serveErrors:
			require.ErrorIs(t, err, context.DeadlineExceeded)
		case <-time.After(grpcServerTestTimeout):
			require.FailNow(t, "gRPC server did not stop within its shutdown budget")
		}

		_, err = watch.Recv()
		require.Error(t, err)
	})
}

func freeGRPCAddress(t *testing.T) string {
	t.Helper()

	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	address := listener.Addr().String()

	err = listener.Close()
	if err != nil {
		panic(err)
	}

	return address
}

func waitForGRPCServer(t *testing.T, address string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	defer cancel()

	dialer := &net.Dialer{}

	for {
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			err = connection.Close()
			if err != nil {
				panic(err)
			}

			return
		}

		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
