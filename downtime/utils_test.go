package downtime_test

import (
	"time"

	"github.com/a-novel-kit/golib/downtime"
)

// window builds a window over services, from start to end relative to now. RFC 3339 metadata
// keeps whole seconds, so now is truncated to keep round trips exact.
func window(now time.Time, start, end time.Duration, services ...string) *downtime.Window {
	return &downtime.Window{Services: services, Start: now.Add(start), End: now.Add(end)}
}
