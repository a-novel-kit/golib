package httpf

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

func SendJSON[Data any](_ context.Context, w http.ResponseWriter, span trace.Span, data Data) {
	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		_ = otel.ReportError(span, err)

		return
	}

	otel.ReportSuccessNoContent(span)
}
