package grpcf

import (
	"time"

	"github.com/a-novel-kit/golib/config"
)

// ServerConfig configures a gRPC server. [LoadServerConfig] reads it from the GRPC_* environment
// variables.
type ServerConfig struct {
	// Port is the port the server listens on.
	Port int `json:"port" yaml:"port"`
	// Shutdown bounds the drain of active calls before remaining ones stop.
	Shutdown time.Duration `json:"shutdown" yaml:"shutdown"`
}

// ServerConfigDefault holds the values [LoadServerConfig] keeps for unset variables. A service
// overrides a field on a copy before loading.
var ServerConfigDefault = ServerConfig{
	Port:     8080,
	Shutdown: 30 * time.Second,
}

// LoadServerConfig reads the GRPC_* variables through getenv, which applies the service's prefix,
// and keeps defaults for unset ones. It panics on a value it cannot parse.
func LoadServerConfig(getenv func(name string) string, defaults ServerConfig) ServerConfig {
	return ServerConfig{
		Port:     config.LoadEnv(getenv("GRPC_PORT"), defaults.Port, config.IntParser),
		Shutdown: config.LoadEnv(getenv("GRPC_TIMEOUT_SHUTDOWN"), defaults.Shutdown, config.DurationParser),
	}
}
