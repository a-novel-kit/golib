// Package downtime lets a service honor a planned downtime: from its start, the service refuses
// every request except liveness and leaves its database alone.
//
// A service only knows the start. It keeps refusing work until operators remove it, even past
// the announced end, so a fix that overruns never reopens a service that is still broken.
package downtime

import (
	"slices"
	"strings"
	"time"
)

// ParseStart parses the RFC 3339 start of a planned downtime, as operators set it in the
// environment. It suits [github.com/a-novel-kit/golib/config.LoadEnv] with a nil fallback.
func ParseStart(value string) (*time.Time, error) {
	start, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}

	return &start, nil
}

// Started reports whether a planned downtime beginning at start is in effect at now. A nil start
// means none is planned.
func Started(start *time.Time, now time.Time) bool {
	return start != nil && !now.Before(*start)
}

// isOpen reports whether name starts with one of the prefixes left open during a downtime.
func isOpen(name string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}
