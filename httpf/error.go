package httpf

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel"
)

type ErrMap map[error]int

func HandleError(
	ctx context.Context, logger logging.Log, w http.ResponseWriter, span trace.Span, errMap ErrMap, err error,
) {
	err = otel.ReportError(span, err)
	status := http.StatusInternalServerError

	for ref, refStatus := range errMap {
		// Default value override. Only effective if no other error matches.
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
