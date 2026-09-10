package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// HealthTimeout bounds one database health probe when the caller has not
// already supplied a shorter deadline.
const HealthTimeout = time.Second

// Health performs one context-aware health probe against the pooled database
// connection carried by ctx. It retries discarded connections within the single
// HealthTimeout budget, respecting earlier caller deadlines and cancellation.
// Other errors, missing contexts and transaction-only contexts are unhealthy.
func Health(ctx context.Context) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get database from context: %w", err)
	}

	pool, ok := db.(*bun.DB)
	if !ok {
		return ErrNoDbInContext
	}

	return ping(ctx, pool, HealthTimeout, PingRetryInterval, func(err error) bool {
		return errors.Is(err, driver.ErrBadConn)
	})
}
