package httpf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// SendJSONStatus encodes data as JSON to w under status, and records the outcome on the
// span. HTTP error statuses (400 and above) report an error using the standard status
// text, without recording the payload. Other statuses report success after encoding.
// This follows HandleError's application-span convention for client and server errors.
//
// The status is a parameter rather than something the caller sends first, because
// net/http freezes the outbound header set when the status line goes out. A caller that
// writes the status first has its Content-Type discarded and answers text/plain; one
// that writes it afterwards gets a "superfluous WriteHeader" and keeps the 200. Owning
// both is the only order that answers JSON under a status other than 200.
//
// An encoding failure is reported on the span. The status line is already sent by then,
// so the body may be truncated.
func SendJSONStatus[Data any](
	_ context.Context, w http.ResponseWriter, span trace.Span, status int, data Data,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		_ = otel.ReportError(span, err)

		return
	}

	if status >= http.StatusBadRequest {
		_ = otel.ReportError(span, errors.New(http.StatusText(status)))

		return
	}

	otel.ReportSuccessNoContent(span)
}
