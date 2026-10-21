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

// Health probes the connection pool ctx carries, once. It retries a discarded connection within
// [HealthTimeout], or within the deadline of ctx when that comes first. Any other error is
// unhealthy, and so is a ctx carrying no database, or only a transaction.
func Health(ctx context.Context) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get database from context: %w", err)
	}

	pool, ok := db.(*bun.DB)
	if !ok {
		return ErrNoDBInContext
	}

	return ping(ctx, pool, HealthTimeout, PingRetryInterval, func(err error) bool {
		return errors.Is(err, driver.ErrBadConn)
	})
}
