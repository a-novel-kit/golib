package servicetoken

import (
	"errors"
	"strconv"

	"github.com/a-novel-kit/golib/config"
)

// ErrIssuerRequired refuses a gate configured with no issuer and without [Config.IssuerOptional]:
// it would admit every caller.
var ErrIssuerRequired = errors.New("service token issuer required")

// LoadConfig reads the service token variables through getenv, which applies the service's prefix:
//
//   - SERVICE_TOKEN_ISSUER, the issuer's URL;
//   - SERVICE_TOKEN_AUDIENCE, the audience the server answers to;
//   - SERVICE_TOKEN_CALLERS, the allowed callers, comma-separated;
//   - SERVICE_ISSUER_OPTIONAL, true to start a local or test stack without an issuer.
//
// It panics on a value it cannot parse.
func LoadConfig(getenv func(name string) string) Config {
	return Config{
		Issuer:         getenv("SERVICE_TOKEN_ISSUER"),
		Audience:       getenv("SERVICE_TOKEN_AUDIENCE"),
		Callers:        config.LoadEnv(getenv("SERVICE_TOKEN_CALLERS"), nil, config.SliceParser(config.StringParser)),
		IssuerOptional: config.LoadEnv(getenv("SERVICE_ISSUER_OPTIONAL"), false, strconv.ParseBool),
	}
}
