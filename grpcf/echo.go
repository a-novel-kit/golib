package grpcf

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	golibproto "github.com/a-novel-kit/golib/grpcf/proto/gen"
)

type echo struct {
	golibproto.UnimplementedEchoServiceServer
}

func (handler *echo) UnaryEcho(context.Context, *golibproto.UnaryEchoRequest) (*golibproto.UnaryEchoResponse, error) {
	return &golibproto.UnaryEchoResponse{
		Message: "Hello world!",
	}, nil
}

// RegisterEchoServers registers the built-in echo and standard health services
// and returns the health controller for caller-owned lifecycle updates.
func RegisterEchoServers(server *grpc.Server) *health.Server {
	healthcheck := health.NewServer()
	healthpb.RegisterHealthServer(server, healthcheck)

	golibproto.RegisterEchoServiceServer(server, &echo{})

	return healthcheck
}

// SetEchoServersContext registers the built-in echo and standard health
// services. It preserves the legacy serving default while leaving all later
// health transitions to the caller; ctx and healthPing are retained for API
// compatibility and do not start a background loop.
func SetEchoServersContext(_ context.Context, server *grpc.Server, _ time.Duration) {
	RegisterEchoServers(server).SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
}
