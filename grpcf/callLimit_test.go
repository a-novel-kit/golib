package grpcf_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/a-novel-kit/golib/grpcf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

// heldService holds each call to test.Held/Hold until the test ends it, and answers test.Held/Pass at
// once.
var heldService = &grpc.ServiceDesc{
	ServiceName: "test.Held",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Hold", Handler: holdCall},
		{MethodName: "Pass", Handler: passCall},
	},
}

// heldCalls reports each call test.Held/Hold starts, and ends one for each value sent on release.
type heldCalls struct {
	started chan struct{}
	release chan struct{}
}

// holdCall runs test.Held/Hold until heldCalls releases it or its client cancels it.
func holdCall(srv any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	err := decode(&emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	calls := srv.(*heldCalls)
	calls.started <- struct{}{}

	select {
	case <-calls.release:
	case <-ctx.Done():
	}

	return &emptypb.Empty{}, nil
}

// passCall answers test.Held/Pass at once.
func passCall(_ any, _ context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	return &emptypb.Empty{}, decode(&emptypb.Empty{})
}

// scrapeCallLimit returns what callLimit answers a scrape.
func scrapeCallLimit(t *testing.T, callLimit *grpcf.CallLimit) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	callLimit.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))

	return recorder
}

// callsInFlightGauge is the gauge a CallLimit serves while running calls run.
func callsInFlightGauge(running int) string {
	return "# HELP grpc_server_calls_in_flight The gRPC calls the server runs now.\n" +
		"# TYPE grpc_server_calls_in_flight gauge\n" +
		"grpc_server_calls_in_flight " + strconv.Itoa(running) + "\n"
}

func TestCallLimit(t *testing.T) {
	t.Parallel()

	// Each case holds two calls, then makes one more.
	const held = 2

	testCases := []struct {
		name string

		limit int
		// end ends the first held call before the next one starts, or leaves it running when nil.
		end func(release chan<- struct{}, cancel context.CancelFunc)

		expectEndCode codes.Code
		expectCode    codes.Code
	}{
		{
			name: "Success/Unbounded",

			limit: 0,

			expectCode: codes.OK,
		},
		{
			name: "Success/NegativeUnbounded",

			limit: -1,

			expectCode: codes.OK,
		},
		{
			name: "Success/UnderLimit",

			limit: held + 1,

			expectCode: codes.OK,
		},
		{
			// The health service still answers.
			name: "Error/AtLimit",

			limit: held,

			expectCode: codes.Unavailable,
		},
		{
			name: "Success/SlotFreedOnEnd",

			limit: held,
			end:   func(release chan<- struct{}, _ context.CancelFunc) { release <- struct{}{} },

			expectEndCode: codes.OK,
			expectCode:    codes.OK,
		},
		{
			name: "Success/SlotFreedOnCancel",

			limit: held,
			end:   func(_ chan<- struct{}, cancel context.CancelFunc) { cancel() },

			expectEndCode: codes.Canceled,
			expectCode:    codes.OK,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := &heldCalls{started: make(chan struct{}, held), release: make(chan struct{})}
			callLimit := grpcf.NewCallLimit(testCase.limit)

			server := grpcf.NewServer(
				t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil, callLimit,
			)
			grpcf.RegisterEchoServers(server).SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
			server.RegisterService(heldService, calls)

			address, config := freeGRPCServer(t, time.Second)
			go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

			waitForGRPCServer(t, address)

			connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })

			ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
			t.Cleanup(cancel)

			results := make(chan error, held)
			cancels := make([]context.CancelFunc, held)

			for i := range held {
				callCtx, cancelCall := context.WithCancel(ctx)
				cancels[i] = cancelCall

				go func() { results <- connection.Invoke(callCtx, "/test.Held/Hold", &emptypb.Empty{}, &emptypb.Empty{}) }()
			}

			for range held {
				select {
				case <-calls.started:
				case <-ctx.Done():
					require.FailNow(t, "held calls did not start")
				}
			}

			// Even an unbounded server counts its calls.
			scrape := scrapeCallLimit(t, callLimit)
			require.Equal(t, "text/plain; version=0.0.4; charset=utf-8", scrape.Header().Get("Content-Type"))
			require.Equal(t, callsInFlightGauge(held), scrape.Body.String())

			running := held

			if testCase.end != nil {
				testCase.end(calls.release, cancels[0])

				select {
				case err = <-results:
					require.Equal(t, testCase.expectEndCode, status.Code(err))

					running--
				case <-ctx.Done():
					require.FailNow(t, "held call did not end")
				}
			}

			// A slot frees once the transport cancels its call's context, just after the call returns.
			require.Eventually(t, func() bool {
				err := connection.Invoke(ctx, "/test.Held/Pass", &emptypb.Empty{}, &emptypb.Empty{})

				return status.Code(err) == testCase.expectCode
			}, grpcServerTestTimeout, 10*time.Millisecond)

			healthResponse, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{})
			require.NoError(t, err)
			require.Equal(t, healthpb.HealthCheckResponse_SERVING, healthResponse.GetStatus())

			// Each passing call leaves the count once its context ends. Refused calls and health checks
			// never enter it.
			require.Eventually(t, func() bool {
				return scrapeCallLimit(t, callLimit).Body.String() == callsInFlightGauge(running)
			}, grpcServerTestTimeout, 10*time.Millisecond)

			close(calls.release)

			for range running {
				select {
				case err = <-results:
					require.NoError(t, err)
				case <-ctx.Done():
					require.FailNow(t, "held calls did not end")
				}
			}

			require.Eventually(t, func() bool {
				return scrapeCallLimit(t, callLimit).Body.String() == callsInFlightGauge(0)
			}, grpcServerTestTimeout, 10*time.Millisecond)
		})
	}
}
