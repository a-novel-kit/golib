//go:build !unix

package postgrespresets

import (
	"context"

	"github.com/uptrace/bun/driver/pgdriver"
)

// checkIdleConn keeps every pooled connection where the socket cannot be read without waiting. A
// connection the server closed then fails its next query.
func checkIdleConn(context.Context, *pgdriver.Conn) error {
	return nil
}
