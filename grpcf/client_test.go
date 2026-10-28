package grpcf_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/a-novel-kit/golib/grpcf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

func TestServiceTokenCredentialsProvider(t *testing.T) {
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
}
