package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

const (
	PingTimeout = 10 * time.Second
	// PingRetryInterval is the wait between failed ping attempts. Without it,
	// Ping would tight-loop reconnects on an unreachable database for the
	// duration of PingTimeout.
	PingRetryInterval = 100 * time.Millisecond
)

// Ping a database connection until it succeeds or the timeout is reached.
// Honors ctx cancellation both for the PingContext call and for the wait
// between retries.
func Ping(ctx context.Context, client *bun.DB) error {
	start := time.Now()

	for err := client.PingContext(ctx); err != nil; err = client.PingContext(ctx) {
		if time.Since(start) > PingTimeout {
			return fmt.Errorf("ping database: %w", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("ping database: %w", ctx.Err())
		case <-time.After(PingRetryInterval):
		}
	}

	return nil
}
