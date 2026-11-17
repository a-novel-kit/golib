package grpcf

import (
	"cmp"
	"context"
	"crypto/tls"
	"net"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/a-novel-kit/golib/config"
	"github.com/a-novel-kit/golib/servicetoken"
)

// ClientConfig locates a gRPC service a process calls, and says how to dial it.
// [LoadClientConfig] reads it from the variables of a prefix.
type ClientConfig struct {
	// Host is the service's host name, without scheme or port.
	Host string `json:"host" yaml:"host"`
	// Port is the service's gRPC port.
	Port int `json:"port" yaml:"port"`
	// TLS dials TLS 1.3 once TokenPath is set, and verifies the server's certificate for Host against
	// the system roots, which SSL_CERT_DIR extends.
	TLS bool `json:"tls" yaml:"tls"`
	// TokenPath is the file the kubelet projects the caller's service token into, for the callee's
	// audience. Empty dials a local target, in plaintext and without a token.
	TokenPath string `json:"tokenPath" yaml:"tokenPath"`
}

// LoadClientConfig reads the variables of prefix through getenv, which applies the service's
// prefix, and keeps defaults for unset ones. For the prefix SERVICE_JSON_KEYS, it reads
// SERVICE_JSON_KEYS_HOST, SERVICE_JSON_KEYS_PORT, SERVICE_JSON_KEYS_TLS and
// SERVICE_JSON_KEYS_TOKEN_PATH. It panics on a value it cannot parse.
func LoadClientConfig(getenv func(name string) string, prefix string, defaults ClientConfig) ClientConfig {
	return ClientConfig{
		Host:      cmp.Or(getenv(prefix+"_HOST"), defaults.Host),
		Port:      config.LoadEnv(getenv(prefix+"_PORT"), defaults.Port, strconv.Atoi),
		TLS:       config.LoadEnv(getenv(prefix+"_TLS"), defaults.TLS, strconv.ParseBool),
		TokenPath: cmp.Or(getenv(prefix+"_TOKEN_PATH"), defaults.TokenPath),
	}
}

// Address returns the host:port grpc.NewClient dials.
func (client ClientConfig) Address() string {
	return net.JoinHostPort(client.Host, strconv.Itoa(client.Port))
}

// DialOptions returns the options grpc.NewClient dials the service with. Without a TokenPath, they
// dial a local target in plaintext. With one, they dial a service in the same Kubernetes cluster, over
// TLS once TLS is set, and send the caller's service token with every call, which the callee's
// [ServiceTokenGate] checks. Each call reads the file again, so a rotated token takes effect on the
// next call.
//
// The connection then sends each call to the next address the target resolves to, as a headless
// Service resolves to its ready pods. A call refused with Unavailable is retried on the next address,
// up to 4 attempts in all, and a throttle pauses retries while many calls fail.
//
// The retry rests on one rule for servers: Unavailable means the call did not run. [CallLimit] and a
// planned downtime refuse that way, before the handler. A handler that has had an effect answers
// another code.
func (client ClientConfig) DialOptions() []grpc.DialOption {
	if client.TokenPath == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}

	transport := insecure.NewCredentials()
	if client.TLS {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13})
	}

	return []grpc.DialOption{
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(serviceTokenCredentials(client.TokenPath)),
		grpc.WithDefaultServiceConfig(inClusterServiceConfig),
	}
}

// CredentialsProvider supplies the gRPC dial options of a connection.
//
// Prefer [ClientConfig.DialOptions], which [LoadClientConfig] configures from the environment. A
// later release removes the interface once no consumer uses it.
type CredentialsProvider interface {
	// Options returns the dial options to pass when creating the client.
	Options(ctx context.Context) ([]grpc.DialOption, error)
}

// LocalCredentialsProvider dials without transport security, for plaintext connections to a service
// on a trusted local network.
//
// Prefer the [ClientConfig.DialOptions] of a ClientConfig without a TokenPath.
// A later release removes it once no consumer uses it.
type LocalCredentialsProvider struct{}

// Options returns the dial options of a ClientConfig without a TokenPath.
func (provider *LocalCredentialsProvider) Options(_ context.Context) ([]grpc.DialOption, error) {
	return ClientConfig{}.DialOptions(), nil
}

// ServiceTokenCredentialsProvider dials a service in the same Kubernetes cluster with the caller's
// service token, as [ClientConfig.DialOptions] describes.
//
// Prefer the [ClientConfig.DialOptions] of a ClientConfig with a TokenPath.
// A later release removes it once no consumer uses it.
type ServiceTokenCredentialsProvider struct {
	// ServiceTokenPath is the file the kubelet projects the caller's service token into.
	ServiceTokenPath string
	// TLS dials TLS 1.3. False dials plaintext.
	TLS bool
}

// Options returns the dial options of a ClientConfig with this token path and TLS flag.
func (provider *ServiceTokenCredentialsProvider) Options(_ context.Context) ([]grpc.DialOption, error) {
	return ClientConfig{TokenPath: provider.ServiceTokenPath, TLS: provider.TLS}.DialOptions(), nil
}

// inClusterServiceConfig balances the calls of a connection with a service token round robin, and
// retries every method on Unavailable. The retry throttle holds 10 tokens. Each refused attempt
// spends one and each success earns a tenth back, and retries stop while 5 or fewer remain.
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
