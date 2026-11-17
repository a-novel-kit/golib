package grpcf_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/a-novel-kit/golib/grpcf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	"github.com/a-novel-kit/golib/servicetoken"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

// unreachableIssuer returns the URL of an issuer that serves no keys.
func unreachableIssuer(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	return server.URL
}

// withServiceTokens returns ctx carrying tokens as the service token metadata of an incoming call.
func withServiceTokens(ctx context.Context, tokens ...string) context.Context {
	md := metadata.MD{}
	for _, token := range tokens {
		md.Append(servicetoken.Header, token)
	}

	return metadata.NewIncomingContext(ctx, md)
}

func TestServiceTokenGate(t *testing.T) {
	t.Parallel()

	issuer := servicetokentest.NewIssuer(t)
	configured := serviceTokenConfig(issuer.URL)
	faulty := serviceTokenConfig(unreachableIssuer(t))

	optionalWithIssuer := configured
	optionalWithIssuer.IssuerOptional = true

	valid := signServiceToken(t, issuer, serviceTokenCaller)

	testCases := []struct {
		name string

		config servicetoken.Config
		call   func(ctx context.Context, connection *grpc.ClientConn) error
		tokens []string

		expectErr  error
		expectCode codes.Code
	}{
		{
			name: "Success/Unary",

			config: configured,
			call:   callUnary,
			tokens: []string{valid},

			expectCode: codes.OK,
		},
		{
			name: "Success/Stream",

			config: configured,
			call:   callStream,
			tokens: []string{valid},

			expectCode: codes.OK,
		},
		{
			// The kubelet probes the health service without a token.
			name: "Success/Health",

			config: configured,
			call:   callHealth,

			expectCode: codes.OK,
		},
		{
			name: "Error/MissingUnary",

			config: configured,
			call:   callUnary,

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/MissingStream",

			config: configured,
			call:   callStream,

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/Several",

			config: configured,
			call:   callUnary,
			tokens: []string{valid, valid},

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/Rejected",

			config: configured,
			call:   callUnary,
			tokens: []string{signServiceToken(t, issuer, "system:serviceaccount:studio:platform")},

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/VerifierFault",

			config: faulty,
			call:   callUnary,
			tokens: []string{valid},

			expectCode: codes.Unavailable,
		},
		{
			// A deployment that forgets the issuer stops at startup.
			name: "Error/IssuerRequired",

			config: servicetoken.Config{Audience: serviceTokenAudience},

			expectErr: servicetoken.ErrIssuerRequired,
		},
		{
			// A local or test stack runs without an issuer, and admits every call.
			name: "Success/IssuerOptional",

			config: servicetoken.Config{IssuerOptional: true},
			call:   callStream,

			expectCode: codes.OK,
		},
		{
			// A configured gate ignores the opt-out.
			name: "Error/IssuerOptionalWithIssuer",

			config: optionalWithIssuer,
			call:   callUnary,

			expectCode: codes.Unauthenticated,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gate, err := grpcf.ServiceTokenGate(testCase.config, http.DefaultClient)
			require.ErrorIs(t, err, testCase.expectErr)

			if err != nil {
				return
			}

			server := grpcf.NewGRPCServer(t.Context(), &loggingpresets.GRPCLocal{}, nil, nil, gate...)
			grpcf.RegisterHealth(t.Context(), server)
			server.RegisterService(probeService, struct{}{})

			address, config := freeGRPCServer(t, time.Second)
			go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

			waitForGRPCServer(t, address)

			connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })

			ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
			t.Cleanup(cancel)

			tokens := metadata.MD{}
			for _, token := range testCase.tokens {
				tokens.Append(servicetoken.Header, token)
			}

			ctx = metadata.NewOutgoingContext(ctx, tokens)
			require.Equal(t, testCase.expectCode, status.Code(testCase.call(ctx, connection)))
		})
	}
}

// callUnary calls test.Probe/Call once on connection.
func callUnary(ctx context.Context, connection *grpc.ClientConn) error {
	return connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, &wrapperspb.StringValue{})
}

// callStream opens test.Probe/Stream once on connection.
func callStream(ctx context.Context, connection *grpc.ClientConn) error {
	_, err := callProbeStream(ctx, connection)

	return err
}

// callHealth checks the health service once on connection.
func callHealth(ctx context.Context, connection *grpc.ClientConn) error {
	_, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{})

	return err
}

func TestServiceTokenUnaryInterceptor(t *testing.T) {
	t.Parallel()

	issuer := servicetokentest.NewIssuer(t)
	verifier := newServiceTokenVerifier(issuer.URL)
	faulty := newServiceTokenVerifier(unreachableIssuer(t))

	valid := signServiceToken(t, issuer, serviceTokenCaller)

	testCases := []struct {
		name string

		verifier *servicetoken.Verifier
		method   string
		tokens   []string

		expectCode codes.Code
	}{
		{
			name: "Success",

			verifier: verifier,
			method:   "/test.Probe/Call",
			tokens:   []string{valid},

			expectCode: codes.OK,
		},
		{
			// The kubelet probes the health service without a token.
			name: "Success/Health",

			verifier: verifier,
			method:   "/grpc.health.v1.Health/Check",

			expectCode: codes.OK,
		},
		{
			name: "Error/Missing",

			verifier: verifier,
			method:   "/test.Probe/Call",

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/Several",

			verifier: verifier,
			method:   "/test.Probe/Call",
			tokens:   []string{valid, valid},

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/Rejected",

			verifier: verifier,
			method:   "/test.Probe/Call",
			tokens:   []string{signServiceToken(t, issuer, "system:serviceaccount:studio:platform")},

			expectCode: codes.Unauthenticated,
		},
		{
			name: "Error/VerifierFault",

			verifier: faulty,
			method:   "/test.Probe/Call",
			tokens:   []string{valid},

			expectCode: codes.Unavailable,
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

			interceptor := grpcf.ServiceTokenUnaryInterceptor(testCase.verifier)
			_, err := interceptor(
				withServiceTokens(t.Context(), testCase.tokens...), nil,
				&grpc.UnaryServerInfo{FullMethod: testCase.method}, handler,
			)
			require.Equal(t, testCase.expectCode, status.Code(err))
			require.Equal(t, testCase.expectCode == codes.OK, called)
		})
	}
}

func TestServiceTokenStreamInterceptor(t *testing.T) {
	t.Parallel()

	issuer := servicetokentest.NewIssuer(t)
	verifier := newServiceTokenVerifier(issuer.URL)

	testCases := []struct {
		name string

		method string
		tokens []string

		expectCode codes.Code
	}{
		{
			name: "Success",

			method: "/test.Probe/Stream",
			tokens: []string{signServiceToken(t, issuer, serviceTokenCaller)},

			expectCode: codes.OK,
		},
		{
			name: "Success/Health",

			method: "/grpc.health.v1.Health/Watch",

			expectCode: codes.OK,
		},
		{
			name: "Error/Missing",

			method: "/test.Probe/Stream",

			expectCode: codes.Unauthenticated,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stream := &middleware.WrappedServerStream{WrappedContext: withServiceTokens(t.Context(), testCase.tokens...)}
			handler := func(_ any, _ grpc.ServerStream) error { return nil }

			interceptor := grpcf.ServiceTokenStreamInterceptor(verifier)
			err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: testCase.method}, handler)
			require.Equal(t, testCase.expectCode, status.Code(err))
		})
	}
}
