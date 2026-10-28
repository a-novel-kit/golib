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

	"github.com/a-novel-kit/golib/servicetoken"
)

var (
	_ CredentialsProvider = (*LocalCredentialsProvider)(nil)
	_ CredentialsProvider = (*GcloudCredentialsProvider)(nil)
	_ CredentialsProvider = (*ServiceTokenCredentialsProvider)(nil)
)

// CredentialsProvider supplies the gRPC dial options of a connection. They set its transport
// security and, when the target needs it, the authentication of each call. Pick the implementation
// that matches the target: [LocalCredentialsProvider] for a plaintext local connection,
// [GcloudCredentialsProvider] for a Google Cloud endpoint behind TLS, or
// [ServiceTokenCredentialsProvider] for a service in the same Kubernetes cluster.
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

// GcloudCredentialsProvider dials a Google Cloud endpoint over TLS. It attaches a Google-issued OIDC
// identity token to every call, which is how Cloud Run authenticates traffic between services.
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

// ServiceTokenCredentialsProvider dials a service in the same Kubernetes cluster over the cluster
// network, without transport security. It sends the caller's service token with every call, which
// the callee checks with [ServiceTokenUnaryInterceptor] and [ServiceTokenStreamInterceptor].
type ServiceTokenCredentialsProvider struct {
	// ServiceTokenPath is the file the kubelet projects the caller's service token into. Each call
	// reads it again, so a rotated token takes effect on the next call.
	ServiceTokenPath string
}

func (provider *ServiceTokenCredentialsProvider) Options(_ context.Context) ([]grpc.DialOption, error) {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(serviceTokenCredentials(provider.ServiceTokenPath)),
	}, nil
}

// serviceTokenCredentials sends the service token in the file it names with every call.
type serviceTokenCredentials string

func (path serviceTokenCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	token, err := servicetoken.Read(string(path))
	if err != nil {
		return nil, err
	}

	return map[string]string{servicetoken.Header: token}, nil
}

// RequireTransportSecurity lets the token travel over the plaintext connection the provider dials.
func (serviceTokenCredentials) RequireTransportSecurity() bool {
	return false
}
