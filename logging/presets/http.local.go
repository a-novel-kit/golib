package loggingpresets

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/golib/logging"
	"github.com/a-novel-kit/golib/otel/utils"
)

var _ logging.HTTPConfig = (*HTTPLocal)(nil)

// HTTPLocal implements [logging.HTTPConfig] for local development, printing a
// one-line, color-coded summary of each request to the terminal. It logs
// through BaseLogger.
type HTTPLocal struct {
	BaseLogger *LogLocal
}

// localStatusPrefixes marks the request line of each level.
var localStatusPrefixes = map[LogLevel]string{
	LogLevelInfo:  "✓ ",
	LogLevelWarn:  "⚠ ",
	LogLevelError: "🗙 ",
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
	wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: w}
	start := time.Now()

	next.ServeHTTP(wrapped, r)

	latency := time.Since(start)
	level := statusLevel(wrapped.Status())
	style := level.style()
	faint := lipgloss.NewStyle().Faint(true)

	message := faint.Render(start.Format(time.StampNano)) + " " +
		style.Render(fmt.Sprintf("%s %s %s", localStatusPrefixes[level], r.Method, r.URL.Path)) +
		faint.Render(fmt.Sprintf(" (%s)", latency))

	if body := string(wrapped.Response()); level != LogLevelInfo && body != "" {
		message += style.Render("\n\t" + strings.ReplaceAll(body, "\n", "\n\t"))
	}

	// This preset only ever runs in local development, where dumping the raw response body is safe.
	_, _ = fmt.Fprint(logger.BaseLogger.Out, strings.TrimSpace(message)+"\n")
}
