package httpf

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/a-novel-kit/jwt/v2"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/servicetoken"
)

// serviceTokenErrors answers 401 to a token the verifier refuses. Any other failure, such as an
// issuer the verifier cannot reach, stays a 500 server fault.
var serviceTokenErrors = ErrMap{jwt.ErrTokenRejected: http.StatusUnauthorized}

// errServiceTokenCount refuses a request that carries no service token, or several.
var errServiceTokenCount = fmt.Errorf("%w: expected one %s header", jwt.ErrTokenRejected, servicetoken.Header)

// serviceTokenCallerKey holds the caller of a request, once [ServiceTokenMiddleware] verified its
// service token.
type serviceTokenCallerKey struct{}

// ServiceTokenMiddleware refuses a request unless verifier accepts its one service token, which a
// client sends through [ServiceTokenTransport]. The handlers then read the token's caller with
// [ServiceTokenCaller]. A request to a path listed in open passes unchecked, such as the liveness
// route the kubelet probes without a token. [HandleError] answers each refusal and logs it through
// logger.
func ServiceTokenMiddleware(
	verifier *servicetoken.Verifier, logger logging.Log, open ...string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(open, r.URL.Path) {
				next.ServeHTTP(w, r)

				return
			}

			caller, err := verifyServiceToken(r.Context(), verifier, r.Header.Values(servicetoken.Header))
			if err != nil {
				HandleError(r.Context(), logger, w, trace.SpanFromContext(r.Context()), serviceTokenErrors, err)

				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serviceTokenCallerKey{}, caller)))
		})
	}
}

// ServiceTokenCaller returns the caller of the request of ctx, such as
// system:serviceaccount:studio:web, once [ServiceTokenMiddleware] verified its service token. It
// reports false for a request the middleware did not verify: one to an open path, or one to a server
// that runs without the middleware.
func ServiceTokenCaller(ctx context.Context) (string, bool) {
	caller, ok := ctx.Value(serviceTokenCallerKey{}).(string)

	return caller, ok
}

// verifyServiceToken verifies the one token a request's service token header must hold, and returns
// its caller.
func verifyServiceToken(ctx context.Context, verifier *servicetoken.Verifier, tokens []string) (string, error) {
	if len(tokens) != 1 {
		return "", errServiceTokenCount
	}

	return verifier.Verify(ctx, tokens[0])
}
