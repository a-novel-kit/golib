package servicetokentest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a-novel-kit/jwt/v2"
	"github.com/a-novel-kit/jwt/v2/jwa"
	"github.com/a-novel-kit/jwt/v2/jwk"
	"github.com/a-novel-kit/jwt/v2/jws"
)

// An Issuer publishes its key as a GKE cluster does, and signs service tokens with it. As an
// [http.Handler], it serves the discovery document and key set a servicetoken.Verifier fetches.
//
// It also signs a token for callers outside Go, which ask for one with
// GET /token?subject=<caller>&audience=<audience>. The token's "iss" claim is [Issuer.URL] whatever
// address the request reached, so a caller on the host gets a token a verifier inside a container
// network accepts.
type Issuer struct {
	// URL is the issuer, as servicetoken.Config and a token's "iss" claim name it.
	URL string

	producer *jwt.Producer
	mux      *http.ServeMux
}

// NewIssuer starts an Issuer on a local port. The test's cleanup stops it.
func NewIssuer(t *testing.T) *Issuer {
	t.Helper()

	server := httptest.NewUnstartedServer(nil)

	issuer, err := NewIssuerAt("http://" + server.Listener.Addr().String())
	if err != nil {
		panic(err)
	}

	server.Config.Handler = issuer
	server.Start()
	t.Cleanup(server.Close)

	return issuer
}

// NewIssuerAt returns an Issuer for a server that serves it at url. Its new RS256 key lives in
// memory only, so the tokens it signs die with the process.
func NewIssuerAt(url string) (*Issuer, error) {
	privateKey, publicKey, err := jwk.GenerateRSA(jwk.RS256)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	// A GKE key set marks its keys with "use" alone.
	published := *publicKey.JWK
	published.KeyOps = nil

	issuer := &Issuer{
		URL: url,
		producer: jwt.NewProducer(jwt.ProducerConfig{
			StaticPlugins: []jwt.ProducerStaticPlugin{keyID(publicKey.KID)},
			Plugins:       []jwt.ProducerPlugin{jws.NewRSASigner(privateKey.Key(), jws.RS256)},
		}),
		mux: http.NewServeMux(),
	}

	issuer.mux.Handle("GET /.well-known/openid-configuration", serveJSON(map[string]string{
		"issuer":   url,
		"jwks_uri": url + "/jwks",
	}))
	issuer.mux.Handle("GET /jwks", serveJSON(map[string][]jwa.JWK{"keys": {published}}))
	issuer.mux.HandleFunc("GET /token", issuer.serveToken)

	return issuer, nil
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

// ServeHTTP answers the routes [Issuer] describes.
func (issuer *Issuer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	issuer.mux.ServeHTTP(w, r)
}

// serveToken answers the query's subject and audience with a token of [Issuer.Claims], as plain
// text. That text is what a projected token file holds, so a caller can write it to the file it
// reads its token from.
func (issuer *Issuer) serveToken(w http.ResponseWriter, r *http.Request) {
	subject, audience := r.URL.Query().Get("subject"), r.URL.Query().Get("audience")
	if subject == "" || audience == "" {
		http.Error(w, "subject and audience are required", http.StatusBadRequest)

		return
	}

	token, err := issuer.producer.Issue(r.Context(), issuer.Claims(subject, audience), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(token)) //nolint:gosec // A token is base64url text, which no browser renders as markup.
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
