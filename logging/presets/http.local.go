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

func (logger *HTTPLocal) Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := &utils.CaptureHTTPResponseWriter{ResponseWriter: w}

			start := time.Now()

			next.ServeHTTP(wrapped, r)

			latency := time.Since(start)

			status := wrapped.Status()

			lstyle := lipgloss.NewStyle()

			var (
				prefix string
				body   string
			)

			switch {
			case status >= http.StatusInternalServerError:
				lstyle = lstyle.Foreground(LipColorError)
				prefix = "🗙 "
				body = string(wrapped.Response())
			case status >= http.StatusBadRequest:
				lstyle = lstyle.Foreground(LipColorWarn)
				prefix = "⚠ "
				body = string(wrapped.Response())
			default:
				lstyle = lstyle.Foreground(LipColorInfo)
				prefix = "✓ "
				// Don't print body to keep logs clean.
			}

			lstyleExtra := lipgloss.NewStyle().Faint(true)

			message := lstyle.Render(fmt.Sprintf("%s %s %s", prefix, r.Method, r.URL.Path))
			message += lstyleExtra.Render(fmt.Sprintf(" (%s)", latency))
			message = lstyleExtra.Render(start.Format(time.StampNano)) + " " + message

			if body != "" {
				message += lstyle.Render("\n\t" + strings.ReplaceAll(body, "\n", "\n\t"))
			}

			// This preset only ever runs in local development, where dumping the
			// raw response body is safe.
			_, _ = fmt.Fprint(logger.BaseLogger.Out, strings.TrimSpace(message)+"\n")
		})
	}
}
