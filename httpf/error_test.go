package httpf_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-playground/validator/v10"
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

	type profile struct {
		DisplayName string `validate:"required"`
	}

	type request struct {
		ID      string   `validate:"required"`
		Email   string   `validate:"email"`
		Roles   []string `validate:"dive,oneof=user admin"`
		Profile profile
	}

	errValidation := validator.New(validator.WithRequiredStructEnabled()).
		Struct(request{Email: "jane.doe", Roles: []string{"ghost"}})
	errChan := &json.UnsupportedTypeError{Type: reflect.TypeFor[chan int]()}

	testCases := []struct {
		name string

		errMap httpf.ErrMap
		err    error

		expectStatus          int
		expectBody            string
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
		{
			name:   "Tagged/ClientError",
			errMap: httpf.ErrMap{errMapped: http.StatusConflict},
			err: httpf.WithTag(
				httpf.WithTag(fmt.Errorf("%w: %w", errMapped, errSecret), "accountExists", nil), "retryAfter", 60,
			),
			expectStatus: http.StatusConflict,
			expectBody: `{"type": "about:blank", "title": "Conflict", "status": 409,
				"tags": {"accountExists": true, "retryAfter": 60}}`,
			expectLevel: "warn",
		},
		{
			name:         "Tagged/ServerError",
			err:          httpf.WithTag(errSecret, "retryable", nil),
			expectStatus: http.StatusInternalServerError,
			expectBody: `{"type": "about:blank", "title": "Internal Server Error", "status": 500,
				"tags": {"retryable": true}}`,
			expectLevel:           "error",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: errSecret.Error(),
		},
		{
			name:         "Tagged/Unencodable",
			errMap:       httpf.ErrMap{errMapped: http.StatusBadRequest},
			err:          httpf.WithTag(httpf.WithTag(errMapped, "bad", make(chan int)), "kept", 1),
			expectStatus: http.StatusBadRequest,
			expectBody: `{"type": "about:blank", "title": "Bad Request", "status": 400,
				"tags": {"kept": 1}}`,
			expectLevel:           "warn",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: fmt.Sprintf(`unencodable tag "bad": %v`, errChan),
		},
		{
			name:                  "Tagged/NothingEncodable",
			errMap:                httpf.ErrMap{errMapped: http.StatusBadRequest},
			err:                   httpf.WithTag(errMapped, "bad", make(chan int)),
			expectStatus:          http.StatusBadRequest,
			expectLevel:           "warn",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: fmt.Sprintf(`unencodable tag "bad": %v`, errChan),
		},
		{
			name:         "Validator/ClientError",
			errMap:       httpf.ErrMap{errMapped: http.StatusUnprocessableEntity},
			err:          errors.Join(errValidation, errMapped),
			expectStatus: http.StatusUnprocessableEntity,
			expectBody: `{"type": "about:blank", "title": "Unprocessable Entity", "status": 422,
				"tags": {"invalidFields": {
					"id": "required", "email": "email", "roles[0]": "oneof", "profile.displayName": "required"
				}}}`,
			expectLevel: "warn",
		},
		{
			// A server error can come from a validator rejecting the server's own data.
			name:                  "Validator/ServerError",
			err:                   errValidation,
			expectStatus:          http.StatusInternalServerError,
			expectLevel:           "error",
			expectSpanStatus:      codes.Error,
			expectSpanDescription: errValidation.Error(),
		},
		{
			name:   "Validator/TagSetByHandler",
			errMap: httpf.ErrMap{errMapped: http.StatusUnprocessableEntity},
			err: httpf.WithTag(
				errors.Join(errValidation, errMapped), httpf.InvalidFieldsTag, map[string]string{"email": "taken"},
			),
			expectStatus: http.StatusUnprocessableEntity,
			expectBody: `{"type": "about:blank", "title": "Unprocessable Entity", "status": 422,
				"tags": {"invalidFields": {"email": "taken"}}}`,
			expectLevel: "warn",
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
			require.NotContains(t, w.Body.String(), "password")
			require.NotContains(t, w.Body.String(), "jane.doe")

			if testCase.expectBody == "" {
				require.Equal(t, http.StatusText(testCase.expectStatus)+"\n", w.Body.String())
			} else {
				require.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
				require.JSONEq(t, testCase.expectBody, w.Body.String())
			}

			require.Equal(t, []logEntry{{level: testCase.expectLevel, msg: testCase.err.Error()}}, logger.entries)

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, testCase.expectSpanStatus, spans[0].Status().Code)
			require.Equal(t, testCase.expectSpanDescription, spans[0].Status().Description)
		})
	}
}

func TestWithTag(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")

	testCases := []struct {
		name string

		tag func() error

		expectErr error
	}{
		{
			name: "Nil",
			tag:  func() error { return httpf.WithTag(nil, "key", nil) },
		},
		{
			// Chained tags share one wrapper around the error.
			name: "Chained",
			tag: func() error {
				return httpf.WithTag(httpf.WithTag(errFoo, "first", nil), "second", nil)
			},
			expectErr: errFoo,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.tag()
			if testCase.expectErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expectErr, errors.Unwrap(err))
			require.Equal(t, testCase.expectErr.Error(), err.Error())
		})
	}
}
