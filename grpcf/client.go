package grpcf

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/a-novel-kit/golib/servicetoken"
)

var (
	_ CredentialsProvider = (*LocalCredentialsProvider)(nil)
	_ CredentialsProvider = (*ServiceTokenCredentialsProvider)(nil)
)

// CredentialsProvider supplies the gRPC dial options of a connection. They set its transport
// security and, when the target needs it, the authentication and balancing of each call. Pick the
// implementation that matches the target: [LocalCredentialsProvider] for a plaintext local
// connection, or [ServiceTokenCredentialsProvider] for a service in the same Kubernetes cluster.
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

// ServiceTokenCredentialsProvider dials a service in the same Kubernetes cluster, over TLS once
// [ServiceTokenCredentialsProvider.TLS] is set. It sends the caller's service token with every call,
// which the callee checks with [ServiceTokenUnaryInterceptor] and [ServiceTokenStreamInterceptor].
//
// The connection sends each call to the next address the target resolves to, as a headless Service
// resolves to its ready pods. A call refused with Unavailable is retried on the next address, up to
// 4 attempts in all, and a throttle pauses retries while many calls fail.
//
// The retry rests on one rule for servers: Unavailable means the call did not run. [CallLimit] and a
// planned downtime refuse that way, before the handler. A handler that has had an effect answers
// another code.
type ServiceTokenCredentialsProvider struct {
	// ServiceTokenPath is the file the kubelet projects the caller's service token into. Each call
	// reads it again, so a rotated token takes effect on the next call.
	ServiceTokenPath string
	// TLS dials TLS 1.3, and verifies the server's certificate for the target's host against the
	// system roots, which SSL_CERT_DIR extends. False dials plaintext.
	TLS bool
}

func (provider *ServiceTokenCredentialsProvider) Options(_ context.Context) ([]grpc.DialOption, error) {
	transport := insecure.NewCredentials()
	if provider.TLS {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13})
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(serviceTokenCredentials(provider.ServiceTokenPath)),
		grpc.WithDefaultServiceConfig(inClusterServiceConfig),
	}, nil
}

// inClusterServiceConfig balances the calls of a [ServiceTokenCredentialsProvider] connection
// round robin, and retries every method on Unavailable. The retry throttle holds 10 tokens. Each
// refused attempt spends one and each success earns a tenth back, and retries stop while 5 or fewer
// remain.
const inClusterServiceConfig = `{
	"loadBalancingConfig": [{"round_robin": {}}],
	"methodConfig": [{
		"name": [{}],
		"retryPolicy": {
			"maxAttempts": 4,
			"initialBackoff": "0.1s",
			"maxBackoff": "1s",
			"backoffMultiplier": 2,
			"retryableStatusCodes": ["UNAVAILABLE"]
		}
	}],
	"retryThrottling": {"maxTokens": 10, "tokenRatio": 0.1}
}`

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
