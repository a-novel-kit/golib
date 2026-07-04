// Package httpf holds small helpers for writing HTTP handler responses that stay
// consistent with the service's OpenTelemetry tracing and structured logging: every
// outcome is recorded on the request span so handlers report success and failure the
// same way.
package httpf

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

// ErrMap maps sentinel errors to the HTTP status HandleError returns for them. A nil
// key sets the fallback status for unmatched errors; without one, they fall back to
// 500 Internal Server Error.
type ErrMap map[error]int

// HandleError writes a response for a failed handler. It records err on the span,
// matches it against errMap (with errors.Is) to pick a status, logs it, and writes the
// status text as the body. Unmatched errors default to 500 Internal Server Error.
func HandleError(
	ctx context.Context, logger logging.Log, w http.ResponseWriter, span trace.Span, errMap ErrMap, err error,
) {
	err = otel.ReportError(span, err)
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

	logger.Err(ctx, err.Error())
	http.Error(w, http.StatusText(status), status)
}
