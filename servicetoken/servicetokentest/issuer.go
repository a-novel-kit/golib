package servicetokentest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a-novel-kit/jwt/v2"
	"github.com/a-novel-kit/jwt/v2/jwa"
	"github.com/a-novel-kit/jwt/v2/jwk"
	"github.com/a-novel-kit/jwt/v2/jws"
)

// An Issuer publishes its key as a GKE cluster does, and signs service tokens with it.
type Issuer struct {
	// URL is the issuer, as servicetoken.Config and a token's "iss" claim name it.
	URL string

	producer *jwt.Producer
}

// NewIssuer starts an Issuer with a new RS256 key. The test's cleanup stops it.
func NewIssuer(t *testing.T) *Issuer {
	t.Helper()

	privateKey, publicKey, err := jwk.GenerateRSA(jwk.RS256)
	if err != nil {
		panic(err)
	}

	// A GKE key set marks its keys with "use" alone.
	published := *publicKey.JWK
	published.KeyOps = nil

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.Handle("GET /.well-known/openid-configuration", serveJSON(map[string]string{
		"issuer":   server.URL,
		"jwks_uri": server.URL + "/jwks",
	}))
	mux.Handle("GET /jwks", serveJSON(map[string][]jwa.JWK{"keys": {published}}))

	return &Issuer{
		URL: server.URL,
		producer: jwt.NewProducer(jwt.ProducerConfig{
			StaticPlugins: []jwt.ProducerStaticPlugin{keyID(publicKey.KID)},
			Plugins:       []jwt.ProducerPlugin{jws.NewRSASigner(privateKey.Key(), jws.RS256)},
		}),
	}
}

// Claims returns the claims of a token that subject holds for audience, valid for an hour from now.
// A test changes them to build an invalid token.
func (issuer *Issuer) Claims(subject, audience string) *jwa.Claims {
	now := time.Now()

	return &jwa.Claims{ClaimsCommon: jwa.ClaimsCommon{
		Iss: issuer.URL,
		Sub: subject,
		Aud: jwa.Audience{audience},
		Exp: now.Add(time.Hour).Unix(),
		Nbf: now.Unix(),
		Iat: now.Unix(),
	}}
}

// Sign returns claims as a token signed with the issuer's key.
func (issuer *Issuer) Sign(t *testing.T, claims *jwa.Claims) string {
	t.Helper()

	token, err := issuer.producer.Issue(t.Context(), claims, nil)
	if err != nil {
		panic(err)
	}

	return token
}

// keyID stamps the ID of the signing key on each token, as a Kubernetes issuer does.
type keyID string

func (kid keyID) Header(_ context.Context, header *jwa.JWH) (*jwa.JWH, error) {
	header.KID = string(kid)

	return header, nil
}

// serveJSON answers every request with document.
func serveJSON(document any) http.Handler {
	body, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}
