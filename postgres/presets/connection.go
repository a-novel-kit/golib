package postgrespresets

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
)

// ErrConnectionPassword is returned by [Connection.Options] when the discrete fields are
// selected without a password.
var ErrConnectionPassword = errors.New("postgres password is empty")

// A Connection describes how to reach PostgreSQL, for [NewDefault]. A non-empty Host selects
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
