// Package tlstest signs the certificates golib's TLS tests serve, as cert-manager's CA issuer does in
// a cluster.
package tlstest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a-novel-kit/golib/servertls"
)

// Authority signs server certificates for 127.0.0.1 and localhost.
type Authority struct {
	// Pool holds the authority's certificate, for a client's root CAs.
	Pool *x509.CertPool

	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
}

// NewAuthority returns an authority with a key and a self-signed certificate of its own.
func NewAuthority(t *testing.T) *Authority {
	t.Helper()

	key := newKey()
	template := &x509.Certificate{
		SerialNumber:          newSerialNumber(),
		Subject:               pkix.Name{CommonName: "golib test authority"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	certificate := parseCertificate(sign(template, template, key, key))

	pool := x509.NewCertPool()
	pool.AddCert(certificate)

	return &Authority{Pool: pool, certificate: certificate, key: key}
}

// WriteCertificate writes the authority's certificate in dir as ca.crt, so a client that reads
// SSL_CERT_DIR trusts it.
func (authority *Authority) WriteCertificate(t *testing.T, dir string) {
	t.Helper()

	writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", authority.certificate.Raw)
}

// WriteKeyPair signs a certificate with a serial number of its own, and writes it with its key in dir
// as tls.crt and tls.key, the names cert-manager gives them. An earlier pair in dir is replaced.
func (authority *Authority) WriteKeyPair(t *testing.T, dir string) (servertls.Files, *x509.Certificate) {
	t.Helper()

	key := newKey()
	template := &x509.Certificate{
		SerialNumber: newSerialNumber(),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der := sign(template, authority.certificate, key, authority.key)

	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}

	files := servertls.Files{CertFile: filepath.Join(dir, "tls.crt"), KeyFile: filepath.Join(dir, "tls.key")}
	writePEM(files.KeyFile, "PRIVATE KEY", privateKey)
	writePEM(files.CertFile, "CERTIFICATE", der)

	return files, parseCertificate(der)
}

func newKey() *ecdsa.PrivateKey {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	return key
}

func newSerialNumber() *big.Int {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}

	return serialNumber
}

func sign(template, parent *x509.Certificate, key, parentKey *ecdsa.PrivateKey) []byte {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		panic(err)
	}

	return der
}

func parseCertificate(der []byte) *x509.Certificate {
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}

	return certificate
}

func writePEM(path, blockType string, der []byte) {
	err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600)
	if err != nil {
		panic(err)
	}
}
