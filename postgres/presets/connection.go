package postgrespresets

import (
	"cmp"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/a-novel-kit/golib/config"
)

// ErrConnectionPassword is returned by [Connection.Options] when the discrete fields are
// selected without a password.
var ErrConnectionPassword = errors.New("postgres password is empty")

// A Connection describes how to reach PostgreSQL, and the pool [Connection.Preset] opens there.
// [LoadConnection] reads it from the environment. A non-empty Host selects
// the discrete fields, so an orchestrator can inject the password from a secret store without
// assembling a credential-bearing URL. An empty Host falls back to DSN.
type Connection struct {
	// DSN is the connection URL, read only when Host is empty.
	DSN string

	// Host is the server hostname or IP address.
	Host string
	// Port is the server port.
	Port int
	// User is the login role.
	User string
	// Password is the login credential. It is required when Host is set.
	Password string
	// Database is the database name.
	Database string
	// TLSEnabled encrypts the connection. Disable it only when another trusted boundary
	// protects the database link.
	TLSEnabled bool

	// DialTimeout bounds how long establishing one connection may take, whichever fields
	// select the server. Zero keeps the driver default.
	DialTimeout time.Duration

	// MaxOpenConns bounds the connections the pool of [Connection.Preset] opens. Zero leaves them
	// unbounded.
	MaxOpenConns int
	// MaxIdleConns caps the connections that pool keeps idle. Zero keeps [MaxIdleConnsDefault].
	MaxIdleConns int
}

// ConnectionDefault holds the values [LoadConnection] keeps for unset variables: PostgreSQL's
// standard port, an encrypted connection, and a pool of 20 connections, open and idle. Multiplied by
// a service's replica count, 20 stays under PostgreSQL's default max_connections of 100, with room for
// the migrations job and a psql session. A service overrides a field on a copy before loading.
var ConnectionDefault = Connection{Port: 5432, TLSEnabled: true, MaxOpenConns: 20, MaxIdleConns: 20}

// LoadConnection reads the POSTGRES_* variables through getenv, which applies the service's prefix,
// and keeps defaults for unset ones: POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD,
// POSTGRES_DATABASE, POSTGRES_TLS_ENABLED, POSTGRES_MAX_OPEN_CONNS and POSTGRES_MAX_IDLE_CONNS. No
// variable fills DSN or DialTimeout, which keep their defaults. It panics on a value it cannot parse.
func LoadConnection(getenv func(name string) string, defaults Connection) Connection {
	return Connection{
		DSN:          defaults.DSN,
		Host:         cmp.Or(getenv("POSTGRES_HOST"), defaults.Host),
		Port:         config.LoadEnv(getenv("POSTGRES_PORT"), defaults.Port, strconv.Atoi),
		User:         cmp.Or(getenv("POSTGRES_USER"), defaults.User),
		Password:     cmp.Or(getenv("POSTGRES_PASSWORD"), defaults.Password),
		Database:     cmp.Or(getenv("POSTGRES_DATABASE"), defaults.Database),
		TLSEnabled:   config.LoadEnv(getenv("POSTGRES_TLS_ENABLED"), defaults.TLSEnabled, strconv.ParseBool),
		DialTimeout:  defaults.DialTimeout,
		MaxOpenConns: config.LoadEnv(getenv("POSTGRES_MAX_OPEN_CONNS"), defaults.MaxOpenConns, strconv.Atoi),
		MaxIdleConns: config.LoadEnv(getenv("POSTGRES_MAX_IDLE_CONNS"), defaults.MaxIdleConns, strconv.Atoi),
	}
}

// Preset returns a [Default] that connects as described, with its pool bounded by MaxOpenConns and
// MaxIdleConns. Extra options apply last, such as the session parameters of a migrations job.
func (connection Connection) Preset(extra ...pgdriver.Option) (*Default, error) {
	options, err := connection.Options()
	if err != nil {
		return nil, err
	}

	preset := NewDefault(append(options, extra...)...)
	preset.MaxOpenConns = connection.MaxOpenConns
	preset.MaxIdleConns = connection.MaxIdleConns

	return preset, nil
}

// Options returns the driver options that connect as described, ready for [NewDefault].
func (connection Connection) Options() ([]pgdriver.Option, error) {
	var options []pgdriver.Option

	if connection.Host == "" {
		options = []pgdriver.Option{pgdriver.WithDSN(connection.DSN)}
	} else {
		if connection.Password == "" {
			return nil, ErrConnectionPassword
		}

		options = []pgdriver.Option{
			pgdriver.WithAddr(net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))),
			pgdriver.WithUser(connection.User),
			pgdriver.WithPassword(connection.Password),
			pgdriver.WithDatabase(connection.Database),
			pgdriver.WithInsecure(!connection.TLSEnabled),
		}
	}

	if connection.DialTimeout > 0 {
		options = append(options, pgdriver.WithDialTimeout(connection.DialTimeout))
	}

	return options, nil
}
