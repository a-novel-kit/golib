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

// SetEchoServersContext registers the built-in echo + health-check services
// on the given gRPC server and starts a background goroutine that toggles the
// SERVING/NOT_SERVING status every healthPing tick. The goroutine returns
// when ctx is canceled, so the caller can tie its lifetime to graceful
// shutdown.
//
// A non-positive healthPing degrades to a tight toggle loop that still
// honors ctx cancellation. time.NewTicker would panic on a zero or negative
// duration, so the wait is built around time.After instead.
func SetEchoServersContext(ctx context.Context, server *grpc.Server, healthPing time.Duration) {
	healthcheck := health.NewServer()
	healthpb.RegisterHealthServer(server, healthcheck)

	golibproto.RegisterEchoServiceServer(server, &echo{})

	go func() {
		next := healthpb.HealthCheckResponse_SERVING

		for {
			healthcheck.SetServingStatus("", next)

			if next == healthpb.HealthCheckResponse_SERVING {
				next = healthpb.HealthCheckResponse_NOT_SERVING
			} else {
				next = healthpb.HealthCheckResponse_SERVING
			}

			if healthPing > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(healthPing):
				}
			} else {
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	}()
}
