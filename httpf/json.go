package httpf

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// SendJSON encodes data as JSON to w with a JSON content type and records the outcome
// on the span. An encoding failure is reported on the span; by then the status line is
// already sent, so the body may be truncated. The caller sets any non-200 status before
// calling.
func SendJSON[Data any](_ context.Context, w http.ResponseWriter, span trace.Span, data Data) {
	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		_ = otel.ReportError(span, err)

		return
	}

	otel.ReportSuccessNoContent(span)
}
