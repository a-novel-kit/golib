package postgrespresets_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"

	postgrespresets "github.com/a-novel-kit/golib/postgres/presets"
)

func TestConnection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		connection postgrespresets.Connection

		expectAddr        string
		expectUser        string
		expectPassword    string
		expectDatabase    string
		expectTLSEnabled  bool
		expectDialTimeout time.Duration
		expectErr         error
		// expectConnectorPanic expects the driver to refuse the options, instead of connecting anywhere.
		expectConnectorPanic bool
	}{
		{
			name: "Success/Discrete",
			connection: postgrespresets.Connection{
				DSN:         "postgres://unused@unused:1/unused",
				Host:        "postgres.internal",
				Port:        5433,
				User:        "service",
				Password:    "password-with-:/@%",
				Database:    "service-db",
				DialTimeout: 3 * time.Minute,
			},
			expectAddr:        "postgres.internal:5433",
			expectUser:        "service",
			expectPassword:    "password-with-:/@%",
			expectDatabase:    "service-db",
			expectDialTimeout: 3 * time.Minute,
		},
		{
			name: "Success/DiscreteTLS",
			connection: postgrespresets.Connection{
				Host:       "2001:db8::1",
				Port:       5432,
				User:       "service",
				Password:   "password",
				Database:   "service-db",
				TLSEnabled: true,
			},
			expectAddr:        "[2001:db8::1]:5432",
			expectUser:        "service",
			expectPassword:    "password",
			expectDatabase:    "service-db",
			expectTLSEnabled:  true,
			expectDialTimeout: 5 * time.Second,
		},
		{
			name: "Success/DSN",
			connection: postgrespresets.Connection{
				DSN:         "postgres://dsn-user@dsn.internal:6432/dsn-db?sslmode=disable",
				DialTimeout: time.Minute,
			},
			expectAddr:        "dsn.internal:6432",
			expectUser:        "dsn-user",
			expectDatabase:    "dsn-db",
			expectDialTimeout: time.Minute,
		},
		{
			name: "Error/MissingPassword",
			connection: postgrespresets.Connection{
				DSN:      "postgres://dsn-user@dsn.internal:6432/dsn-db?sslmode=disable",
				Host:     "postgres.internal",
				Port:     5432,
				User:     "service",
				Database: "service-db",
			},
			expectErr: postgrespresets.ErrConnectionPassword,
		},
		{
			// An unset host never reaches the driver's default server: without a DSN either, the driver
			// refuses to build a connector.
			name: "Error/MissingHost",
			connection: postgrespresets.Connection{
				Port:     5432,
				User:     "service",
				Password: "password",
				Database: "service-db",
			},
			expectConnectorPanic: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options, err := testCase.connection.Options()
			require.ErrorIs(t, err, testCase.expectErr)

			if testCase.expectErr != nil {
				return
			}

			if testCase.expectConnectorPanic {
				require.Panics(t, func() { pgdriver.NewConnector(options...) })

				return
			}

			driverConfig := pgdriver.NewConnector(options...).Config()

			require.Equal(t, testCase.expectAddr, driverConfig.Addr)
			require.Equal(t, testCase.expectUser, driverConfig.User)
			require.Equal(t, testCase.expectPassword, driverConfig.Password)
			require.Equal(t, testCase.expectDatabase, driverConfig.Database)
			require.Equal(t, testCase.expectTLSEnabled, driverConfig.TLSConfig != nil)
			require.Equal(t, testCase.expectDialTimeout, driverConfig.DialTimeout)
		})
	}
}

func TestLoadConnection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		env map[string]string

		expect postgrespresets.Connection
	}{
		{
			name: "Success/Defaults",

			expect: postgrespresets.ConnectionDefault,
		},
		{
			name: "Success/Set",

			env: map[string]string{
				"POSTGRES_HOST":           "postgres.internal",
				"POSTGRES_PORT":           "5433",
				"POSTGRES_USER":           "service",
				"POSTGRES_PASSWORD":       "secret",
				"POSTGRES_DATABASE":       "service-db",
				"POSTGRES_TLS_ENABLED":    "false",
				"POSTGRES_MAX_OPEN_CONNS": "8",
				"POSTGRES_MAX_IDLE_CONNS": "4",
			},

			expect: postgrespresets.Connection{
				Host:         "postgres.internal",
				Port:         5433,
				User:         "service",
				Password:     "secret",
				Database:     "service-db",
				MaxOpenConns: 8,
				MaxIdleConns: 4,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(name string) string { return testCase.env[name] }
			require.Equal(t, testCase.expect, postgrespresets.LoadConnection(getenv, postgrespresets.ConnectionDefault))
		})
	}

	t.Run("Error/Unparsable", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() {
			postgrespresets.LoadConnection(func(string) string { return "many" }, postgrespresets.ConnectionDefault)
		})
	})
}

func TestConnectionPreset(t *testing.T) {
	t.Parallel()

	connection := postgrespresets.Connection{
		Host:         "postgres.internal",
		Port:         5432,
		User:         "service",
		Password:     "secret",
		Database:     "service-db",
		MaxOpenConns: 8,
		MaxIdleConns: 4,
	}

	preset, err := connection.Preset(pgdriver.WithAddr("replica.internal:6543"))
	require.NoError(t, err)
	require.Equal(t, 8, preset.MaxOpenConns)
	require.Equal(t, 4, preset.MaxIdleConns)

	// The extra options follow the connection's own, so they win: the connection sets the address
	// first.
	require.Equal(t, "replica.internal:6543", pgdriver.NewConnector(preset.Options()...).Config().Addr)

	_, err = postgrespresets.Connection{Host: "postgres.internal"}.Preset()
	require.ErrorIs(t, err, postgrespresets.ErrConnectionPassword)

	// A service that sets no POSTGRES_HOST never reaches the driver's default server.
	unsetHost := postgrespresets.LoadConnection(func(string) string { return "" }, postgrespresets.ConnectionDefault)

	unset, err := unsetHost.Preset()
	require.NoError(t, err)
	require.Panics(t, func() { pgdriver.NewConnector(unset.Options()...) })
}
