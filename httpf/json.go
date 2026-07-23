package httpf

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// SendJSONStatus encodes data as JSON to w under status, and records the outcome on the
// span.
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

	otel.ReportSuccessNoContent(span)
}
