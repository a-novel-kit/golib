package httpf

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// SendJSONStatus encodes data as JSON to w under status. It leaves the span status to the
// handler, which knows whether an error status stems from a fault.
//
// It writes the status line itself, after the Content-Type header, since net/http freezes the
// headers once the status goes out. Call it on a writer that has written nothing yet.
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
	}
}
