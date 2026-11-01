package servertls_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/internal/tlstest"
	"github.com/a-novel-kit/golib/servertls"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	authority := tlstest.NewAuthority(t)

	testCases := []struct {
		name string

		// files replaces the key pair files the server loads.
		files func(t *testing.T, files servertls.Files) servertls.Files
		// change alters the files once the server has loaded them, and returns the certificate the next
		// handshake presents, or nil for the one loaded first.
		change        func(t *testing.T, files servertls.Files) *x509.Certificate
		clientVersion uint16

		expectErr          bool
		expectHandshakeErr bool
	}{
		{
			name: "Success",
		},
		{
			name: "Success/Renewal",

			change: func(t *testing.T, files servertls.Files) *x509.Certificate {
				t.Helper()

				_, renewed := authority.WriteKeyPair(t, filepath.Dir(files.CertFile))

				return renewed
			},
		},
		{
			name: "Success/FailedReloadKeepsPair",

			change: func(t *testing.T, files servertls.Files) *x509.Certificate {
				t.Helper()

				err := os.WriteFile(files.CertFile, []byte("not a certificate"), 0o600)
				if err != nil {
					panic(err)
				}

				return nil
			},
		},
		{
			name: "Error/MissingCertificate",

			files: func(_ *testing.T, files servertls.Files) servertls.Files {
				files.CertFile += ".missing"

				return files
			},

			expectErr: true,
		},
		{
			name: "Error/MismatchedKey",

			files: func(t *testing.T, files servertls.Files) servertls.Files {
				t.Helper()

				other, _ := authority.WriteKeyPair(t, t.TempDir())
				files.KeyFile = other.KeyFile

				return files
			},

			expectErr: true,
		},
		{
			name: "Error/TLS12Client",

			clientVersion: tls.VersionTLS12,

			expectHandshakeErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, presented := authority.WriteKeyPair(t, t.TempDir())
			if testCase.files != nil {
				files = testCase.files(t, files)
			}

			config, err := servertls.Config(files)
			if testCase.expectErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			if testCase.change != nil {
				renewed := testCase.change(t, files)
				if renewed != nil {
					presented = renewed
				}
			}

			client := &tls.Config{
				RootCAs:    authority.Pool,
				ServerName: "localhost",
				MinVersion: tls.VersionTLS12,
				MaxVersion: testCase.clientVersion,
			}

			certificate, err := handshake(t, config, client)
			if testCase.expectHandshakeErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, presented.SerialNumber, certificate.SerialNumber)
		})
	}
}

// handshake runs a TLS handshake between server and client over an in-memory connection, and
// returns the certificate the server presented.
func handshake(t *testing.T, server, client *tls.Config) (*x509.Certificate, error) {
	t.Helper()

	serverConnection, clientConnection := net.Pipe()

	go func() {
		_ = tls.Server(serverConnection, server).HandshakeContext(t.Context())
		_ = serverConnection.Close()
	}()

	connection := tls.Client(clientConnection, client)
	defer func() { _ = connection.Close() }()

	err := connection.HandshakeContext(t.Context())
	if err != nil {
		return nil, err
	}

	return connection.ConnectionState().PeerCertificates[0], nil
}
