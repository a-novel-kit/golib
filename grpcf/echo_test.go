package grpcf_test

import (
	"context"
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

	address, config := freeGRPCServer(t, time.Second)
	server := grpc.NewServer()
	healthcheck := grpcf.RegisterEchoServers(server)
	healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- grpcf.ListenAndServe(t.Context(), config, server) }()

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
