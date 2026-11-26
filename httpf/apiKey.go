package httpf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/a-novel-kit/jwt/v2"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/apikey"
	"github.com/a-novel-kit/golib/logging"
)

// apiKeyErrors answers 401 to a request the gate refuses. Any other failure is a body that failed
// to read, which answers 400 as a handler's own decoding does, and 413 past the route's body limit.
var apiKeyErrors = ErrMap{jwt.ErrTokenRejected: http.StatusUnauthorized, nil: http.StatusBadRequest}

var (
	// errRequestTokenMissing refuses a request whose Authorization header carries no Bearer token.
	errRequestTokenMissing = fmt.Errorf("%w: expected a request token as a Bearer token", jwt.ErrTokenRejected)
	// errAPIKeyUnset refuses every request of a gate that holds no public key.
	errAPIKeyUnset = fmt.Errorf("%w: the gate holds no public key to verify a request token", jwt.ErrTokenRejected)
)

// APIKeyGate returns the middleware that refuses a request unless it carries a request token signed
// with the API key config names, and logs each refusal through logger. The owner of the key sends
// the token as a Bearer token in the Authorization header (RFC 6750 §2.1), and the handlers read
// the body the token was checked against.
//
// The gate fails closed: without a public key it refuses every request, unless config.Optional lets
// every request through. It returns the error of [apikey.NewVerifier] for a key it cannot read.
//
// The gate holds a request's body in memory, so mount it under a body limit, as every route of
// [NewMux] is.
func APIKeyGate(config apikey.Config, logger logging.Log) (func(http.Handler) http.Handler, error) {
	if config.PublicKey == "" {
		if config.Optional {
			return func(next http.Handler) http.Handler { return next }, nil
		}

		return func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refuseAPIKey(w, r, logger, errAPIKeyUnset)
			})
		}, nil
	}

	verifier, err := apikey.NewVerifier(config)
	if err != nil {
		return nil, fmt.Errorf("api key verifier: %w", err)
	}

	return apiKeyMiddleware(verifier, logger), nil
}

// apiKeyMiddleware refuses a request unless verifier accepts its request token for its body. The
// verifier reads the body, which the middleware puts back for the handlers.
func apiKeyMiddleware(verifier *apikey.Verifier, logger logging.Log) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// RFC 9110 §11.1: the name of an authentication scheme ignores case.
			scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") {
				refuseAPIKey(w, r, logger, errRequestTokenMissing)

				return
			}

			body, err := verifier.Verify(r.Context(), token, r.Body)
			if err != nil {
				refuseAPIKey(w, r, logger, err)

				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))

			next.ServeHTTP(w, r)
		})
	}
}

// refuseAPIKey answers a request the gate refuses, through [HandleError], which logs err.
//
// A refused token answers 401 with the WWW-Authenticate challenge RFC 9110 §15.5.2 requires. The
// challenge carries no error code, which RFC 6750 §3 only recommends: every refusal reads alike, so
// the answer never says which check failed. The log does.
func refuseAPIKey(w http.ResponseWriter, r *http.Request, logger logging.Log, err error) {
	if errors.Is(err, jwt.ErrTokenRejected) {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}

	HandleError(r.Context(), logger, w, trace.SpanFromContext(r.Context()), apiKeyErrors, err)
}
