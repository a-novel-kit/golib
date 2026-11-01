package grpcf_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/downtime"
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
	Streams:     []grpc.StreamDesc{{StreamName: "Stream", Handler: probeStream, ServerStreams: true}},
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

// probeStream answers test.Probe/Stream with the value the base context set.
func probeStream(_ any, stream grpc.ServerStream) error {
	value, _ := stream.Context().Value(probeContextKey{}).(string)

	return stream.SendMsg(wrapperspb.String(value))
}

// callProbeStream opens test.Probe/Stream on connection and returns its one answer.
func callProbeStream(ctx context.Context, connection *grpc.ClientConn) (string, error) {
	stream, err := connection.NewStream(ctx, &probeService.Streams[0], "/test.Probe/Stream")
	if err != nil {
		return "", err
	}

	err = stream.CloseSend()
	if err != nil {
		return "", err
	}

	response := &wrapperspb.StringValue{}
	err = stream.RecvMsg(response)

	return response.GetValue(), err
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
			name: "Success/BaseContext",

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

			streamValue, err := callProbeStream(ctx, connection)
			require.Equal(t, testCase.expectCode, status.Code(err))
			require.Equal(t, testCase.expectValue, streamValue)
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

	t.Run("Success/ConnectionEndsAtItsAge", func(t *testing.T) {
		t.Parallel()

		// A call outlives the connection it started on, and the next call opens a new one.
		calls := &heldCalls{started: make(chan struct{}, 1), release: make(chan struct{})}

		server := grpcf.NewServer(t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil)
		server.RegisterService(heldService, calls)

		address, config := freeGRPCServer(t, time.Second)
		go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

		waitForGRPCServer(t, address)

		connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		t.Cleanup(cancel)

		opened := time.Now()
		held := make(chan error, 1)

		go func() { held <- connection.Invoke(ctx, "/test.Held/Hold", &emptypb.Empty{}, &emptypb.Empty{}) }()

		select {
		case <-calls.started:
		case <-ctx.Done():
			require.FailNow(t, "held call did not start")
		}

		// The client leaves Ready once the server announces the end of the connection, 30 seconds after
		// it opened, give or take a tenth.
		require.True(t, connection.WaitForStateChange(ctx, connectivity.Ready))

		age := time.Since(opened)
		require.GreaterOrEqual(t, age, 27*time.Second)
		require.Less(t, age, 34*time.Second)

		close(calls.release)
		require.NoError(t, <-held)

		err = connection.Invoke(ctx, "/test.Held/Pass", &emptypb.Empty{}, &emptypb.Empty{})
		require.NoError(t, err)
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

// faultBehavior is how a call to faultService ends.
type faultBehavior func(ctx context.Context) error

// faultService ends each call as the faultBehavior it is registered with.
var faultService = &grpc.ServiceDesc{
	ServiceName: "test.Fault",
	HandlerType: (*any)(nil),
	Methods:     []grpc.MethodDesc{{MethodName: "Call", Handler: faultCall}},
	Streams:     []grpc.StreamDesc{{StreamName: "Stream", Handler: faultStream, ServerStreams: true}},
}

// faultCall decodes the empty request of test.Fault/Call, then ends it through interceptor.
func faultCall(
	srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor,
) (any, error) {
	err := decode(&emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	handler := func(ctx context.Context, _ any) (any, error) { return &emptypb.Empty{}, srv.(faultBehavior)(ctx) }

	return interceptor(ctx, &emptypb.Empty{}, &grpc.UnaryServerInfo{FullMethod: "/test.Fault/Call"}, handler)
}

// faultStream ends test.Fault/Stream.
func faultStream(srv any, stream grpc.ServerStream) error {
	return srv.(faultBehavior)(stream.Context())
}

// panicCall panics with a value that holds caller input.
func panicCall(context.Context) error {
	panic("secret")
}

// tracedConfig is a telemetry backend that exports nothing but traces each call on provider.
type tracedConfig struct {
	otelpresets.Disabled

	provider trace.TracerProvider
}

func (config *tracedConfig) RPCInterceptor() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(config.provider)))
}

// driverError stands for an error type of a dependency, whose message holds caller input.
type driverError struct{}

func (*driverError) Error() string {
	return "password authentication failed for user=secret"
}

func TestNewServerFaults(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Hour)

	// The refusal a dependency in planned downtime sends.
	_, errDowntime := downtime.UnaryServerInterceptor(&started)(
		t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: "/jsonkeys.ClaimsSignService/ClaimsSign"}, nil,
	)

	testCases := []struct {
		name string

		behavior      faultBehavior
		downtimeStart *time.Time

		expectCode codes.Code
		// The innermost type of a reported fault. Empty expects no error.type.
		expectType string
	}{
		{
			name: "Error/Fault",

			behavior: func(context.Context) error {
				return grpcf.Fault(codes.Internal, "internal error", fmt.Errorf("select jwk: %w", &driverError{}))
			},

			expectCode: codes.Internal,
			expectType: "*grpcf_test.driverError",
		},
		{
			// A bare status error ends the chain.
			name: "Error/Unavailable",

			behavior: func(context.Context) error {
				return status.Error(codes.Unavailable, "service dependencies unavailable")
			},

			expectCode: codes.Unavailable,
			expectType: "*status.Error",
		},
		{
			name: "Error/Panic",

			behavior: panicCall,

			expectCode: codes.Internal,
			expectType: "string",
		},
		{
			name: "Error/NotFound",

			behavior: func(context.Context) error { return status.Error(codes.NotFound, "jwk not found") },

			expectCode: codes.NotFound,
		},
		{
			name: "Error/InvalidArgument",

			behavior: func(context.Context) error { return status.Error(codes.InvalidArgument, "invalid key id") },

			expectCode: codes.InvalidArgument,
		},
		{
			name: "Error/DependencyDowntime",

			behavior: func(context.Context) error { return errDowntime },

			expectCode: codes.Unavailable,
		},
		{
			name: "Error/Downtime",

			behavior:      func(context.Context) error { return nil },
			downtimeStart: &started,

			expectCode: codes.Unavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			server := grpcf.NewServer(
				t.Context(), &tracedConfig{provider: provider}, &loggingpresets.GRPCLocal{}, testCase.downtimeStart, nil,
			)
			server.RegisterService(faultService, testCase.behavior)

			address, config := freeGRPCServer(t, time.Second)
			go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

			waitForGRPCServer(t, address)

			connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })

			ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
			t.Cleanup(cancel)

			err = connection.Invoke(ctx, "/test.Fault/Call", &emptypb.Empty{}, &emptypb.Empty{})
			require.Equal(t, testCase.expectCode, status.Code(err))
			require.NotContains(t, err.Error(), "secret")

			stream, err := connection.NewStream(ctx, &faultService.Streams[0], "/test.Fault/Stream")
			require.NoError(t, err)
			require.NoError(t, stream.CloseSend())
			require.Equal(t, testCase.expectCode, status.Code(stream.RecvMsg(&emptypb.Empty{})))

			// A server span ends after its caller has the answer.
			require.Eventually(t, func() bool { return len(recorder.Ended()) == 2 }, grpcServerTestTimeout, 10*time.Millisecond)

			for _, span := range recorder.Ended() {
				attributes := attribute.NewSet(span.Attributes()...)
				errorType, _ := attributes.Value("error.type")
				require.Equal(t, testCase.expectType, errorType.AsString(), span.Name())
			}
		})
	}
}
