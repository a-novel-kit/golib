package loggingpresets_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestGRPCOTel(t *testing.T) {
	t.Parallel()

	t.Run("Success/Unary", func(t *testing.T) {
		t.Parallel()

		response, err := (&loggingpresets.GRPCOTel{}).UnaryInterceptor()(t.Context(), "request",
			&grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"},
			func(_ context.Context, request any) (any, error) { return request.(string) + " handled", nil },
		)

		require.NoError(t, err)
		require.Equal(t, "request handled", response)
	})

	t.Run("Success/Stream", func(t *testing.T) {
		t.Parallel()

		handled := false
		err := (&loggingpresets.GRPCOTel{}).StreamInterceptor()(nil, &serverStream{ctx: t.Context()},
			&grpc.StreamServerInfo{FullMethod: "/test.Service/Call"},
			func(any, grpc.ServerStream) error {
				handled = true

				return nil
			},
		)

		require.NoError(t, err)
		require.True(t, handled)
	})

	t.Run("Error/PanicUnary", func(t *testing.T) {
		t.Parallel()

		requirePanicRecovered(t, &loggingpresets.GRPCOTel{}, panicUnaryCall)
	})

	t.Run("Error/PanicStream", func(t *testing.T) {
		t.Parallel()

		requirePanicRecovered(t, &loggingpresets.GRPCOTel{}, panicStreamCall)
	})
}
