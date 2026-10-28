package servicetoken

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/a-novel-kit/jwt/v2"
	"github.com/a-novel-kit/jwt/v2/jwa"
	"github.com/a-novel-kit/jwt/v2/jwk"
	"github.com/a-novel-kit/jwt/v2/jwp"
	"github.com/a-novel-kit/jwt/v2/jws"
)

const (
	// leeway absorbs the clock skew between the issuer and this server on a token's lifetime.
	leeway = time.Minute
	// keysCacheDuration is how long the issuer's keys serve before a refresh. A token signed with a
	// key missing from the cache triggers an earlier, rate-limited one.
	keysCacheDuration = time.Hour
	// keysStaleIfError keeps the last keys in service while the issuer is unreachable, so an outage
	// of its endpoint leaves service calls running. A key the issuer withdraws stays trusted within
	// this window until a fetch succeeds.
	keysStaleIfError = 24 * time.Hour
	// maxDocumentSize caps each document the issuer serves.
	maxDocumentSize = 1 << 20
)

// Config names the service tokens a [Verifier] accepts.
type Config struct {
	// Issuer is the URL of the cluster's service-account issuer, as tokens carry it in their "iss"
	// claim. A GKE cluster's issuer is
	// https://container.googleapis.com/v1/projects/<project>/locations/<location>/clusters/<cluster>.
	Issuer string `json:"issuer" yaml:"issuer"`
	// Audience is the audience this server answers to. Each caller's projected volume requests its
	// token for this audience.
	Audience string `json:"audience" yaml:"audience"`
	// Callers are the subjects allowed to call, each written system:serviceaccount:<namespace>:<name>.
	Callers []string `json:"callers" yaml:"callers"`
}

// A Verifier checks the service token of an incoming call. It verifies the RS256 signature against
// the issuer's keys locally, so build one per process and share it: it caches the keys.
type Verifier struct {
	recipient *jwt.Recipient
}

// NewVerifier returns a [Verifier] for the tokens config names. It fetches the issuer's keys
// through client on the first call, from the key set the issuer's OpenID discovery document names.
func NewVerifier(config Config, client *http.Client) *Verifier {
	source := jwk.NewSource(jwk.SourceConfig{
		CacheDuration:         keysCacheDuration,
		Fetch:                 issuerKeys{client: client, issuer: config.Issuer}.fetch,
		StaleIfError:          keysStaleIfError,
		RefreshOnUnknownKeyID: true,
	})

	checker := jwp.NewClaimsChecker(&jwp.ClaimsCheckerConfig{
		Checks: []jwp.ClaimsCheck{jwp.NewClaimsCheckTimestamp(leeway, true), callerCheck(config)},
	})

	return &Verifier{recipient: jwt.NewRecipient(jwt.RecipientConfig{
		Plugins:      []jwt.RecipientPlugin{jws.NewSourcedRSAVerifier(source, jws.RS256)},
		Deserializer: checker.Unmarshal,
	})}
}

// Verify checks a service token. An error matching [jwt.ErrTokenRejected] refuses the token, which
// the caller resolves by sending a valid one. Any other error is the server's fault, such as an
// issuer it cannot reach.
func (verifier *Verifier) Verify(ctx context.Context, token string) error {
	var claims jwa.Claims

	err := verifier.recipient.Consume(ctx, token, &claims)
	if err != nil {
		return fmt.Errorf("verify service token: %w", err)
	}

	return nil
}

// callerCheck rejects a token from another issuer, for another audience, or from a subject outside
// the allowed callers.
type callerCheck Config

func (check callerCheck) CheckClaims(claims *jwa.Claims) error {
	switch {
	case claims.Iss != check.Issuer:
		return fmt.Errorf("issuer %q is not %q", claims.Iss, check.Issuer)
	case !slices.Contains(claims.Aud, check.Audience):
		return fmt.Errorf("audience %v lacks %q", claims.Aud, check.Audience)
	case !slices.Contains(check.Callers, claims.Sub):
		return fmt.Errorf("caller %q is not allowed", claims.Sub)
	}

	return nil
}

// issuerKeys fetches the keys an issuer signs its tokens with.
type issuerKeys struct {
	client *http.Client
	issuer string
}

// fetch reads the key set the issuer's OpenID discovery document names.
func (keys issuerKeys) fetch(ctx context.Context) ([]*jwa.JWK, error) {
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"` //nolint:tagliatelle // OpenID Connect Discovery names the member.
	}

	err := keys.get(ctx, keys.issuer+"/.well-known/openid-configuration", &discovery)
	if err != nil {
		return nil, fmt.Errorf("discover issuer: %w", err)
	}

	// OpenID Connect Discovery requires the document to name the issuer it was fetched from.
	if discovery.Issuer != keys.issuer {
		return nil, fmt.Errorf("discovery document names issuer %q", discovery.Issuer)
	}

	var set struct {
		Keys []jwa.JWK `json:"keys"`
	}

	err = keys.get(ctx, discovery.JWKSURI, &set)
	if err != nil {
		return nil, fmt.Errorf("fetch key set: %w", err)
	}

	return verificationKeys(set.Keys), nil
}

// get decodes the JSON document at address into dst.
func (keys issuerKeys) get(ctx context.Context, address string, dst any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	response, err := keys.client.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: %s", address, response.Status)
	}

	err = json.NewDecoder(io.LimitReader(response.Body, maxDocumentSize)).Decode(dst)
	if err != nil {
		return fmt.Errorf("decode %s: %w", address, err)
	}

	return nil
}

// verificationKeys returns the keys of set as the signature verifier reads them. Kubernetes marks a
// signing key with "use" alone, which RFC 7517 lets stand for "key_ops". The verifier reads
// "key_ops", so a signing key without them gets the verification its use implies.
func verificationKeys(set []jwa.JWK) []*jwa.JWK {
	keys := make([]*jwa.JWK, len(set))

	for i := range set {
		key := &set[i]
		if key.Use == jwa.UseSig && len(key.KeyOps) == 0 {
			key.KeyOps = jwa.KeyOps{jwa.KeyOpVerify}
		}

		keys[i] = key
	}

	return keys
}
