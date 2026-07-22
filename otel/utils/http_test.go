package utils_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/otel/utils"
)

// The wrapper is the innermost middleware in every service, so it is the writer every
// handler receives. What it drops, every handler loses.

func TestCaptureKeepsTheBodyOnlyForAFailure(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		status int
		// writeHeader reflects a handler that writes without calling WriteHeader, where
		// net/http sends an implicit 200.
		writeHeader bool

		expectStatus int
		expectBody   string
	}{
		{
			name:         "ImplicitOK",
			writeHeader:  false,
			expectStatus: http.StatusOK,
			expectBody:   "",
		},
		{
			name:         "ExplicitOK",
			status:       http.StatusOK,
			writeHeader:  true,
			expectStatus: http.StatusOK,
			expectBody:   "",
		},
		{
			name:         "Created",
			status:       http.StatusCreated,
			writeHeader:  true,
			expectStatus: http.StatusCreated,
			expectBody:   "",
		},
		{
			// The local preset reports a body from 400 up, not 500 up.
			name:         "BadRequest",
			status:       http.StatusBadRequest,
			writeHeader:  true,
			expectStatus: http.StatusBadRequest,
			expectBody:   "payload",
		},
		{
			name:         "InternalServerError",
			status:       http.StatusInternalServerError,
			writeHeader:  true,
			expectStatus: http.StatusInternalServerError,
			expectBody:   "payload",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: httptest.NewRecorder()}

			if testCase.writeHeader {
				wrapped.WriteHeader(testCase.status)
			}

			n, err := wrapped.Write([]byte("payload"))
			require.NoError(t, err)
			require.Equal(t, len("payload"), n)

			require.Equal(t, testCase.expectStatus, wrapped.Status())
			require.Equal(t, testCase.expectBody, string(wrapped.Response()))

			// The size is counted whether or not the body is kept, since it is reported
			// for every request.
			require.Equal(t, int64(len("payload")), wrapped.Size())
		})
	}
}

// flushRecorder reports whether Flush reached the writer underneath the wrapper.
type flushRecorder struct {
	http.ResponseWriter

	flushed bool
}

func (w *flushRecorder) Flush() { w.flushed = true }

func TestCaptureForwardsFlush(t *testing.T) {
	t.Parallel()

	// The idiomatic capability check a streaming handler makes. Against a wrapper that
	// only embeds the interface it yields false, and the handler buffers what it meant
	// to stream.
	underlying := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: underlying}

	flusher, ok := any(wrapped).(http.Flusher)
	require.True(t, ok, "a handler's own http.Flusher assertion must succeed")

	flusher.Flush()
	require.True(t, underlying.flushed, "Flush must reach the writer underneath")
}

func TestCaptureFlushIsSafeWithoutSupport(t *testing.T) {
	t.Parallel()

	// httptest.NewRecorder does implement Flusher, so a writer that does not needs to be
	// built by hand.
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: writeOnlyResponseWriter{}}

	require.NotPanics(t, wrapped.Flush)
}

func TestCaptureHijackReportsAWriterThatCannot(t *testing.T) {
	t.Parallel()

	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: httptest.NewRecorder()}

	_, _, err := wrapped.Hijack()
	require.ErrorIs(t, err, utils.ErrNotHijackable)
}

func TestCaptureUnwrapReachesTheWrappedWriter(t *testing.T) {
	t.Parallel()

	// http.ResponseController walks Unwrap to find Flush, so a chain of middleware stays
	// transparent to it.
	underlying := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: underlying}

	require.NoError(t, http.NewResponseController(wrapped).Flush())
	require.True(t, underlying.flushed)
}

func TestCaptureReadFromCountsWithoutAWrappedReaderFrom(t *testing.T) {
	t.Parallel()

	// The fallback copies through Write, so the size stays accurate and the copy does not
	// recurse into ReadFrom.
	recorder := httptest.NewRecorder()
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: recorder}

	written, err := wrapped.ReadFrom(strings.NewReader("streamed"))
	require.NoError(t, err)
	require.Equal(t, int64(len("streamed")), written)
	require.Equal(t, int64(len("streamed")), wrapped.Size())
	require.Equal(t, "streamed", recorder.Body.String())
}

func TestCaptureReadFromForwardsWhenSupported(t *testing.T) {
	t.Parallel()

	underlying := &readFromRecorder{ResponseWriter: httptest.NewRecorder()}
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: underlying}

	written, err := wrapped.ReadFrom(strings.NewReader("streamed"))
	require.NoError(t, err)
	require.Equal(t, int64(len("streamed")), written)
	require.True(t, underlying.used, "the wrapped writer's own ReadFrom must be preferred")
	require.Equal(t, int64(len("streamed")), wrapped.Size())
}

type readFromRecorder struct {
	http.ResponseWriter

	used bool
}

func (w *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	w.used = true

	return io.Copy(w.ResponseWriter, src)
}

// writeOnlyResponseWriter implements the interface and nothing beyond it.
type writeOnlyResponseWriter struct{}

func (writeOnlyResponseWriter) Header() http.Header         { return http.Header{} }
func (writeOnlyResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (writeOnlyResponseWriter) WriteHeader(int)             {}
