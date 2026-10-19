// Package downtime lets a service honor a planned downtime: from its start, the service refuses
// every request except liveness and leaves its database alone.
//
// A service only knows the start. It keeps refusing work until operators remove it, even past
// the announced end, so a fix that overruns never reopens a service that is still broken.
package downtime
