package httpf

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
	"github.com/a-novel-kit/golib/sentryf"
)

// ErrMap maps sentinel errors to the HTTP status HandleError returns for them. An error matching
// several sentinels takes the highest of their statuses, so a server fault never hides behind a
// client error. A nil key sets the status of unmatched errors, which is 500 Internal Server Error
// without one.
type ErrMap map[error]int

// HandleError writes a response for a failed handler. It matches err against errMap (with
// errors.Is) to pick a status, logs err, and writes the status text as the body.
//
// The body never carries err, which can hold internal detail such as a database address. The log
// and the trace keep it. When err carries tags from [WithTag], or a client error comes from a
// validator, the body is an RFC 9457 problem details object holding those tags instead.
//
// A server error logs at error level, marks the span failed and reaches Sentry through
// [sentryf.Capture], under its method and chi route pattern. Under a router from [NewRouter], the
// request's server span names its type through [otel.ErrorType]. A client error logs at warning level
// and leaves the span status unset, since the handler answered it.
//
// A gRPC dependency's planned downtime refusal anywhere in err wins over errMap: the response is
// the one [downtime.Middleware] gives, logged as a warning, since the server did nothing wrong.
func HandleError(
	ctx context.Context, logger logging.Log, w http.ResponseWriter, span trace.Span, errMap ErrMap, err error,
) {
	if downtime.Refused(err) {
		logger.Warn(ctx, err.Error())
		downtime.Respond(w)

		return
	}

	status := errMap.status(err)

	if status >= http.StatusInternalServerError {
		logger.Err(ctx, otel.ReportError(span, err).Error())
		serverSpan(ctx).SetAttributes(otel.ErrorType(err))
		sentryf.Capture(ctx, route(ctx), err)
	} else {
		logger.Warn(ctx, err.Error())
	}

	body := problemBody(span, err, status)
	if body == nil {
		http.Error(w, http.StatusText(status), status)

		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	_, err = w.Write(body)
	if err != nil {
		_ = otel.ReportError(span, fmt.Errorf("write problem body: %w", err))
	}
}

// status returns the highest status mapped to a sentinel err matches. An unmatched err takes the
// status of the nil key, or 500 Internal Server Error without one.
func (errMap ErrMap) status(err error) int {
	matched := 0

	for ref, refStatus := range errMap {
		if ref != nil && refStatus > matched && errors.Is(err, ref) {
			matched = refStatus
		}
	}

	if matched > 0 {
		return matched
	}

	if fallback, ok := errMap[nil]; ok {
		return fallback
	}

	return http.StatusInternalServerError
}

// route names the route chi matched for the request of ctx, such as "GET /v2/session/". It is empty
// outside a chi router.
func route(ctx context.Context) string {
	routeCtx := chi.RouteContext(ctx)
	if routeCtx == nil {
		return ""
	}

	return routeCtx.RouteMethod + " " + routeCtx.RoutePattern()
}
