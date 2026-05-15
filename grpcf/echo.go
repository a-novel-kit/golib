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

// SetEchoServers registers the built-in echo + health-check services on the
// given gRPC server and starts a background goroutine that toggles the
// SERVING/NOT_SERVING status every healthPing tick.
//
// Deprecated: the started goroutine has no stop signal and outlives the
// caller. Use SetEchoServersContext instead — it accepts a context and the
// goroutine exits cleanly when the context is canceled (e.g. during graceful
// shutdown).
func SetEchoServers(server *grpc.Server, healthPing time.Duration) {
	SetEchoServersContext(context.Background(), server, healthPing)
}

// SetEchoServersContext is the ctx-aware variant of SetEchoServers: the
// background health-toggle goroutine returns when ctx is canceled, instead of
// running for the lifetime of the process.
func SetEchoServersContext(ctx context.Context, server *grpc.Server, healthPing time.Duration) {
	healthcheck := health.NewServer()
	healthpb.RegisterHealthServer(server, healthcheck)

	golibproto.RegisterEchoServiceServer(server, &echo{})

	go func() {
		ticker := time.NewTicker(healthPing)
		defer ticker.Stop()

		next := healthpb.HealthCheckResponse_SERVING

		for {
			healthcheck.SetServingStatus("", next)

			if next == healthpb.HealthCheckResponse_SERVING {
				next = healthpb.HealthCheckResponse_NOT_SERVING
			} else {
				next = healthpb.HealthCheckResponse_SERVING
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
