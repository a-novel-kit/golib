package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

const (
	// PingTimeout bounds how long Ping keeps retrying before giving up.
	PingTimeout = 10 * time.Second
	// PingRetryInterval is the wait between failed ping attempts, keeping Ping
	// from tight-looping reconnects on an unreachable database.
	PingRetryInterval = 100 * time.Millisecond
)

// Ping a database connection until it succeeds or the timeout is reached.
// Honors ctx cancellation both for the PingContext call and for the wait
// between retries.
func Ping(ctx context.Context, client *bun.DB) error {
	return ping(ctx, client, PingTimeout, PingRetryInterval)
}

type pinger interface {
	PingContext(ctx context.Context) error
}

func ping(ctx context.Context, client pinger, timeout time.Duration, retryInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}

	pingContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	for {
		err := client.PingContext(pingContext)
		if err == nil {
			return nil
		}

		contextErr := pingContext.Err()
		if contextErr != nil {
			return fmt.Errorf("ping database: %w", errors.Join(err, contextErr))
		}

		timer := time.NewTimer(retryInterval)

		select {
		case <-pingContext.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return fmt.Errorf("ping database: %w", errors.Join(err, pingContext.Err()))
		case <-timer.C:
		}
	}
}
