package grpcf_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/grpcf"
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
