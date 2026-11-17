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

func TestRegisterHealth(t *testing.T) {
	t.Parallel()

	// drain stands for the process context, done once the server starts to drain.
	drain, startDrain := context.WithCancel(t.Context())

	address, config := freeGRPCServer(t, time.Second)
	server := grpc.NewServer()
	grpcf.RegisterHealth(drain, server)

	go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	t.Cleanup(cancel)

	watch, err := healthpb.NewHealthClient(connection).Watch(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)

	response, err := watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, response.GetStatus())

	startDrain()

	response, err = watch.Recv()
	require.NoError(t, err)
	require.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, response.GetStatus())
}
