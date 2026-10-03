package loggingpresets_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/a-novel-kit/golib/logging"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

// The panic value stands in for whatever a failing handler held when it panicked.
const panicValue = "dial tcp db.internal:5432: password authentication failed for user=admin"

func TestPanicInterceptors(t *testing.T) {
	t.Parallel()

	unary := func(ctx context.Context, config logging.RPCConfig) error {
		_, err := config.PanicUnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{},
			func(context.Context, any) (any, error) { panic(panicValue) },
		)

		return err
	}

	stream := func(ctx context.Context, config logging.RPCConfig) error {
		return config.PanicStreamInterceptor()(nil, &serverStream{ctx: ctx}, &grpc.StreamServerInfo{},
			func(any, grpc.ServerStream) error { panic(panicValue) },
		)
	}

	testCases := []struct {
		name string

		config logging.RPCConfig
		call   func(ctx context.Context, config logging.RPCConfig) error
	}{
		{name: "Gcloud/Unary", config: &loggingpresets.GRPCGcloud{}, call: unary},
		{name: "Gcloud/Stream", config: &loggingpresets.GRPCGcloud{}, call: stream},
		{name: "Local/Unary", config: &loggingpresets.GRPCLocal{}, call: unary},
		{name: "Local/Stream", config: &loggingpresets.GRPCLocal{}, call: stream},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			ctx, span := provider.Tracer("presets-test").Start(t.Context(), "rpc")
			err := testCase.call(ctx, testCase.config)

			span.End()

			require.Equal(t, codes.Internal, status.Code(err))
			require.Equal(t, "internal error", status.Convert(err).Message())

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, otelcodes.Error, spans[0].Status().Code)
			require.Contains(t, spans[0].Status().Description, panicValue)
		})
	}
}

// serverStream carries the RPC context into a stream interceptor.
type serverStream struct {
	grpc.ServerStream

	ctx context.Context //nolint:containedctx // A ServerStream exposes its RPC context through Context.
}

func (stream *serverStream) Context() context.Context {
	return stream.ctx
}
