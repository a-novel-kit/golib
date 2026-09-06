package grpcf_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/a-novel-kit/golib/grpcf"
)

func TestRegisterEchoServers(t *testing.T) {
	t.Parallel()

	address := freeGRPCAddress(t)
	server := grpc.NewServer()
	healthcheck := grpcf.RegisterEchoServers(server)
	healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- grpcf.Serve(t.Context(), server, address, time.Second) }()

	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	client := healthpb.NewHealthClient(connection)
	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	t.Cleanup(cancel)

	watch, err := client.Watch(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)

	response, err := watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())

	time.Sleep(20 * time.Millisecond)

	response, err = client.Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())

	healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	response, err = watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, response.GetStatus())
}

func TestSetEchoServersContext(t *testing.T) {
	t.Parallel()

	address := freeGRPCAddress(t)
	server := grpc.NewServer()
	grpcf.SetEchoServersContext(t.Context(), server, -time.Second)

	listenerConfig := &net.ListenConfig{}
	listener, err := listenerConfig.Listen(t.Context(), "tcp", address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()

	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	t.Cleanup(cancel)

	client := healthpb.NewHealthClient(connection)
	response, err := client.Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())

	server.Stop()
	require.NoError(t, <-serveErrors)
}
