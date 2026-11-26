package apikey

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/a-novel-kit/jwt/v2"
	"github.com/a-novel-kit/jwt/v2/jwa"
	"github.com/a-novel-kit/jwt/v2/jwp"
	"github.com/a-novel-kit/jwt/v2/jws"
)

const (
	// MaxLifetime is the longest a request token lives, from its "iat" claim to its "exp" claim.
	MaxLifetime = 5 * time.Minute
	// Leeway absorbs the clock skew between the signer and this server on a token's times: a token
	// is accepted from Leeway before its "iat" claim until Leeway after its "exp" claim.
	Leeway = 30 * time.Second
)

var (
	// ErrPurposeRequired refuses a [Config] that names no purpose: its verifier would accept a
	// token made for none.
	ErrPurposeRequired = errors.New("api key purpose required")
	// ErrInvalidPublicKey refuses a [Config] whose public key is not one Ed25519 public key in PEM.
	ErrInvalidPublicKey = errors.New("invalid api key public key")
)

// Config names the system API key a [Verifier] trusts.
type Config struct {
	// Purpose is the one thing the key may do, such as publish-narrative-engine-plugin. A request
	// token names it in its "aud" claim.
	Purpose string `json:"purpose" yaml:"purpose"`
	// PublicKey is the key's Ed25519 public key in PEM, as "openssl pkey -pubout" writes it.
	PublicKey string `json:"publicKey" yaml:"publicKey"`
	// Optional lets a gate without a PublicKey admit every request. Only a local or test stack sets
	// it: a gate without a key otherwise refuses every request.
	Optional bool `json:"optional" yaml:"optional"`
}

// Claims are the claims of a request token.
type Claims struct {
	// Aud is the purpose of the API key that signs the token.
	Aud string `json:"aud,omitempty"`
	// Iat is when the token was signed, as a Unix timestamp in seconds.
	Iat int64 `json:"iat,omitempty"`
	// Exp is when the token expires, as a Unix timestamp in seconds, [MaxLifetime] after Iat at most.
	Exp int64 `json:"exp,omitempty"`
	// BodySHA256 is the SHA-256 of the body of the request the token was signed for, in lowercase
	// hex.
	BodySHA256 string `json:"bodySHA256,omitempty"` //nolint:tagliatelle // The token names the claim.
}

// A Verifier checks the request tokens signed with one API key. It holds the public key, so a check
// calls no other service.
type Verifier struct {
	recipient *jwt.Recipient
}

// NewVerifier returns a [Verifier] for the request tokens config's key signs. It returns
// [ErrPurposeRequired] without a purpose, and [ErrInvalidPublicKey] for a public key it cannot read.
func NewVerifier(config Config) (*Verifier, error) {
	if config.Purpose == "" {
		return nil, ErrPurposeRequired
	}

	publicKey, err := parsePublicKey(config.PublicKey)
	if err != nil {
		return nil, err
	}

	checker := jwp.NewClaimsChecker(&jwp.ClaimsCheckerConfig{
		Checks: []jwp.ClaimsCheck{jwp.NewClaimsCheckTimestamp(Leeway, true), requestCheck(config.Purpose)},
	})

	return &Verifier{recipient: jwt.NewRecipient(jwt.RecipientConfig{
		Plugins:      []jwt.RecipientPlugin{jws.NewED25519Verifier(publicKey)},
		Deserializer: checker.Unmarshal,
	})}, nil
}

// Verify checks a request token and the body it was signed for, and returns that body. It checks the
// token's signature, purpose and times first, and reads body only once they hold, so a caller
// without the key makes the server read nothing.
//
// An error matching [jwt.ErrTokenRejected] refuses the request, which the caller resolves by signing
// a token for the body it sends. Any other error is a body that failed to read.
//
// A token can be replayed until it expires, with the body it was signed for alone. Verify keeps no
// token, so a purpose whose requests are not safe to repeat needs a check of its own.
func (verifier *Verifier) Verify(ctx context.Context, token string, body io.Reader) ([]byte, error) {
	var claims Claims

	err := verifier.recipient.Consume(ctx, token, &claims)
	if err != nil {
		// The verifier holds its key, so a token it cannot verify or decode is at fault.
		return nil, fmt.Errorf("verify request token: %w: %w", jwt.ErrTokenRejected, err)
	}

	content, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	digest := sha256.Sum256(content)
	if claims.BodySHA256 != hex.EncodeToString(digest[:]) {
		return nil, fmt.Errorf("%w: the body is not the one the request token was signed for", jwt.ErrTokenRejected)
	}

	return content, nil
}

// requestCheck rejects a token made for another purpose than its own, signed in the future, or
// living longer than [MaxLifetime].
type requestCheck string

func (purpose requestCheck) CheckClaims(claims *jwa.Claims) error {
	signedAt := time.Unix(claims.Iat, 0)
	lifetime := time.Unix(claims.Exp, 0).Sub(signedAt)

	switch {
	case len(claims.Aud) != 1 || claims.Aud[0] != string(purpose):
		return fmt.Errorf("purpose %q is not %q", claims.Aud, string(purpose))
	case claims.Iat <= 0:
		return errors.New("missing issue date")
	case signedAt.After(time.Now().Add(Leeway)):
		return fmt.Errorf("token signed in the future, at %s", signedAt)
	case lifetime <= 0 || lifetime > MaxLifetime:
		return fmt.Errorf("token lives %s, expected %s at most", lifetime, MaxLifetime)
	}

	return nil
}

// parsePublicKey reads the one Ed25519 public key a PEM document holds.
func parsePublicKey(document string) (ed25519.PublicKey, error) {
	block, rest := pem.Decode([]byte(document))
	if block == nil || len(bytes.TrimSpace(rest)) > 0 {
		return nil, fmt.Errorf("%w: expected one PEM block", ErrInvalidPublicKey)
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPublicKey, err)
	}

	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: %T is not an Ed25519 key", ErrInvalidPublicKey, parsed)
	}

	return publicKey, nil
}
