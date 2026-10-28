package loggingpresets_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/a-novel-kit/golib/logging"
)

// The panic value stands in for whatever a failing handler held when it panicked.
const panicValue = "dial tcp db.internal:5432: password authentication failed for user=admin"

// panicUnaryCall runs a unary handler that panics under ctx, through the recovery of config.
func panicUnaryCall(ctx context.Context, config logging.RPCConfig) error {
	_, err := config.PanicUnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) { panic(panicValue) },
	)

	return err
}

// panicStreamCall runs a streaming handler that panics under ctx, through the recovery of config.
func panicStreamCall(ctx context.Context, config logging.RPCConfig) error {
	return config.PanicStreamInterceptor()(nil, &serverStream{ctx: ctx}, &grpc.StreamServerInfo{},
		func(any, grpc.ServerStream) error { panic(panicValue) },
	)
}

// requirePanicRecovered runs call through the panic recovery of config, under a span. The panic must
// reach the caller as a fixed Internal status, and only the span may hold the panic value.
func requirePanicRecovered(
	t *testing.T, config logging.RPCConfig, call func(ctx context.Context, config logging.RPCConfig) error,
) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

	ctx, span := provider.Tracer("presets-test").Start(t.Context(), "rpc")
	err := call(ctx, config)

	span.End()

	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal error", status.Convert(err).Message())

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, otelcodes.Error, spans[0].Status().Code)
	require.Contains(t, spans[0].Status().Description, panicValue)
	require.Contains(t, spans[0].Attributes(), attribute.String("error.type", "string"))
}

// buildConcurrently builds every interceptor of config, each in its own goroutine. The goroutines
// start together, so the race detector sees any setup they share without synchronization.
func buildConcurrently(config logging.RPCConfig) {
	builds := []func(){
		func() { config.UnaryInterceptor() },
		func() { config.StreamInterceptor() },
		func() { config.PanicUnaryInterceptor() },
		func() { config.PanicStreamInterceptor() },
	}

	start := make(chan struct{})

	var wg sync.WaitGroup
	for _, build := range builds {
		wg.Go(func() {
			<-start
			build()
		})
	}

	close(start)
	wg.Wait()
}

// serverStream carries the RPC context into a stream interceptor.
type serverStream struct {
	grpc.ServerStream

	ctx context.Context //nolint:containedctx // A ServerStream exposes its RPC context through Context.
}

func (stream *serverStream) Context() context.Context {
	return stream.ctx
}
