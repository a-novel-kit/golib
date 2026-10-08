package downtime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/downtime"
)

func TestUnaryServerInterceptor(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Minute)
	scheduled := time.Now().Add(time.Hour)

	testCases := []struct {
		name string

		start  *time.Time
		method string

		expectCalled bool
	}{
		{
			name:         "Success/NoDowntime",
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectCalled: true,
		},
		{
			name:         "Success/Scheduled",
			start:        &scheduled,
			method:       "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
			expectCalled: true,
		},
		{
			name:         "Success/OpenMethod",
			start:        &started,
			method:       "/grpc.health.v1.Health/Check",
			expectCalled: true,
		},
		{
			name:   "Error/Started",
			start:  &started,
			method: "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign",
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

			interceptor := downtime.UnaryServerInterceptor(testCase.start, "/grpc.health.v1.Health/")
			_, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testCase.method}, handler)

			require.Equal(t, testCase.expectCalled, called)

			if testCase.expectCalled {
				require.NoError(t, err)

				return
			}

			require.Equal(t, codes.Unavailable, status.Code(err))
			require.True(t, downtime.Refused(err))
		})
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Minute)

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
			name:       "Error/Started",
			method:     "/anovel.jsonkeys.v2.KeysService/Watch",
			expectCode: codes.Unavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := downtime.StreamServerInterceptor(&started, "/grpc.health.v1.Health/")
			handler := func(_ any, _ grpc.ServerStream) error { return nil }
			err := interceptor(nil, nil, &grpc.StreamServerInfo{FullMethod: testCase.method}, handler)

			require.Equal(t, testCase.expectCode, status.Code(err))
		})
	}
}

func TestRefused(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Minute)
	_, refusal := downtime.UnaryServerInterceptor(&started)(
		t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: "/anovel.jsonkeys.v2.ClaimsSignService/ClaimsSign"}, nil,
	)

	otherReason, err := status.New(codes.Unavailable, "overloaded").
		WithDetails(&errdetails.ErrorInfo{Reason: "OVERLOADED"})
	require.NoError(t, err)

	testCases := []struct {
		name string

		err error

		expect bool
	}{
		{
			name:   "Refusal",
			err:    refusal,
			expect: true,
		},
		{
			name:   "WrappedRefusal",
			err:    fmt.Errorf("issue access token: %w", refusal),
			expect: true,
		},
		{
			name: "Nil",
		},
		{
			name: "NotStatus",
			err:  errors.New("foo"),
		},
		{
			name: "NoDetails",
			err:  status.Error(codes.Unavailable, "unavailable"),
		},
		{
			name: "OtherReason",
			err:  otherReason.Err(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expect, downtime.Refused(testCase.err))
		})
	}
}
