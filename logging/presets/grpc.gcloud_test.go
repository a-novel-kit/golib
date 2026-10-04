package loggingpresets_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestGRPCGcloud(t *testing.T) {
	t.Parallel()

	unary := func(ctx context.Context, config *loggingpresets.GRPCGcloud) error {
		_, err := config.UnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"},
			func(context.Context, any) (any, error) { return nil, nil },
		)

		return err
	}

	stream := func(ctx context.Context, config *loggingpresets.GRPCGcloud) error {
		return config.StreamInterceptor()(nil, &serverStream{ctx: ctx},
			&grpc.StreamServerInfo{FullMethod: "/test.Service/Call"},
			func(any, grpc.ServerStream) error { return nil },
		)
	}

	testCases := []struct {
		name string

		call   func(ctx context.Context, config *loggingpresets.GRPCGcloud) error
		inSpan bool
	}{
		{name: "Unary", call: unary},
		{name: "Unary/InSpan", call: unary, inSpan: true},
		{name: "Stream/InSpan", call: stream, inSpan: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			var span trace.Span

			if testCase.inSpan {
				provider := sdktrace.NewTracerProvider()

				t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

				ctx, span = provider.Tracer("presets-test").Start(ctx, "rpc")
				defer span.End()
			}

			out := &bytes.Buffer{}
			require.NoError(t, testCase.call(ctx, &loggingpresets.GRPCGcloud{ProjectId: "project", Out: out}))

			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.NotEmpty(t, lines)

			for _, line := range lines {
				var entry gcloudEntry
				require.NoError(t, json.Unmarshal([]byte(line), &entry), line)
				require.Equal(t, "INFO", entry.Severity)

				if !testCase.inSpan {
					require.Empty(t, entry.Trace)
					require.Nil(t, entry.TraceSampled)

					continue
				}

				spanContext := span.SpanContext()
				require.Equal(t, "projects/project/traces/"+spanContext.TraceID().String(), entry.Trace)
				require.Equal(t, spanContext.SpanID().String(), entry.SpanID)
				require.Equal(t, spanContext.IsSampled(), *entry.TraceSampled)
			}
		})
	}
}
