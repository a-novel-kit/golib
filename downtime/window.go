// Package downtime lets services honor a planned downtime: an announced window during which the
// services it lists refuse work and leave their database alone.
//
// Before the window starts, services work normally and clients can read the window to warn
// users. From its start until operators clear it, [Middleware] and the gRPC interceptors refuse
// every request except the routes left open, such as health and status. The end is an estimate
// for clients: a window keeps refusing work past it until it is cleared.
package downtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrInvalidWindow is returned by [ParseWindow] for a malformed or incomplete window.
var ErrInvalidWindow = errors.New("invalid downtime window")

// Window is a planned downtime. Its JSON form is the value operators set in the environment.
type Window struct {
	// Services names the services the window stops.
	Services []string `json:"services"`
	// Start is when the listed services start refusing work.
	Start time.Time `json:"start"`
	// End is when the window is expected to finish. Clients display it; services ignore it.
	End time.Time `json:"end"`
}

// ParseWindow parses the JSON form of a window. An empty value means there is no window, which
// suits [github.com/a-novel-kit/golib/config.LoadEnv] with a nil fallback.
func ParseWindow(value string) (*Window, error) {
	if value == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()

	var window Window

	err := decoder.Decode(&window)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidWindow, err)
	}

	if len(window.Services) == 0 || window.Start.IsZero() || window.End.Before(window.Start) {
		return nil, fmt.Errorf("%w: it needs services, a start and an end after the start", ErrInvalidWindow)
	}

	return &window, nil
}

// InProgress reports whether the window stops service at now. A nil window stops nothing.
func (window *Window) InProgress(service string, now time.Time) bool {
	return window != nil && !now.Before(window.Start) && slices.Contains(window.Services, service)
}

// isOpen reports whether name starts with one of the prefixes left open during a downtime.
func isOpen(name string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}
