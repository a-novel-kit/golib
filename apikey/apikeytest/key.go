package apikeytest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
	"time"

	"github.com/a-novel-kit/jwt/v2"
	"github.com/a-novel-kit/jwt/v2/jwa"
	"github.com/a-novel-kit/jwt/v2/jws"

	"github.com/a-novel-kit/golib/apikey"
)

// A Key is an API key made for a test. Its private key lives in memory only, so the request tokens
// it signs die with the process.
type Key struct {
	// PublicKey is the key's public key in PEM, as apikey.Config holds it.
	PublicKey string

	producer *jwt.Producer
}

// NewKey generates an API key.
func NewKey() *Key {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		panic(err)
	}

	return &Key{
		PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})),
		producer: jwt.NewProducer(jwt.ProducerConfig{
			Header: jwt.HeaderProducerConfig{Typ: jwa.TypJWT},
			// A request token is labeled "EdDSA", the name its format gives the algorithm.
			Plugins: []jwt.ProducerPlugin{jws.NewEdDSASigner(privateKey)}, //nolint:staticcheck // See above.
		}),
	}
}

// Claims returns the claims of a request token signed now for purpose and body, which expires after
// [apikey.MaxLifetime]. A test changes them to build an invalid token.
func (key *Key) Claims(purpose string, body []byte) *apikey.Claims {
	now := time.Now()
	digest := sha256.Sum256(body)

	return &apikey.Claims{
		Aud:        purpose,
		Iat:        now.Unix(),
		Exp:        now.Add(apikey.MaxLifetime).Unix(),
		BodySHA256: hex.EncodeToString(digest[:]),
	}
}

// Sign returns claims as a request token signed with the key. Claims are an [apikey.Claims], or any
// value that encodes as the claims a test needs.
func (key *Key) Sign(t *testing.T, claims any) string {
	t.Helper()

	token, err := key.producer.Issue(t.Context(), claims, nil)
	if err != nil {
		panic(err)
	}

	return token
}
