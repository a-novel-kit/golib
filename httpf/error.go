package httpf

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// ErrMap maps sentinel errors to the HTTP status HandleError returns for them. A nil
// key sets the fallback status for unmatched errors; without one, they fall back to
// 500 Internal Server Error.
type ErrMap map[error]int

// HandleError writes a response for a failed handler. It matches err against errMap (with
// errors.Is) to pick a status, logs err, and writes the status text as the body. Unmatched errors
// default to 500 Internal Server Error.
//
// The body never carries err, which can hold internal detail such as a database address; the log
// and the trace keep it. When err carries tags from [WithTag], the body is an RFC 9457 problem
// details object holding those tags instead.
//
// A server error logs at error level and marks the span failed. A client error logs at warning
// level and leaves the span status unset, since the handler answered it.
func HandleError(
	ctx context.Context, logger logging.Log, w http.ResponseWriter, span trace.Span, errMap ErrMap, err error,
) {
	status := http.StatusInternalServerError

	for ref, refStatus := range errMap {
		// A nil key only sets the fallback status; keep scanning, since a concrete match wins.
		if ref == nil {
			status = refStatus

			continue
		}

		if errors.Is(err, ref) {
			status = refStatus

			break
		}
	}

	if status >= http.StatusInternalServerError {
		logger.Err(ctx, otel.ReportError(span, err).Error())
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
