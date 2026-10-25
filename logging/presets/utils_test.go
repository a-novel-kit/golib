package loggingpresets_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireOptionalInterfaces serves a request through middleware into writers that each implement one
// optional interface, or none. The handler behind middleware must find exactly the interfaces of the
// writer underneath, and each call it makes must reach that writer.
func requireOptionalInterfaces(t *testing.T, middleware func(http.Handler) http.Handler) {
	t.Helper()

	testCases := []struct {
		name string

		writer func(recorder *recordingWriter) http.ResponseWriter

		expect []string
	}{
		{
			name:   "Success/None",
			writer: func(recorder *recordingWriter) http.ResponseWriter { return recorder },
		},
		{
			name:   "Success/Flusher",
			writer: func(recorder *recordingWriter) http.ResponseWriter { return flusherWriter{recorder} },
			expect: []string{"Flush"},
		},
		{
			name:   "Success/Hijacker",
			writer: func(recorder *recordingWriter) http.ResponseWriter { return hijackerWriter{recorder} },
			expect: []string{"Hijack"},
		},
		{
			name:   "Success/ReaderFrom",
			writer: func(recorder *recordingWriter) http.ResponseWriter { return readerFromWriter{recorder} },
			expect: []string{"ReadFrom"},
		},
		{
			name:   "Success/Pusher",
			writer: func(recorder *recordingWriter) http.ResponseWriter { return pusherWriter{recorder} },
			expect: []string{"Push"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingWriter{ResponseWriter: httptest.NewRecorder()}

			var reached []string

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = callOptionalInterfaces(w)
			}))
			handler.ServeHTTP(testCase.writer(recorder), httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/items/42", nil,
			))

			require.Equal(t, testCase.expect, reached, "the handler must find the writer's interfaces")
			require.Equal(t, testCase.expect, recorder.calls, "each call must reach the writer")
		})
	}
}

// callOptionalInterfaces calls every optional interface w implements, and returns the methods it
// called.
func callOptionalInterfaces(w http.ResponseWriter) []string {
	var called []string

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()

		called = append(called, "Flush")
	}

	if hijacker, ok := w.(http.Hijacker); ok {
		_, _, _ = hijacker.Hijack()

		called = append(called, "Hijack")
	}

	if readerFrom, ok := w.(io.ReaderFrom); ok {
		_, _ = readerFrom.ReadFrom(strings.NewReader("body"))

		called = append(called, "ReadFrom")
	}

	if pusher, ok := w.(http.Pusher); ok {
		_ = pusher.Push("/style.css", nil)

		called = append(called, "Push")
	}

	return called
}

// recordingWriter implements http.ResponseWriter and no optional interface. The writers embedding it
// add one each, and record its calls here.
type recordingWriter struct {
	http.ResponseWriter

	calls []string
}

type flusherWriter struct{ *recordingWriter }

func (w flusherWriter) Flush() { w.calls = append(w.calls, "Flush") }

type hijackerWriter struct{ *recordingWriter }

func (w hijackerWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.calls = append(w.calls, "Hijack")

	return nil, nil, http.ErrNotSupported
}

type readerFromWriter struct{ *recordingWriter }

func (w readerFromWriter) ReadFrom(src io.Reader) (int64, error) {
	w.calls = append(w.calls, "ReadFrom")

	return io.Copy(w.ResponseWriter, src)
}

type pusherWriter struct{ *recordingWriter }

func (w pusherWriter) Push(string, *http.PushOptions) error {
	w.calls = append(w.calls, "Push")

	return nil
}
