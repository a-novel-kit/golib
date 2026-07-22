package utils

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

// ErrNotHijackable is returned by Hijack when the wrapped writer does not support
// hijacking, as on HTTP/2.
var ErrNotHijackable = errors.New("the underlying ResponseWriter does not support hijacking")

// captureBodyFrom is the lowest status whose body is kept. Both HTTP logging presets
// read Response() only to report a failure, so a successful response has nothing to keep
// it for.
const captureBodyFrom = http.StatusBadRequest

// CaptureHTTPResponseWriter wraps an http.ResponseWriter and records the status code and
// byte count as the handler writes them, so middleware can report them after the handler
// returns. The body is recorded only for a status of 400 or above.
//
// It forwards the optional interfaces a handler reaches for — [http.Flusher],
// [http.Hijacker], [io.ReaderFrom] — and exposes Unwrap for [http.ResponseController].
// Embedding the interface alone promotes only Header, Write and WriteHeader, and a
// handler's own `w.(http.Flusher)` check would then take its no-flush branch against a
// writer that can in fact flush.
type CaptureHTTPResponseWriter struct {
	http.ResponseWriter

	status   int
	size     int64
	response []byte
}

// Compile-time proof that the optional interfaces survive the wrap.
var (
	_ http.Flusher  = (*CaptureHTTPResponseWriter)(nil)
	_ http.Hijacker = (*CaptureHTTPResponseWriter)(nil)
	_ io.ReaderFrom = (*CaptureHTTPResponseWriter)(nil)
)

func (w *CaptureHTTPResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *CaptureHTTPResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, err
	}

	w.size += int64(n)

	// A handler that writes without calling WriteHeader leaves status at 0, which is an
	// implicit 200 and so not captured.
	if w.status >= captureBodyFrom {
		w.response = append(w.response, b[:n]...)
	}

	return n, nil
}

// Unwrap returns the wrapped writer, which is how [http.ResponseController] reaches
// Flush, Hijack and the deadline setters through a chain of middleware.
func (w *CaptureHTTPResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush forwards to the wrapped writer when it can flush, and does nothing otherwise —
// the same outcome a handler's own capability check would reach.
func (w *CaptureHTTPResponseWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	flusher.Flush()
}

// Hijack forwards to the wrapped writer, and reports [ErrNotHijackable] when it does not
// support hijacking.
func (w *CaptureHTTPResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, ErrNotHijackable
	}

	conn, buf, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("(CaptureHTTPResponseWriter.Hijack) %w", err)
	}

	return conn, buf, nil
}

// ReadFrom forwards to the wrapped writer's own ReadFrom when it has one, so a file
// served this way keeps whatever fast path the platform offers. Otherwise the data is
// copied through Write.
//
// The forwarding path streams past this wrapper, so a body sent through it is counted
// but not captured. It carries a successful response; the failing statuses whose body
// the presets read are written as bytes.
func (w *CaptureHTTPResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		// writerOnly hides this type's own ReadFrom, so io.Copy cannot select it and
		// recurse.
		written, err := io.Copy(writerOnly{w}, src)
		if err != nil {
			return written, fmt.Errorf("(CaptureHTTPResponseWriter.ReadFrom) %w", err)
		}

		return written, nil
	}

	written, err := readerFrom.ReadFrom(src)
	w.size += written

	if err != nil {
		return written, fmt.Errorf("(CaptureHTTPResponseWriter.ReadFrom) %w", err)
	}

	return written, nil
}

// writerOnly exposes only Write, so io.Copy falls back to it rather than finding a
// ReadFrom and calling back into its caller.
type writerOnly struct {
	io.Writer
}

// Status returns the captured status code, defaulting to 200 when the handler
// wrote the body without an explicit WriteHeader call.
func (w *CaptureHTTPResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}

	return w.status
}

func (w *CaptureHTTPResponseWriter) Size() int64 {
	return w.size
}

// Response returns the recorded body. It is populated only for a status of 400 or above.
func (w *CaptureHTTPResponseWriter) Response() []byte {
	return w.response
}
