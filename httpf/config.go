package httpf

import (
	"time"

	"github.com/a-novel-kit/golib/config"
)

// ServerConfig configures a REST server: its port, request lifecycle, body and rate limits, and
// CORS. [LoadServerConfig] reads it from the REST_* environment variables.
type ServerConfig struct {
	// Port is the port the server listens on.
	Port int `json:"port" yaml:"port"`
	// Timeouts bound each phase of a request.
	Timeouts ServerTimeouts `json:"timeouts" yaml:"timeouts"`
	// MaxRequestSize caps a request body, in bytes. chi cannot raise the router's limit for one
	// route, so a route that needs more must sit outside it.
	MaxRequestSize int64 `json:"maxRequestSize" yaml:"maxRequestSize"`
	// ClientIPHeader names the header a trusted proxy sets to the visitor IP. Empty keeps the TCP
	// peer address. Name one only when every request reaches the server through that proxy.
	ClientIPHeader string `json:"clientIPHeader" yaml:"clientIPHeader"`
	// RateLimitsEnabled turns the rate limits on. Only test stacks turn them off.
	RateLimitsEnabled bool `json:"rateLimitsEnabled" yaml:"rateLimitsEnabled"`
	// Cors configures cross-origin requests.
	Cors CorsConfig `json:"cors" yaml:"cors"`
}

// ServerTimeouts bound each phase of a request.
type ServerTimeouts struct {
	Read       time.Duration `json:"read"       yaml:"read"`
	ReadHeader time.Duration `json:"readHeader" yaml:"readHeader"`
	Write      time.Duration `json:"write"      yaml:"write"`
	Idle       time.Duration `json:"idle"       yaml:"idle"`
	// Request bounds handling one request, after its headers are read.
	Request time.Duration `json:"request" yaml:"request"`
	// Shutdown bounds the drain of active requests before remaining connections close.
	Shutdown time.Duration `json:"shutdown" yaml:"shutdown"`
}

// CorsConfig configures cross-origin requests. The methods a router allows are the service's to
// choose, in [RouterOptions].
type CorsConfig struct {
	AllowedOrigins   []string `json:"allowedOrigins"   yaml:"allowedOrigins"`
	AllowedHeaders   []string `json:"allowedHeaders"   yaml:"allowedHeaders"`
	AllowCredentials bool     `json:"allowCredentials" yaml:"allowCredentials"`
	// MaxAge is how long a browser caches a preflight answer, in seconds.
	MaxAge int `json:"maxAge" yaml:"maxAge"`
}

// ServerConfigDefault holds the values [LoadServerConfig] keeps for unset variables. A service
// overrides a field on a copy before loading.
var ServerConfigDefault = ServerConfig{
	Port: 8080,
	Timeouts: ServerTimeouts{
		Read:       15 * time.Second,
		ReadHeader: 3 * time.Second,
		Write:      30 * time.Second,
		Idle:       60 * time.Second,
		Request:    60 * time.Second,
		Shutdown:   30 * time.Second,
	},
	MaxRequestSize:    16 << 10,
	RateLimitsEnabled: true,
	Cors: CorsConfig{
		AllowedOrigins: []string{"*"},
		AllowedHeaders: []string{"*"},
		MaxAge:         3600,
	},
}

// LoadServerConfig reads the REST_* variables through getenv, which applies the service's prefix,
// and keeps defaults for unset ones. It panics on a value it cannot parse.
func LoadServerConfig(getenv func(name string) string, defaults ServerConfig) ServerConfig {
	stringSlice := config.SliceParser(config.StringParser)

	return ServerConfig{
		Port: config.LoadEnv(getenv("REST_PORT"), defaults.Port, config.IntParser),
		Timeouts: ServerTimeouts{
			Read:       config.LoadEnv(getenv("REST_TIMEOUT_READ"), defaults.Timeouts.Read, config.DurationParser),
			ReadHeader: config.LoadEnv(getenv("REST_TIMEOUT_READ_HEADER"), defaults.Timeouts.ReadHeader, config.DurationParser),
			Write:      config.LoadEnv(getenv("REST_TIMEOUT_WRITE"), defaults.Timeouts.Write, config.DurationParser),
			Idle:       config.LoadEnv(getenv("REST_TIMEOUT_IDLE"), defaults.Timeouts.Idle, config.DurationParser),
			Request:    config.LoadEnv(getenv("REST_TIMEOUT_REQUEST"), defaults.Timeouts.Request, config.DurationParser),
			Shutdown:   config.LoadEnv(getenv("REST_TIMEOUT_SHUTDOWN"), defaults.Timeouts.Shutdown, config.DurationParser),
		},
		MaxRequestSize:    config.LoadEnv(getenv("REST_MAX_REQUEST_SIZE"), defaults.MaxRequestSize, config.Int64Parser),
		ClientIPHeader:    config.LoadEnv(getenv("REST_CLIENT_IP_HEADER"), defaults.ClientIPHeader, config.StringParser),
		RateLimitsEnabled: config.LoadEnv(getenv("REST_RATE_LIMITS_ENABLED"), defaults.RateLimitsEnabled, config.BoolParser),
		Cors: CorsConfig{
			AllowedOrigins: config.LoadEnv(getenv("REST_CORS_ALLOWED_ORIGINS"), defaults.Cors.AllowedOrigins, stringSlice),
			AllowedHeaders: config.LoadEnv(getenv("REST_CORS_ALLOWED_HEADERS"), defaults.Cors.AllowedHeaders, stringSlice),
			AllowCredentials: config.LoadEnv(
				getenv("REST_CORS_ALLOW_CREDENTIALS"), defaults.Cors.AllowCredentials, config.BoolParser,
			),
			MaxAge: config.LoadEnv(getenv("REST_CORS_MAX_AGE"), defaults.Cors.MaxAge, config.IntParser),
		},
	}
}
