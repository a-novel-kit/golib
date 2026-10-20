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

// Ping retries a database connection until it answers or [PingTimeout] passes. It honors ctx
// cancellation both for each attempt and for the wait between attempts.
func Ping(ctx context.Context, client *bun.DB) error {
	return ping(ctx, client, PingTimeout, PingRetryInterval, nil)
}

// pinger is the part of a database handle ping needs, so a test can script its answers.
type pinger interface {
	PingContext(ctx context.Context) error
}

// ping retries client until it answers, timeout passes or ctx ends. It stops at the first error
// retryable rejects. A nil retryable retries every error.
func ping(
	ctx context.Context, client pinger, timeout time.Duration, retryInterval time.Duration,
	retryable func(error) bool,
) error {
	pingContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		err := client.PingContext(pingContext)

		switch {
		case err == nil:
			return nil
		case pingContext.Err() != nil:
			return fmt.Errorf("ping database: %w", errors.Join(err, pingContext.Err()))
		case retryable != nil && !retryable(err):
			return fmt.Errorf("ping database: %w", err)
		}

		select {
		case <-pingContext.Done():
			return fmt.Errorf("ping database: %w", errors.Join(err, pingContext.Err()))
		case <-time.After(retryInterval):
		}
	}
}
