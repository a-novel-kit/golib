package grpcf_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/a-novel-kit/golib/grpcf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

const grpcServerTestTimeout = 5 * time.Second

// probeContextKey holds the value the base context of TestNewServer adds to every call.
type probeContextKey struct{}

// probeService answers with the base context's value, through the server's interceptors.
var probeService = &grpc.ServiceDesc{
	ServiceName: "test.Probe",
	HandlerType: (*any)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Call", Handler: probeCall}},
}

// probeCall decodes the empty request of test.Probe/Call, then answers it through interceptor.
func probeCall(
	_ any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor,
) (any, error) {
	request := &emptypb.Empty{}

	err := decode(request)
	if err != nil {
		return nil, err
	}

	return interceptor(ctx, request, &grpc.UnaryServerInfo{FullMethod: "/test.Probe/Call"}, probeValue)
}

// probeValue answers with the value the base context set.
func probeValue(ctx context.Context, _ any) (any, error) {
	value, _ := ctx.Value(probeContextKey{}).(string)

	return wrapperspb.String(value), nil
}

func TestListenAndServe(t *testing.T) {
	t.Parallel()

	t.Run("Success/ProcessCancellation", func(t *testing.T) {
		t.Parallel()

		address, config := freeGRPCServer(t, time.Second)
		ctx, cancel := context.WithCancel(t.Context())

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- grpcf.ListenAndServe(ctx, config, grpc.NewServer()) }()

		waitForGRPCServer(t, address)
		cancel()

		select {
		case err := <-serveErrors:
			require.NoError(t, err)
		case <-time.After(grpcServerTestTimeout):
			require.FailNow(t, "gRPC server did not stop after process cancellation")
		}
	})

	t.Run("Error/BindFailure", func(t *testing.T) {
		t.Parallel()

		listenerConfig := &net.ListenConfig{}
		listener, err := listenerConfig.Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, listener.Close()) })

		config := grpcf.ServerConfig{Port: listener.Addr().(*net.TCPAddr).Port, Shutdown: time.Second}

		err = grpcf.ListenAndServe(t.Context(), config, grpc.NewServer())
		require.Error(t, err)
		require.Contains(t, err.Error(), "listen for gRPC")
	})

	t.Run("Error/ForcedShutdown", func(t *testing.T) {
		t.Parallel()

		address, config := freeGRPCServer(t, 50*time.Millisecond)
		ctx, cancel := context.WithCancel(t.Context())
		server := grpc.NewServer()
		healthpb.RegisterHealthServer(server, health.NewServer())

		serveErrors := make(chan error, 1)
		go func() { serveErrors <- grpcf.ListenAndServe(ctx, config, server) }()

		connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		watchCtx, stopWatch := context.WithTimeout(t.Context(), grpcServerTestTimeout)
		t.Cleanup(stopWatch)

		watch, err := healthpb.NewHealthClient(connection).Watch(
			watchCtx,
			&healthpb.HealthCheckRequest{},
			grpc.WaitForReady(true),
		)
		require.NoError(t, err)

		_, err = watch.Recv()
		require.NoError(t, err)

		cancel()

		select {
		case err = <-serveErrors:
			require.ErrorIs(t, err, context.DeadlineExceeded)
		case <-time.After(grpcServerTestTimeout):
			require.FailNow(t, "gRPC server did not stop within its shutdown budget")
		}

		_, err = watch.Recv()
		require.Error(t, err)
	})
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Hour)

	testCases := []struct {
		name string

		downtimeStart *time.Time

		expectCode  codes.Code
		expectValue string
	}{
		{
			name: "Success",

			expectCode:  codes.OK,
			expectValue: "process",
		},
		{
			// A started downtime refuses the call, while health still answers.
			name: "Error/Downtime",

			downtimeStart: &started,

			expectCode: codes.Unavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			baseContext := func(ctx context.Context) context.Context {
				return context.WithValue(ctx, probeContextKey{}, "process")
			}

			server := grpcf.NewServer(
				t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, testCase.downtimeStart, baseContext,
			)
			grpcf.RegisterEchoServers(server).SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
			server.RegisterService(probeService, struct{}{})

			address, config := freeGRPCServer(t, time.Second)
			go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

			waitForGRPCServer(t, address)

			connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })

			ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
			t.Cleanup(cancel)

			healthResponse, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{})
			require.NoError(t, err)
			require.Equal(t, healthpb.HealthCheckResponse_SERVING, healthResponse.GetStatus())

			response := &wrapperspb.StringValue{}
			err = connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, response)
			require.Equal(t, testCase.expectCode, status.Code(err))
			require.Equal(t, testCase.expectValue, response.GetValue())
		})
	}

	t.Run("Success/StreamEndsWhenShutdownBegins", func(t *testing.T) {
		t.Parallel()

		// A stream would hold the drain to its deadline. It ends the moment shutdown begins, telling its
		// client to resume elsewhere, and the drain completes at once.
		ctx, cancel := context.WithCancel(t.Context())
		server := grpcf.NewServer(ctx, &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil)
		grpcf.RegisterEchoServers(server).SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

		address, config := freeGRPCServer(t, grpcServerTestTimeout)
		serveErrors := make(chan error, 1)

		go func() { serveErrors <- grpcf.ListenAndServe(ctx, config, server) }()

		connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		watchCtx, stopWatch := context.WithTimeout(t.Context(), grpcServerTestTimeout)
		t.Cleanup(stopWatch)

		watch, err := healthpb.NewHealthClient(connection).Watch(
			watchCtx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true),
		)
		require.NoError(t, err)

		_, err = watch.Recv()
		require.NoError(t, err)

		cancel()

		select {
		case err = <-serveErrors:
			// Not forced: the drain finished well inside its deadline.
			require.NoError(t, err)
		case <-time.After(2 * grpcServerTestTimeout):
			require.FailNow(t, "gRPC server did not stop")
		}

		_, err = watch.Recv()
		require.Equal(t, codes.Unavailable, status.Code(err))
	})
}

// freeGRPCServer returns the loopback address of a port no listener holds at the time of the call,
// and the configuration of a server on that port, draining within shutdown.
func freeGRPCServer(t *testing.T, shutdown time.Duration) (string, grpcf.ServerConfig) {
	t.Helper()

	listenerConfig := &net.ListenConfig{}

	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	err = listener.Close()
	if err != nil {
		panic(err)
	}

	return "127.0.0.1:" + strconv.Itoa(port), grpcf.ServerConfig{Port: port, Shutdown: shutdown}
}

// waitForGRPCServer returns once address accepts connections.
func waitForGRPCServer(t *testing.T, address string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	defer cancel()

	dialer := &net.Dialer{}

	for {
		connection, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			err = connection.Close()
			if err != nil {
				panic(err)
			}

			return
		}

		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
