package httpf_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/otel"
)

// logEntry is one call recorded by recordingLog.
type logEntry struct {
	level string
	msg   string
}

// recordingLog is a logging.Log that keeps every call for inspection.
type recordingLog struct {
	entries []logEntry
}

func (log *recordingLog) Info(_ context.Context, msg string, _ ...any) {
	log.entries = append(log.entries, logEntry{level: "info", msg: msg})
}

func (log *recordingLog) Warn(_ context.Context, msg string, _ ...any) {
	log.entries = append(log.entries, logEntry{level: "warn", msg: msg})
}

func (log *recordingLog) Err(_ context.Context, msg string, _ ...any) {
	log.entries = append(log.entries, logEntry{level: "error", msg: msg})
}

func TestHandleError(t *testing.T) {
	t.Parallel()

	errMapped := errors.New("mapped")
	// A wrapped driver error is the shape that carries secrets.
	errSecret := errors.New("dial tcp db.internal:5432: password authentication failed for user=admin")
	// A child span already describes this one.
	errReported := otel.ReportError(trace.SpanFromContext(context.Background()), errSecret)

	testCases := []struct {
		name string

		errMap httpf.ErrMap
		err    error

		expectStatus          int
		expectLevel           string
		expectSpanStatus      codes.Code
		expectSpanDescription string
	}{
		{
			name:                  "Error/Unmatched",
			err:                   fmt.Errorf("select user: %w", errSecret),
			expectStatus:          http.StatusInternalServerError,
			expectLevel:           "error",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: "select user: " + errSecret.Error(),
		},
		{
			name:             "Error/ReportedByChild",
			err:              fmt.Errorf("select user: %w", errReported),
			expectStatus:     http.StatusInternalServerError,
			expectLevel:      "error",
			expectSpanStatus: codes.Error,
		},
		{
			name:         "Error/Mapped",
			errMap:       httpf.ErrMap{errMapped: http.StatusNotFound},
			err:          fmt.Errorf("%w: %w", errMapped, errSecret),
			expectStatus: http.StatusNotFound,
			expectLevel:  "warn",
		},
		{
			name:         "Error/Fallback",
			errMap:       httpf.ErrMap{nil: http.StatusBadRequest},
			err:          errSecret,
			expectStatus: http.StatusBadRequest,
			expectLevel:  "warn",
		},
		{
			name:         "Error/MatchBeatsFallback",
			errMap:       httpf.ErrMap{nil: http.StatusBadRequest, errMapped: http.StatusConflict},
			err:          fmt.Errorf("%w: %w", errMapped, errSecret),
			expectStatus: http.StatusConflict,
			expectLevel:  "warn",
		},
		{
			name:                  "Error/MappedServerError",
			errMap:                httpf.ErrMap{errMapped: http.StatusServiceUnavailable},
			err:                   fmt.Errorf("%w: %w", errMapped, errSecret),
			expectStatus:          http.StatusServiceUnavailable,
			expectLevel:           "error",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: "mapped: " + errSecret.Error(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })

			ctx, span := provider.Tracer("httpf-test").Start(t.Context(), "handler")
			logger := &recordingLog{}
			w := httptest.NewRecorder()

			httpf.HandleError(ctx, logger, w, span, testCase.errMap, testCase.err)
			span.End()

			require.Equal(t, testCase.expectStatus, w.Code)
			require.Equal(t, http.StatusText(testCase.expectStatus)+"\n", w.Body.String())
			require.NotContains(t, w.Body.String(), "password")

			require.Equal(t, []logEntry{{level: testCase.expectLevel, msg: testCase.err.Error()}}, logger.entries)

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, testCase.expectSpanStatus, spans[0].Status().Code)
			require.Equal(t, testCase.expectSpanDescription, spans[0].Status().Description)
		})
	}
}
