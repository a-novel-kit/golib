package sentryf

import "github.com/getsentry/sentry-go"

// Config configures the reporter that [Init] installs. [LoadConfig] reads it from the standard
// SENTRY_* environment variables.
type Config struct {
	// DSN is the client key of the service's Sentry project, which also names the ingest host.
	// Without one, the reporter sends nothing.
	DSN string `json:"dsn" yaml:"dsn"`
	// Release names the deployed build as <service>@<semver>, such as
	// service-authentication@2.17.0. Sentry resolves an issue "in the next release" by it.
	Release string `json:"release" yaml:"release"`
	// Environment names the deployment, such as production.
	Environment string `json:"environment" yaml:"environment"`
	// Transport delivers the events. Nil sends them to the host the DSN names.
	Transport sentry.Transport `json:"-" yaml:"-"`
}

// LoadConfig reads SENTRY_DSN, SENTRY_RELEASE and SENTRY_ENVIRONMENT through getenv, which applies
// the service's prefix.
func LoadConfig(getenv func(name string) string) Config {
	return Config{
		DSN:         getenv("SENTRY_DSN"),
		Release:     getenv("SENTRY_RELEASE"),
		Environment: getenv("SENTRY_ENVIRONMENT"),
	}
}
