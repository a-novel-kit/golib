package grpcf

import (
	"cmp"
	"strconv"
	"time"

	"github.com/a-novel-kit/golib/config"
	"github.com/a-novel-kit/golib/servertls"
)

// ServerConfig configures a gRPC server. [LoadServerConfig] reads it from the GRPC_* environment
// variables.
type ServerConfig struct {
	// Port is the port the server listens on.
	Port int `json:"port" yaml:"port"`
	// TLS names the key pair the server presents. Once it does, [ListenAndServe] serves TLS on Port.
	TLS servertls.Files `json:"tls" yaml:"tls"`
	// PlaintextPort, when not zero, also serves plaintext there, so callers move to a TLS Port
	// without a failed call.
	PlaintextPort int `json:"plaintextPort" yaml:"plaintextPort"`
	// Shutdown bounds the drain of active calls before remaining ones stop. The default of 6 seconds
	// leaves room for the OpenTelemetry flush before Kubernetes kills the pod at 30.
	Shutdown time.Duration `json:"shutdown" yaml:"shutdown"`
	// MaxConcurrentCalls bounds the calls the server runs at once, once passed to [NewCallLimit].
	// Zero bounds nothing.
	MaxConcurrentCalls int `json:"maxConcurrentCalls" yaml:"maxConcurrentCalls"`
}

// ServerConfigDefault holds the values [LoadServerConfig] keeps for unset variables. A service
// overrides a field on a copy before loading.
var ServerConfigDefault = ServerConfig{
	Port:     8080,
	Shutdown: 6 * time.Second,
}

// LoadServerConfig reads the GRPC_* variables through getenv, which applies the service's prefix,
// and keeps defaults for unset ones. It panics on a value it cannot parse.
func LoadServerConfig(getenv func(name string) string, defaults ServerConfig) ServerConfig {
	return ServerConfig{
		Port: config.LoadEnv(getenv("GRPC_PORT"), defaults.Port, strconv.Atoi),
		TLS: servertls.Files{
			CertFile: cmp.Or(getenv("GRPC_TLS_CERT_FILE"), defaults.TLS.CertFile),
			KeyFile:  cmp.Or(getenv("GRPC_TLS_KEY_FILE"), defaults.TLS.KeyFile),
		},
		PlaintextPort: config.LoadEnv(getenv("GRPC_PLAINTEXT_PORT"), defaults.PlaintextPort, strconv.Atoi),
		Shutdown:      config.LoadEnv(getenv("GRPC_TIMEOUT_SHUTDOWN"), defaults.Shutdown, time.ParseDuration),
		MaxConcurrentCalls: config.LoadEnv(
			getenv("GRPC_MAX_CONCURRENT_CALLS"), defaults.MaxConcurrentCalls, strconv.Atoi,
		),
	}
}
