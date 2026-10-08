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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options, err := testCase.connection.Options()
			require.ErrorIs(t, err, testCase.expectErr)

			if testCase.expectErr != nil {
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
