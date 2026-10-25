package loggingpresets

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/felixge/httpsnoop"

	"github.com/a-novel-kit/golib/logging"
)

// keptBodyLimit caps the response body HTTPLocal keeps to print under a failed request. The client
// still receives the whole body.
const keptBodyLimit = 4 << 10

var _ logging.HTTPConfig = (*HTTPLocal)(nil)

// HTTPLocal implements [logging.HTTPConfig] for local development, printing a
// one-line, color-coded summary of each request to the terminal. It logs
// through BaseLogger.
type HTTPLocal struct {
	BaseLogger *LogLocal
}

// localStatusPrefixes marks the request line of each level.
var localStatusPrefixes = map[LogLevel]string{
	LogLevelInfo:  "✓",
	LogLevelWarn:  "⚠",
	LogLevelError: "🗙",
}

func (logger *HTTPLocal) Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.serve(next, w, r)
		})
	}
}

// serve runs next, then prints the request line, followed by the response body of a failed request.
func (logger *HTTPLocal) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	metrics, body := serveKeepingBody(next, w, r)

	level := statusLevel(metrics.Code)
	style := level.style()
	faint := lipgloss.NewStyle().Faint(true)

	message := faint.Render(start.Format(time.StampNano)) + " " +
		style.Render(fmt.Sprintf("%s %s %s", localStatusPrefixes[level], r.Method, r.URL.Path)) +
		faint.Render(fmt.Sprintf(" (%s)", metrics.Duration))

	if level != LogLevelInfo && len(body) > 0 {
		message += style.Render("\n\t" + strings.ReplaceAll(string(body), "\n", "\n\t"))
	}

	// This preset only ever runs in local development, where dumping the raw response body is safe.
	_, _ = fmt.Fprint(logger.BaseLogger.Out, strings.TrimSpace(message)+"\n")
}

// serveKeepingBody serves r through next. It returns the metrics httpsnoop captures, and the first
// keptBodyLimit bytes next writes. A body sent through io.ReaderFrom is counted but not kept.
func serveKeepingBody(next http.Handler, w http.ResponseWriter, r *http.Request) (httpsnoop.Metrics, []byte) {
	var body []byte

	keepBody := httpsnoop.Hooks{
		Write: func(write httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(b []byte) (int, error) {
				n, err := write(b)
				body = append(body, b[:min(n, keptBodyLimit-len(body))]...)

				return n, err
			}
		},
	}

	metrics := httpsnoop.CaptureMetrics(next, httpsnoop.Wrap(w, keepBody), r)

	return metrics, body
}
