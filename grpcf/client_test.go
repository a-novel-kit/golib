package grpcf_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/a-novel-kit/golib/grpcf"
	"github.com/a-novel-kit/golib/internal/tlstest"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

func TestServiceTokenCredentialsProvider(t *testing.T) {
	t.Parallel()

	t.Run("Success/ServiceToken", func(t *testing.T) {
		t.Parallel()

		issuer := servicetokentest.NewIssuer(t)
		verifier := newServiceTokenVerifier(issuer.URL)

		server := grpcf.NewServer(
			t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil,
			grpc.ChainUnaryInterceptor(grpcf.ServiceTokenUnaryInterceptor(verifier)),
			grpc.ChainStreamInterceptor(grpcf.ServiceTokenStreamInterceptor(verifier)),
		)
		server.RegisterService(probeService, struct{}{})

		address, config := freeGRPCServer(t, time.Second)
		go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

		waitForGRPCServer(t, address)

		path := filepath.Join(t.TempDir(), "token")
		writeServiceToken(t, path, signServiceToken(t, issuer, serviceTokenCaller))

		provider := &grpcf.ServiceTokenCredentialsProvider{ServiceTokenPath: path}
		options, err := provider.Options(t.Context())
		require.NoError(t, err)

		connection, err := grpc.NewClient(address, options...)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
		t.Cleanup(cancel)

		err = connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, &wrapperspb.StringValue{})
		require.NoError(t, err)

		_, err = callProbeStream(ctx, connection)
		require.NoError(t, err)

		// The kubelet replaces the file, and the next call sends the new token.
		writeServiceToken(t, path, signServiceToken(t, issuer, "system:serviceaccount:studio:platform"))

		err = connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, &wrapperspb.StringValue{})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("Error/TLSToPlaintextServer", func(t *testing.T) {
		t.Parallel()

		server := grpcf.NewServer(t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil)
		server.RegisterService(probeService, struct{}{})

		address, config := freeGRPCServer(t, time.Second)
		go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

		waitForGRPCServer(t, address)

		err := invokeProbe(t, address, &grpcf.ServiceTokenCredentialsProvider{ServiceTokenPath: tokenFile(t), TLS: true})
		require.Equal(t, codes.Unavailable, status.Code(err))
	})

	t.Run("Success/RetryOnAnotherServer", func(t *testing.T) {
		t.Parallel()

		// The full server refuses every call from its tap handle, as a CallLimit at its limit does.
		var refused atomic.Int32

		refuse := func(ctx context.Context, _ *tap.Info) (context.Context, error) {
			refused.Add(1)

			return ctx, status.Error(codes.Unavailable, "server at its concurrent call limit: retry later")
		}

		full := grpc.NewServer(grpc.InTapHandle(refuse))
		open := grpcf.NewServer(t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil)
		open.RegisterService(probeService, struct{}{})

		fullAddress, fullConfig := freeGRPCServer(t, time.Second)
		go func() { _ = grpcf.ListenAndServe(t.Context(), fullConfig, full) }()

		openAddress, openConfig := freeGRPCServer(t, time.Second)
		go func() { _ = grpcf.ListenAndServe(t.Context(), openConfig, open) }()

		waitForGRPCServer(t, fullAddress)
		waitForGRPCServer(t, openAddress)

		// The target resolves to both servers, the full one first, as a headless Service resolves to its
		// pods.
		targets := manual.NewBuilderWithScheme("test")
		targets.InitialState(resolver.State{
			Addresses: []resolver.Address{{Addr: fullAddress}, {Addr: openAddress}},
		})

		path := filepath.Join(t.TempDir(), "token")
		writeServiceToken(t, path, "token")

		provider := &grpcf.ServiceTokenCredentialsProvider{ServiceTokenPath: path}
		options, err := provider.Options(t.Context())
		require.NoError(t, err)

		connection, err := grpc.NewClient("test:///service", append(options, grpc.WithResolvers(targets))...)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })

		ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
		t.Cleanup(cancel)

		// Calls take turns between the servers, so one reaches the full server once the client has
		// connected to it. Every call succeeds.
		for refused.Load() == 0 {
			err = connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, &wrapperspb.StringValue{})
			require.NoError(t, err)
		}
	})
}

// TestServiceTokenCredentialsProviderTLS trusts its authority through SSL_CERT_DIR, as a pod trusts
// the cluster's CA. Go reads the variable once, when the process first verifies a certificate
// against the system roots, and no other test of this package does.
//
//nolint:paralleltest // t.Setenv changes the process environment.
func TestServiceTokenCredentialsProviderTLS(t *testing.T) {
	trusted := tlstest.NewAuthority(t)
	roots := t.TempDir()
	trusted.WriteCertificate(t, roots)
	t.Setenv("SSL_CERT_DIR", roots)

	testCases := []struct {
		name string

		authority *tlstest.Authority

		expectCode codes.Code
	}{
		{
			name: "Success",

			authority: trusted,

			expectCode: codes.OK,
		},
		{
			name: "Error/UnknownAuthority",

			authority: tlstest.NewAuthority(t),

			expectCode: codes.Unavailable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := grpcf.NewServer(t.Context(), &otelpresets.Disabled{}, &loggingpresets.GRPCLocal{}, nil, nil)
			server.RegisterService(probeService, struct{}{})

			address, config := freeGRPCServer(t, time.Second)
			config.TLS, _ = testCase.authority.WriteKeyPair(t, t.TempDir())

			go func() { _ = grpcf.ListenAndServe(t.Context(), config, server) }()

			waitForGRPCServer(t, address)

			provider := &grpcf.ServiceTokenCredentialsProvider{ServiceTokenPath: tokenFile(t), TLS: true}
			require.Equal(t, testCase.expectCode, status.Code(invokeProbe(t, address, provider)))
		})
	}
}

// tokenFile returns a file holding a service token, which a server without the service token
// interceptors ignores.
func tokenFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "token")
	writeServiceToken(t, path, "token")

	return path
}

// invokeProbe calls the probe service at address once, through the connection provider configures.
func invokeProbe(t *testing.T, address string, provider grpcf.CredentialsProvider) error {
	t.Helper()

	options, err := provider.Options(t.Context())
	require.NoError(t, err)

	connection, err := grpc.NewClient(address, options...)
	require.NoError(t, err)

	defer func() { _ = connection.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), grpcServerTestTimeout)
	defer cancel()

	return connection.Invoke(ctx, "/test.Probe/Call", &emptypb.Empty{}, &wrapperspb.StringValue{})
}
