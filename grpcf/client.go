package grpcf

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"google.golang.org/api/idtoken"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
)

var (
	_ CredentialsProvider = (*LocalCredentialsProvider)(nil)
	_ CredentialsProvider = (*GcloudCredentialsProvider)(nil)
)

// CredentialsProvider supplies the gRPC dial options that carry a connection's
// transport security and, where needed, its per-call authentication. Choose the
// implementation that matches the target: [LocalCredentialsProvider] for a
// plaintext local connection, [GcloudCredentialsProvider] for a TLS-secured
// Google Cloud endpoint.
type CredentialsProvider interface {
	// Options returns the dial options to pass when creating the client. The
	// context bounds any token exchange the provider performs while building them.
	Options(ctx context.Context) ([]grpc.DialOption, error)
}

// LocalCredentialsProvider dials without transport security, for plaintext
// connections to a service on a trusted local network.
type LocalCredentialsProvider struct{}

func (provider *LocalCredentialsProvider) Options(_ context.Context) ([]grpc.DialOption, error) {
	return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
}

// GcloudCredentialsProvider dials a Google Cloud endpoint over TLS and attaches
// a Google-issued OIDC identity token to every call, the scheme Cloud Run uses
// to authenticate service-to-service traffic.
type GcloudCredentialsProvider struct {
	// Host is the endpoint hostname, without scheme or port. It sets both the
	// identity token's audience and the TLS authority for the connection.
	Host string
}

func (provider *GcloudCredentialsProvider) Options(ctx context.Context) ([]grpc.DialOption, error) {
	systemRoots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("error getting system cert: %w", err)
	}

	tokenSource, err := idtoken.NewTokenSource(ctx, "https://"+provider.Host)
	if err != nil {
		return nil, fmt.Errorf("error getting token source: %w", err)
	}

	cred := credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    systemRoots,
	})

	return []grpc.DialOption{
		grpc.WithTransportCredentials(cred),
		grpc.WithAuthority(provider.Host + ":443"),
		grpc.WithPerRPCCredentials(oauth.TokenSource{TokenSource: tokenSource}),
	}, nil
}
