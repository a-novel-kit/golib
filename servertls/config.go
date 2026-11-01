package servertls

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// Files names the PEM files of the key pair a server presents: its certificate chain, leaf first,
// and the leaf's private key. Its zero value names none, and the server then serves plaintext.
type Files struct {
	CertFile string `json:"certFile" yaml:"certFile"`
	KeyFile  string `json:"keyFile"  yaml:"keyFile"`
}

// Config returns a TLS 1.3 server configuration presenting the key pair files names, or an error
// when the pair does not load.
//
// The configuration loads the pair again at the first handshake after the certificate file changes,
// as it does when the kubelet updates a mounted Secret. A pair that fails to load leaves the previous
// one in service, and the next handshake tries again.
func Config(files Files) (*tls.Config, error) {
	pair := &keyPair{files: files}

	err := pair.reload()
	if err != nil {
		return nil, err
	}

	return &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: pair.get}, nil
}

// keyPair holds the key pair loaded last, which handshakes read concurrently.
type keyPair struct {
	files   Files
	current atomic.Pointer[loadedKeyPair]
}

// loadedKeyPair is a key pair with the modification time of the certificate file it came from.
type loadedKeyPair struct {
	modTime     time.Time
	certificate *tls.Certificate
}

// get returns the key pair for a handshake, loaded again first when its certificate file changed.
func (pair *keyPair) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	// A failed reload keeps the previous pair in service, and the next handshake tries again.
	_ = pair.reload()

	return pair.current.Load().certificate, nil
}

// reload loads the key pair when its certificate file changed since the last load. Handshakes that
// see the same change at once may each load it, and the last one stays.
func (pair *keyPair) reload() error {
	info, err := os.Stat(pair.files.CertFile)
	if err != nil {
		return fmt.Errorf("read TLS certificate: %w", err)
	}

	current := pair.current.Load()
	if current != nil && info.ModTime().Equal(current.modTime) {
		return nil
	}

	certificate, err := tls.LoadX509KeyPair(pair.files.CertFile, pair.files.KeyFile)
	if err != nil {
		return fmt.Errorf("load TLS key pair: %w", err)
	}

	pair.current.Store(&loadedKeyPair{modTime: info.ModTime(), certificate: &certificate})

	return nil
}
