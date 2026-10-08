package downtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/downtime"
)

// retryDelay returns the RetryInfo delay of err, or zero without one.
func retryDelay(t *testing.T, err error) time.Duration {
	t.Helper()

	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.RetryInfo); ok {
			return info.GetRetryDelay().AsDuration()
		}
	}

	return 0
}

func TestUnaryServerInterceptor(t *testing.T) {
	t.Parallel()

	// RFC 3339 metadata keeps whole seconds.
	now := time.Now().UTC().Truncate(time.Second)
	inProgress := window(now, -time.Minute, time.Hour, "json-keys", "authentication")
	pastEnd := window(now, -2*time.Hour, -time.Hour, "json-keys")

	testCases := []struct {
		name string

		window *downtime.Window
		method string

		expectCalled bool
		expectWindow *downtime.Window
		expectRetry  bool
	}{
		{
			name:         "Success/NoWindow",
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectCalled: true,
		},
		{
			name:         "Success/Notice",
			window:       window(now, time.Hour, 2*time.Hour, "json-keys"),
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectCalled: true,
		},
		{
			name:         "Success/OpenMethod",
			window:       inProgress,
			method:       "/grpc.health.v1.Health/Check",
			expectCalled: true,
		},
		{
			name:         "Error/InProgress",
			window:       inProgress,
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectWindow: inProgress,
			expectRetry:  true,
		},
		{
			name:         "Error/PastEnd",
			window:       pastEnd,
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectWindow: pastEnd,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := func(_ context.Context, _ any) (any, error) {
				called = true

				return "ok", nil
			}

			interceptor := downtime.UnaryServerInterceptor(testCase.window, "json-keys", "/grpc.health.v1.Health/")
			_, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testCase.method}, handler)

			require.Equal(t, testCase.expectCalled, called)

			if testCase.expectCalled {
				require.NoError(t, err)

				return
			}

			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Equal(t, testCase.expectWindow, downtime.FromError(err))
			require.Equal(t, testCase.expectRetry, retryDelay(t, err) > 0)
		})
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	inProgress := window(now, -time.Minute, time.Hour, "json-keys")

	testCases := []struct {
		name string

		method string

		expectCode codes.Code
	}{
		{
			name:       "Success/OpenMethod",
			method:     "/grpc.health.v1.Health/Watch",
			expectCode: codes.OK,
		},
		{
			name:       "Error/InProgress",
			method:     "/anovel.jsonkeys.v2.KeysService/Watch",
			expectCode: codes.Unavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := downtime.StreamServerInterceptor(inProgress, "json-keys", "/grpc.health.v1.Health/")
			handler := func(_ any, _ grpc.ServerStream) error { return nil }
			err := interceptor(nil, nil, &grpc.StreamServerInfo{FullMethod: testCase.method}, handler)

			require.Equal(t, testCase.expectCode, status.Code(err))
		})
	}
}

func TestFromError(t *testing.T) {
	t.Parallel()

	otherReason, err := status.New(codes.Unavailable, "overloaded").
		WithDetails(&errdetails.ErrorInfo{Reason: "OVERLOADED"})
	require.NoError(t, err)

	badMetadata, err := status.New(codes.Unavailable, "planned downtime").WithDetails(&errdetails.ErrorInfo{
		Reason:   downtime.Reason,
		Metadata: map[string]string{"services": "json-keys", "start": "soon", "end": "later"},
	})
	require.NoError(t, err)

	testCases := []struct {
		name string

		err error
	}{
		{
			name: "Success/NotStatus",
			err:  errors.New("foo"),
		},
		{
			name: "Success/NoDetails",
			err:  status.Error(codes.Unavailable, "unavailable"),
		},
		{
			name: "Success/OtherReason",
			err:  otherReason.Err(),
		},
		{
			name: "Success/MalformedMetadata",
			err:  badMetadata.Err(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Nil(t, downtime.FromError(testCase.err))
		})
	}
}
