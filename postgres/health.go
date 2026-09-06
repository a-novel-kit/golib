package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// HealthTimeout bounds one database health probe when the caller has not
// already supplied a shorter deadline.
const HealthTimeout = time.Second

// Health performs one context-aware health probe against the pooled database
// connection carried by ctx. Missing and transaction-only contexts cannot
// establish pool readiness and are reported as unhealthy.
func Health(ctx context.Context) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get database from context: %w", err)
	}

	pool, ok := db.(*bun.DB)
	if !ok {
		return ErrNoDbInContext
	}

	probeCtx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()

	err = pool.PingContext(probeCtx)
	if err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
