package grpcf

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	golibproto "github.com/a-novel-kit/golib/grpcf/proto/gen"
)

// echo implements UnaryEcho. The streaming RPCs of EchoService stay unimplemented.
type echo struct {
	golibproto.UnimplementedEchoServiceServer
}

// UnaryEcho answers every call with the same greeting, whatever the request.
func (handler *echo) UnaryEcho(context.Context, *golibproto.UnaryEchoRequest) (*golibproto.UnaryEchoResponse, error) {
	return &golibproto.UnaryEchoResponse{
		Message: "Hello world!",
	}, nil
}

// RegisterHealth registers the standard health service and the built-in echo service on server.
// The health service reports SERVING until ctx is done, then NOT_SERVING, so a client stops sending
// calls to a server that drains. Pass the context the server drains under.
func RegisterHealth(ctx context.Context, server *grpc.Server) {
	healthcheck := health.NewServer()
	healthpb.RegisterHealthServer(server, healthcheck)
	golibproto.RegisterEchoServiceServer(server, &echo{})

	healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	context.AfterFunc(ctx, func() {
		healthcheck.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	})
}
