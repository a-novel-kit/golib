package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// healthTimeout bounds one database health probe when the caller has not
// already supplied a shorter deadline.
const healthTimeout = time.Second

// Health probes the connection pool ctx carries, once. It retries a discarded connection, or a
// server that is restarting, within a second, or within the deadline of ctx when that comes
// first. Any other error is unhealthy, and so is a ctx carrying no database, or only a transaction.
func Health(ctx context.Context) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get database from context: %w", err)
	}

	pool, ok := db.(*bun.DB)
	if !ok {
		return ErrNoDBInContext
	}

	return ping(ctx, pool, healthTimeout, pingRetryInterval, transient)
}
